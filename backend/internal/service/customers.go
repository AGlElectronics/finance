package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Customer is who an invoice is addressed to.
type Customer struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	BillingAddress string    `json:"billing_address"`
	TaxID          string    `json:"tax_id"`
	Notes          string    `json:"notes"`
	Archived       bool      `json:"archived"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// List annotations.
	InvoiceCount     int64 `json:"invoice_count"`
	OutstandingCents int64 `json:"outstanding_cents"`
}

// CustomerInput creates or updates a customer. On update, nil fields keep
// their current value.
type CustomerInput struct {
	Name           *string `json:"name"`
	Email          *string `json:"email"`
	BillingAddress *string `json:"billing_address"`
	TaxID          *string `json:"tax_id"`
	Notes          *string `json:"notes"`
	Archived       *bool   `json:"archived"`
}

// CustomerService owns the customers table.
type CustomerService struct {
	pool  *pgxpool.Pool
	audit *audit.Writer
}

func NewCustomerService(pool *pgxpool.Pool, audit *audit.Writer) *CustomerService {
	return &CustomerService{pool: pool, audit: audit}
}

const customerCols = `
	c.id, c.name, c.email, c.billing_address, c.tax_id, c.notes,
	c.archived_at IS NOT NULL, c.created_at, c.updated_at,
	(SELECT count(*) FROM invoices i WHERE i.customer_id = c.id),
	COALESCE((SELECT sum(i.total_cents - COALESCE(p.paid, 0))
	            FROM invoices i
	            LEFT JOIN LATERAL (SELECT sum(amount_cents) AS paid
	                                 FROM payments WHERE invoice_id = i.id) p ON true
	           WHERE i.customer_id = c.id AND i.status = 'issued'), 0)`

func scanCustomer(row pgx.Row) (Customer, error) {
	var c Customer
	err := row.Scan(&c.ID, &c.Name, &c.Email, &c.BillingAddress, &c.TaxID, &c.Notes,
		&c.Archived, &c.CreatedAt, &c.UpdatedAt, &c.InvoiceCount, &c.OutstandingCents)
	return c, err
}

// List returns customers ordered by name. Archived ones only when asked.
func (s *CustomerService) List(ctx context.Context, includeArchived bool) ([]Customer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+customerCols+`
		  FROM customers c
		 WHERE $1 OR c.archived_at IS NULL
		 ORDER BY lower(c.name)`, includeArchived)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []Customer{}
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get returns one customer.
func (s *CustomerService) Get(ctx context.Context, id uuid.UUID) (Customer, error) {
	c, err := scanCustomer(s.pool.QueryRow(ctx, `
		SELECT `+customerCols+` FROM customers c WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Customer{}, httperror.NotFound("customer")
	}
	if err != nil {
		return Customer{}, httperror.Internal(err)
	}
	return c, nil
}

// Create inserts a customer; name is the only required field.
func (s *CustomerService) Create(ctx context.Context, actorName string, in CustomerInput) (Customer, error) {
	name := strings.TrimSpace(deref(in.Name))
	if name == "" {
		return Customer{}, httperror.Validation("customer needs a name", map[string]string{"name": "required"})
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO customers (name, email, billing_address, tax_id, notes)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		name, strings.TrimSpace(deref(in.Email)), strings.TrimSpace(deref(in.BillingAddress)),
		strings.TrimSpace(deref(in.TaxID)), strings.TrimSpace(deref(in.Notes))).Scan(&id)
	if err != nil {
		return Customer{}, httperror.Internal(err)
	}
	if err := s.audit.Record(ctx, actorName, "customer.created", "customer", id, map[string]string{"name": name}); err != nil {
		return Customer{}, err
	}
	return s.Get(ctx, id)
}

// Update patches the given fields; nil fields are left alone.
func (s *CustomerService) Update(ctx context.Context, actorName string, id uuid.UUID, in CustomerInput) (Customer, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return Customer{}, httperror.Validation("customer needs a name", map[string]string{"name": "required"})
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE customers SET
			name            = COALESCE(btrim($2), name),
			email           = COALESCE(btrim($3), email),
			billing_address = COALESCE(btrim($4), billing_address),
			tax_id          = COALESCE(btrim($5), tax_id),
			notes           = COALESCE(btrim($6), notes),
			archived_at     = CASE WHEN $7::boolean IS NULL THEN archived_at
			                       WHEN $7 THEN COALESCE(archived_at, now())
			                       ELSE NULL END,
			updated_at      = now()
		 WHERE id = $1`,
		id, in.Name, in.Email, in.BillingAddress, in.TaxID, in.Notes, in.Archived)
	if err != nil {
		return Customer{}, httperror.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return Customer{}, httperror.NotFound("customer")
	}
	if err := s.audit.Record(ctx, actorName, "customer.updated", "customer", id, nil); err != nil {
		return Customer{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes a customer that has no invoices; otherwise it conflicts
// (archive the customer instead, PATCH {"archived": true}).
func (s *CustomerService) Delete(ctx context.Context, actorName string, id uuid.UUID) error {
	var invoices int64
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE customer_id = $1`, id).Scan(&invoices); err != nil {
		return httperror.Internal(err)
	}
	if invoices > 0 {
		return httperror.Conflict("customer has invoices; archive it instead")
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id)
	if err != nil {
		return httperror.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("customer")
	}
	return s.audit.Record(ctx, actorName, "customer.deleted", "customer", id, nil)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
