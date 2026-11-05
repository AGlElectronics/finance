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
	"github.com/AGlElectronics/finance/backend/internal/domain"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Expense is money going out: rent, gear, travel, services.
type Expense struct {
	ID          uuid.UUID `json:"id"`
	OccurredOn  string    `json:"occurred_on"` // YYYY-MM-DD
	Vendor      string    `json:"vendor"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	AmountCents int64     `json:"amount_cents"`
	Method      string    `json:"method"`
	Reference   string    `json:"reference"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ExpenseInput creates or updates an expense. On update, nil fields keep
// their current value.
type ExpenseInput struct {
	OccurredOn  *string `json:"occurred_on"`
	Vendor      *string `json:"vendor"`
	Category    *string `json:"category"`
	Description *string `json:"description"`
	AmountCents *int64  `json:"amount_cents"`
	Method      *string `json:"method"`
	Reference   *string `json:"reference"`
}

func (in ExpenseInput) validate(creating bool) error {
	fields := map[string]string{}
	if in.Vendor != nil && strings.TrimSpace(*in.Vendor) == "" {
		fields["vendor"] = "required"
	}
	if creating && in.Vendor == nil {
		fields["vendor"] = "required"
	}
	if in.AmountCents != nil && *in.AmountCents <= 0 {
		fields["amount_cents"] = "must be positive"
	}
	if creating && in.AmountCents == nil {
		fields["amount_cents"] = "required"
	}
	if in.Category != nil && !domain.ValidExpenseCategory(*in.Category) {
		fields["category"] = "unknown category"
	}
	if in.Method != nil && !domain.ValidPaymentMethod(*in.Method) {
		fields["method"] = "unknown method"
	}
	if in.OccurredOn != nil {
		if _, err := time.Parse("2006-01-02", *in.OccurredOn); err != nil {
			fields["occurred_on"] = "use YYYY-MM-DD"
		}
	}
	if len(fields) > 0 {
		return httperror.Validation("expense is not valid", fields)
	}
	return nil
}

// ExpenseService owns the expenses table.
type ExpenseService struct {
	pool  *pgxpool.Pool
	audit *audit.Writer
}

func NewExpenseService(pool *pgxpool.Pool, audit *audit.Writer) *ExpenseService {
	return &ExpenseService{pool: pool, audit: audit}
}

const expenseCols = `
	e.id, to_char(e.occurred_on, 'YYYY-MM-DD'), e.vendor, e.category,
	e.description, e.amount_cents, e.method, e.reference, e.created_at, e.updated_at`

func scanExpense(row pgx.Row) (Expense, error) {
	var e Expense
	err := row.Scan(&e.ID, &e.OccurredOn, &e.Vendor, &e.Category,
		&e.Description, &e.AmountCents, &e.Method, &e.Reference, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// List returns expenses newest first, optionally only one category.
func (s *ExpenseService) List(ctx context.Context, category string) ([]Expense, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+expenseCols+`
		  FROM expenses e
		 WHERE $1 = '' OR e.category = $1
		 ORDER BY e.occurred_on DESC, e.created_at DESC`, category)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns one expense.
func (s *ExpenseService) Get(ctx context.Context, id uuid.UUID) (Expense, error) {
	e, err := scanExpense(s.pool.QueryRow(ctx, `
		SELECT `+expenseCols+` FROM expenses e WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, httperror.NotFound("expense")
	}
	if err != nil {
		return Expense{}, httperror.Internal(err)
	}
	return e, nil
}

// Create records a new expense.
func (s *ExpenseService) Create(ctx context.Context, actorName string, in ExpenseInput) (Expense, error) {
	if err := in.validate(true); err != nil {
		return Expense{}, err
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO expenses (occurred_on, vendor, category, description, amount_cents, method, reference)
		VALUES (COALESCE($1::date, current_date), $2, COALESCE($3, 'other'), $4, $5,
		        COALESCE($6, 'bank_transfer'), $7)
		RETURNING id`,
		in.OccurredOn, strings.TrimSpace(deref(in.Vendor)), in.Category,
		strings.TrimSpace(deref(in.Description)), *in.AmountCents,
		in.Method, strings.TrimSpace(deref(in.Reference))).Scan(&id)
	if err != nil {
		return Expense{}, httperror.Internal(err)
	}
	if err := s.audit.Record(ctx, actorName, "expense.created", "expense", id,
		map[string]string{"vendor": strings.TrimSpace(deref(in.Vendor))}); err != nil {
		return Expense{}, err
	}
	return s.Get(ctx, id)
}

// Update patches the given fields; nil fields are left alone.
func (s *ExpenseService) Update(ctx context.Context, actorName string, id uuid.UUID, in ExpenseInput) (Expense, error) {
	if err := in.validate(false); err != nil {
		return Expense{}, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE expenses SET
			occurred_on  = COALESCE($2::date, occurred_on),
			vendor       = COALESCE(btrim($3), vendor),
			category     = COALESCE($4, category),
			description  = COALESCE(btrim($5), description),
			amount_cents = COALESCE($6, amount_cents),
			method       = COALESCE($7, method),
			reference    = COALESCE(btrim($8), reference),
			updated_at   = now()
		 WHERE id = $1`,
		id, in.OccurredOn, in.Vendor, in.Category, in.Description, in.AmountCents, in.Method, in.Reference)
	if err != nil {
		return Expense{}, httperror.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return Expense{}, httperror.NotFound("expense")
	}
	if err := s.audit.Record(ctx, actorName, "expense.updated", "expense", id, nil); err != nil {
		return Expense{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes an expense.
func (s *ExpenseService) Delete(ctx context.Context, actorName string, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	if err != nil {
		return httperror.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return httperror.NotFound("expense")
	}
	return s.audit.Record(ctx, actorName, "expense.deleted", "expense", id, nil)
}
