package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Customer is who an invoice is addressed to. Changing a customer never
// changes invoices already issued: they keep a frozen copy.
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

const customerCols = `
	c.id, c.name, c.email, c.billing_address, c.tax_id, c.notes,
	c.archived_at IS NOT NULL, c.created_at, c.updated_at,
	(SELECT count(*) FROM invoices i WHERE i.customer_id = c.id),
	COALESCE((SELECT sum(GREATEST(b.total_cents - b.credited - (b.paid - b.refunded), 0))
	            FROM (` + invoiceBase + `) b
	           WHERE b.customer_id = c.id AND b.status = 'issued'), 0)`

func scanCustomer(row pgx.Row) (Customer, error) {
	var c Customer
	err := row.Scan(&c.ID, &c.Name, &c.Email, &c.BillingAddress, &c.TaxID, &c.Notes,
		&c.Archived, &c.CreatedAt, &c.UpdatedAt, &c.InvoiceCount, &c.OutstandingCents)
	return c, err
}

// ListCustomers returns customers ordered by name. Archived ones only when asked.
func (s *Service) ListCustomers(ctx context.Context, includeArchived bool) ([]Customer, error) {
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

// GetCustomer returns one customer.
func (s *Service) GetCustomer(ctx context.Context, id uuid.UUID) (Customer, error) {
	c, err := getCustomer(ctx, s.pool, id)
	return c, renderDBErr(err)
}

func getCustomer(ctx context.Context, q querier, id uuid.UUID) (Customer, error) {
	c, err := scanCustomer(q.QueryRow(ctx, `SELECT `+customerCols+` FROM customers c WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Customer{}, httperror.NotFound("customer")
	}
	return c, err
}

func checkCustomer(in CustomerInput, creating bool) error {
	fields := map[string]string{}
	if (creating || in.Name != nil) && strings.TrimSpace(deref(in.Name)) == "" {
		fields["name"] = "required"
	}
	for key, v := range map[string]*string{"name": in.Name, "email": in.Email, "billing_address": in.BillingAddress, "tax_id": in.TaxID, "notes": in.Notes} {
		if v != nil && len(*v) > 2000 {
			fields[key] = "too long"
		}
	}
	if len(fields) > 0 {
		return httperror.Validation("customer is not valid", fields)
	}
	return nil
}

// CreateCustomer inserts a customer; name is the only required field.
func (s *Service) CreateCustomer(ctx context.Context, who audit.Actor, in CustomerInput) (Customer, error) {
	if err := checkCustomer(in, true); err != nil {
		return Customer{}, err
	}
	var out Customer
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO customers (name, email, billing_address, tax_id, notes)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			strings.TrimSpace(deref(in.Name)), strings.TrimSpace(deref(in.Email)), strings.TrimSpace(deref(in.BillingAddress)),
			strings.TrimSpace(deref(in.TaxID)), strings.TrimSpace(deref(in.Notes))).Scan(&id); err != nil {
			return err
		}
		var err error
		if out, err = getCustomer(ctx, tx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{Action: "customer.created", EntityType: "customer", EntityID: id, After: out})
	})
	return out, err
}

// UpdateCustomer patches the given fields; nil fields are left alone.
func (s *Service) UpdateCustomer(ctx context.Context, who audit.Actor, id uuid.UUID, in CustomerInput) (Customer, error) {
	if err := checkCustomer(in, false); err != nil {
		return Customer{}, err
	}
	var out Customer
	err := s.tx(ctx, func(tx pgx.Tx) error {
		before, err := getCustomer(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
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
			id, in.Name, in.Email, in.BillingAddress, in.TaxID, in.Notes, in.Archived); err != nil {
			return err
		}
		if out, err = getCustomer(ctx, tx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{Action: "customer.updated", EntityType: "customer", EntityID: id, Before: before, After: out})
	})
	return out, err
}

// DeleteCustomer removes a customer that has no invoices; otherwise it
// conflicts (archive the customer instead, PATCH {"archived": true}).
func (s *Service) DeleteCustomer(ctx context.Context, who audit.Actor, id uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		before, err := getCustomer(ctx, tx, id)
		if err != nil {
			return err
		}
		if before.InvoiceCount > 0 {
			return httperror.Conflict("customer has invoices; archive it instead")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id); err != nil {
			if isForeignKey(err) {
				return httperror.Conflict("customer has invoices; archive it instead")
			}
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{Action: "customer.deleted", EntityType: "customer", EntityID: id, Before: before})
	})
}
