package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
	// Deleted expenses stay in the database (and the audit log) but leave
	// the ledger and the dashboard.
	Deleted       bool       `json:"deleted"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	DeletedReason string     `json:"deleted_reason,omitempty"`
}

// ExpenseInput creates or updates an expense. On update, nil fields keep
// their current value; the amount and the date cannot change after the
// expense is recorded (delete it and record it again).
type ExpenseInput struct {
	OccurredOn  *string `json:"occurred_on"`
	Vendor      *string `json:"vendor"`
	Category    *string `json:"category"`
	Description *string `json:"description"`
	AmountCents *int64  `json:"amount_cents"`
	Method      *string `json:"method"`
	Reference   *string `json:"reference"`
}

// DeleteInput explains a deletion.
type DeleteInput struct {
	Reason string `json:"reason"`
}

// ExpenseFilter narrows ListExpenses.
type ExpenseFilter struct {
	Category       string
	IncludeDeleted bool
	Page
}

// ExpensePage is one page of expenses.
type ExpensePage struct {
	Expenses []Expense `json:"expenses"`
	Total    int64     `json:"total"`
	Page
}

func (in ExpenseInput) validate(creating bool) error {
	fields := map[string]string{}
	if in.Vendor != nil && strings.TrimSpace(*in.Vendor) == "" {
		fields["vendor"] = "required"
	}
	if creating && in.Vendor == nil {
		fields["vendor"] = "required"
	}
	if in.AmountCents != nil && (*in.AmountCents <= 0 || *in.AmountCents > domain.MaxAmountCents) {
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
		if _, err := time.Parse(dateLayout, *in.OccurredOn); err != nil {
			fields["occurred_on"] = "use YYYY-MM-DD"
		}
	}
	for key, v := range map[string]*string{"vendor": in.Vendor, "description": in.Description, "reference": in.Reference} {
		if v != nil && len(*v) > 2000 {
			fields[key] = "too long"
		}
	}
	if len(fields) > 0 {
		return httperror.Validation("expense is not valid", fields)
	}
	return nil
}

const expenseCols = `
	e.id, to_char(e.occurred_on, 'YYYY-MM-DD'), e.vendor, e.category,
	e.description, e.amount_cents, e.method, e.reference, e.created_at, e.updated_at,
	e.deleted_at, e.deleted_reason`

func scanExpense(row pgx.Row) (Expense, error) {
	var e Expense
	err := row.Scan(&e.ID, &e.OccurredOn, &e.Vendor, &e.Category,
		&e.Description, &e.AmountCents, &e.Method, &e.Reference, &e.CreatedAt, &e.UpdatedAt,
		&e.DeletedAt, &e.DeletedReason)
	e.Deleted = e.DeletedAt != nil
	return e, err
}

// ListExpenses returns one page of expenses, newest first.
func (s *Service) ListExpenses(ctx context.Context, f ExpenseFilter) (ExpensePage, error) {
	f.Page = f.Page.Normalize()
	out := ExpensePage{Expenses: []Expense{}, Page: f.Page}
	const where = `
		  FROM expenses e
		 WHERE ($1 = '' OR e.category = $1) AND ($2 OR e.deleted_at IS NULL)`
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, f.Category, f.IncludeDeleted).Scan(&out.Total); err != nil {
		return out, httperror.Internal(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+expenseCols+where+`
		 ORDER BY e.occurred_on DESC, e.created_at DESC, e.id DESC LIMIT $3 OFFSET $4`,
		f.Category, f.IncludeDeleted, f.Limit, f.Offset)
	if err != nil {
		return out, httperror.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return out, httperror.Internal(err)
		}
		out.Expenses = append(out.Expenses, e)
	}
	return out, rows.Err()
}

// GetExpense returns one expense, deleted or not.
func (s *Service) GetExpense(ctx context.Context, id uuid.UUID) (Expense, error) {
	e, err := getExpense(ctx, s.pool, id, false)
	return e, renderDBErr(err)
}

func getExpense(ctx context.Context, q querier, id uuid.UUID, lock bool) (Expense, error) {
	sql := `SELECT ` + expenseCols + ` FROM expenses e WHERE e.id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	e, err := scanExpense(q.QueryRow(ctx, sql, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Expense{}, httperror.NotFound("expense")
	}
	return e, err
}

// CreateExpense records a new expense, or asks for approval first when the
// amount is above the approval threshold.
func (s *Service) CreateExpense(ctx context.Context, who audit.Actor, idem Idem, in ExpenseInput) (Result, error) {
	if err := in.validate(true); err != nil {
		return Result{}, err
	}
	d := s.today()
	if in.OccurredOn != nil {
		d, _ = time.Parse(dateLayout, *in.OccurredOn)
	}
	if err := s.checkBookingDate("occurred_on", d); err != nil {
		return Result{}, err
	}
	day := d.Format(dateLayout)
	in.OccurredOn = &day
	var res Result
	err := s.tx(ctx, func(tx pgx.Tx) error {
		if prev, err := claimIdem(ctx, tx, who, idem); err != nil || prev != nil {
			if prev != nil {
				res = *prev
			}
			return err
		}
		if _, err := s.execExpense(ctx, tx, who, in, true, nil); err != nil {
			return err
		}
		var err error
		if s.needsApproval(*in.AmountCents) {
			res.Pending = true
			if res.ID, err = s.requestApproval(ctx, tx, who, ActionExpense, nil, *in.AmountCents, in); err != nil {
				return err
			}
		} else if res.ID, err = s.execExpense(ctx, tx, who, in, false, nil); err != nil {
			return err
		}
		return finishIdem(ctx, tx, who, idem, res, "expense")
	})
	return res, err
}

// execExpense checks (dry) or inserts an expense. in.OccurredOn is set.
func (s *Service) execExpense(ctx context.Context, tx pgx.Tx, who audit.Actor, in ExpenseInput, dry bool, approvalID *uuid.UUID) (uuid.UUID, error) {
	d, _ := time.Parse(dateLayout, deref(in.OccurredOn))
	if err := ensureOpen(ctx, tx, d); err != nil {
		return uuid.Nil, err
	}
	if dry {
		return uuid.Nil, nil
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO expenses (occurred_on, vendor, category, description, amount_cents, method, reference)
		VALUES ($1::date, $2, COALESCE($3, 'other'), $4, $5, COALESCE($6, 'bank_transfer'), $7)
		RETURNING id`,
		d, strings.TrimSpace(deref(in.Vendor)), in.Category,
		strings.TrimSpace(deref(in.Description)), *in.AmountCents,
		in.Method, strings.TrimSpace(deref(in.Reference))).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	e, err := getExpense(ctx, tx, id, false)
	if err != nil {
		return uuid.Nil, err
	}
	return id, s.audit.Record(ctx, tx, who, audit.Event{
		Action: "expense.created", EntityType: "expense", EntityID: id, After: e,
		Metadata: map[string]any{"approval_id": approvalID},
	})
}

// UpdateExpense patches vendor, category, description, method and
// reference. The amount and date are fixed once recorded, so a change can
// never bypass approval or a closed month: delete and record again instead.
func (s *Service) UpdateExpense(ctx context.Context, who audit.Actor, id uuid.UUID, in ExpenseInput) (Expense, error) {
	if err := in.validate(false); err != nil {
		return Expense{}, err
	}
	var out Expense
	err := s.tx(ctx, func(tx pgx.Tx) error {
		before, err := getExpense(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if before.Deleted {
			return httperror.Conflict("deleted expenses cannot be changed")
		}
		fields := map[string]string{}
		if in.AmountCents != nil && *in.AmountCents != before.AmountCents {
			fields["amount_cents"] = "cannot change; delete the expense and record it again"
		}
		if in.OccurredOn != nil && *in.OccurredOn != before.OccurredOn {
			fields["occurred_on"] = "cannot change; delete the expense and record it again"
		}
		if len(fields) > 0 {
			return httperror.Validation("the amount and date of a recorded expense are fixed", fields)
		}
		d, _ := time.Parse(dateLayout, before.OccurredOn)
		if err := ensureOpen(ctx, tx, d); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE expenses SET
				vendor      = COALESCE(btrim($2), vendor),
				category    = COALESCE($3, category),
				description = COALESCE(btrim($4), description),
				method      = COALESCE($5, method),
				reference   = COALESCE(btrim($6), reference),
				updated_at  = now()
			 WHERE id = $1`,
			id, in.Vendor, in.Category, in.Description, in.Method, in.Reference); err != nil {
			return err
		}
		if out, err = getExpense(ctx, tx, id, false); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "expense.updated", EntityType: "expense", EntityID: id, Before: before, After: out,
		})
	})
	return out, err
}

// DeleteExpense soft-deletes an expense: it stays in the database with the
// reason, and leaves the ledger and dashboard.
func (s *Service) DeleteExpense(ctx context.Context, who audit.Actor, id uuid.UUID, in DeleteInput) error {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return httperror.Validation("say why the expense is deleted", map[string]string{"reason": "required"})
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		before, err := getExpense(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if before.Deleted {
			return httperror.Conflict("expense is already deleted")
		}
		d, _ := time.Parse(dateLayout, before.OccurredOn)
		if err := ensureOpen(ctx, tx, d); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE expenses SET deleted_at = now(), deleted_reason = $2, updated_at = now() WHERE id = $1`, id, reason); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "expense.deleted", EntityType: "expense", EntityID: id, Before: before,
			Metadata: map[string]string{"reason": reason},
		})
	})
}
