package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/db"
	"github.com/AGlElectronics/finance/backend/internal/domain"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

const dateLayout = "2006-01-02"

// Line is one row on an invoice.
type Line struct {
	ID          uuid.UUID `json:"id"`
	Description string    `json:"description"`
	// Quantity in units (1.5 = one and a half); stored in hundredths.
	Quantity       float64 `json:"quantity"`
	UnitPriceCents int64   `json:"unit_price_cents"`
	TaxRateBp      int32   `json:"tax_rate_bp"`
	SubtotalCents  int64   `json:"subtotal_cents"`
	TaxCents       int64   `json:"tax_cents"`
	TotalCents     int64   `json:"total_cents"`
}

// Payment is money received against an issued invoice.
type Payment struct {
	ID          uuid.UUID `json:"id"`
	PaidOn      string    `json:"paid_on"`
	AmountCents int64     `json:"amount_cents"`
	Method      string    `json:"method"`
	Reference   string    `json:"reference"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
}

// InvoiceSummary is the list shape.
type InvoiceSummary struct {
	ID           uuid.UUID `json:"id"`
	Number       string    `json:"number,omitempty"`
	CustomerID   uuid.UUID `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	// Status is derived: draft | issued | partial | paid | void.
	Status    string `json:"status"`
	Currency  string `json:"currency"`
	IssueDate string `json:"issue_date,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
	Overdue   bool   `json:"overdue"`

	SubtotalCents int64 `json:"subtotal_cents"`
	TaxCents      int64 `json:"tax_cents"`
	TotalCents    int64 `json:"total_cents"`
	PaidCents     int64 `json:"paid_cents"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Invoice is the detail shape.
type Invoice struct {
	InvoiceSummary
	Notes    string    `json:"notes"`
	Lines    []Line    `json:"lines"`
	Payments []Payment `json:"payments"`
}

// LineInput is one line on a draft.
type LineInput struct {
	Description    string  `json:"description"`
	Quantity       float64 `json:"quantity"`
	UnitPriceCents int64   `json:"unit_price_cents"`
	TaxRateBp      int32   `json:"tax_rate_bp"`
}

// InvoiceInput creates or rewrites a draft.
type InvoiceInput struct {
	CustomerID uuid.UUID   `json:"customer_id"`
	DueDate    string      `json:"due_date,omitempty"`
	Notes      string      `json:"notes"`
	Lines      []LineInput `json:"lines"`
}

// IssueInput issues a draft. Empty dates default to today / +30 days.
type IssueInput struct {
	IssueDate string `json:"issue_date,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
}

// PaymentInput records money received.
type PaymentInput struct {
	PaidOn      string `json:"paid_on,omitempty"`
	AmountCents int64  `json:"amount_cents"`
	Method      string `json:"method"`
	Reference   string `json:"reference"`
	Note        string `json:"note"`
}

// InvoiceService owns invoices, their lines and payments.
type InvoiceService struct {
	pool  *pgxpool.Pool
	audit *audit.Writer
}

func NewInvoiceService(pool *pgxpool.Pool, audit *audit.Writer) *InvoiceService {
	return &InvoiceService{pool: pool, audit: audit}
}

type checkedLine struct {
	in            LineInput
	qtyHundredths int64
	subtotal      int64
	tax           int64
}

func checkLines(lines []LineInput) ([]checkedLine, int64, int64, error) {
	if len(lines) == 0 {
		return nil, 0, 0, httperror.Validation("an invoice needs at least one line", nil)
	}
	out := make([]checkedLine, 0, len(lines))
	var subtotal, tax int64
	for i, l := range lines {
		issues := map[string]string{}
		if strings.TrimSpace(l.Description) == "" {
			issues["description"] = "required"
		}
		qtyH := int64(math.Round(l.Quantity * 100))
		if qtyH <= 0 || qtyH > 100_000_000 {
			issues["quantity"] = "must be between 0.01 and 1000000"
		}
		if l.TaxRateBp < 0 || l.TaxRateBp > 10000 {
			issues["tax_rate_bp"] = "must be between 0 and 10000"
		}
		if len(issues) > 0 {
			return nil, 0, 0, httperror.Validation(fmt.Sprintf("line %d is invalid", i+1), issues)
		}
		c := checkedLine{in: l, qtyHundredths: qtyH}
		c.subtotal = domain.LineSubtotal(qtyH, l.UnitPriceCents)
		c.tax = domain.LineTax(c.subtotal, l.TaxRateBp)
		subtotal += c.subtotal
		tax += c.tax
		out = append(out, c)
	}
	if subtotal+tax < 0 {
		return nil, 0, 0, httperror.Validation("invoice total cannot be negative", nil)
	}
	return out, subtotal, tax, nil
}

func parseDate(s, field string) (*time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return nil, httperror.Validation(field+" must be a YYYY-MM-DD date", map[string]string{field: "invalid"})
	}
	return &t, nil
}

func writeLines(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID, lines []checkedLine) error {
	if _, err := tx.Exec(ctx, `DELETE FROM invoice_lines WHERE invoice_id = $1`, invoiceID); err != nil {
		return err
	}
	for i, l := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_lines (invoice_id, position, description, quantity_hundredths, unit_price_cents, tax_rate_bp)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			invoiceID, i, strings.TrimSpace(l.in.Description), l.qtyHundredths, l.in.UnitPriceCents, l.in.TaxRateBp); err != nil {
			return err
		}
	}
	return nil
}

// Create inserts a draft invoice with its lines and stored totals.
func (s *InvoiceService) Create(ctx context.Context, actorName string, in InvoiceInput) (Invoice, error) {
	lines, subtotal, tax, err := checkLines(in.Lines)
	if err != nil {
		return Invoice{}, err
	}
	due, err := parseDate(in.DueDate, "due_date")
	if err != nil {
		return Invoice{}, err
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO invoices (customer_id, due_date, notes, subtotal_cents, tax_cents, total_cents)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.CustomerID, due, strings.TrimSpace(in.Notes), subtotal, tax, subtotal+tax).Scan(&id); err != nil {
			if isForeignKey(err) {
				return httperror.Validation("customer does not exist", map[string]string{"customer_id": "unknown"})
			}
			return err
		}
		return writeLines(ctx, tx, id, lines)
	})
	if err != nil {
		return Invoice{}, renderDBErr(err)
	}
	if err := s.audit.Record(ctx, actorName, "invoice.created", "invoice", id, nil); err != nil {
		return Invoice{}, err
	}
	return s.Get(ctx, id)
}

// UpdateDraft rewrites a draft's customer, dates, notes and lines. Issued
// and void invoices are immutable.
func (s *InvoiceService) UpdateDraft(ctx context.Context, actorName string, id uuid.UUID, in InvoiceInput) (Invoice, error) {
	lines, subtotal, tax, err := checkLines(in.Lines)
	if err != nil {
		return Invoice{}, err
	}
	due, err := parseDate(in.DueDate, "due_date")
	if err != nil {
		return Invoice{}, err
	}
	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusDraft {
			return httperror.Conflict("only draft invoices can be edited")
		}
		if _, err := tx.Exec(ctx, `
			UPDATE invoices SET customer_id = $2, due_date = $3, notes = $4,
			       subtotal_cents = $5, tax_cents = $6, total_cents = $7, updated_at = now()
			 WHERE id = $1`,
			id, in.CustomerID, due, strings.TrimSpace(in.Notes), subtotal, tax, subtotal+tax); err != nil {
			if isForeignKey(err) {
				return httperror.Validation("customer does not exist", map[string]string{"customer_id": "unknown"})
			}
			return err
		}
		return writeLines(ctx, tx, id, lines)
	})
	if err != nil {
		return Invoice{}, renderDBErr(err)
	}
	if err := s.audit.Record(ctx, actorName, "invoice.updated", "invoice", id, nil); err != nil {
		return Invoice{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes a draft. Issued invoices are never deleted — void them.
func (s *InvoiceService) Delete(ctx context.Context, actorName string, id uuid.UUID) error {
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusDraft {
			return httperror.Conflict("only drafts can be deleted; void the invoice instead")
		}
		_, err = tx.Exec(ctx, `DELETE FROM invoices WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return renderDBErr(err)
	}
	return s.audit.Record(ctx, actorName, "invoice.deleted", "invoice", id, nil)
}

// Issue turns a draft into an issued invoice: it takes the next sequential
// number (FIN-0001), stamps the dates and locks the lines.
func (s *InvoiceService) Issue(ctx context.Context, actorName string, id uuid.UUID, in IssueInput) (Invoice, error) {
	issueT, err := parseDate(in.IssueDate, "issue_date")
	if err != nil {
		return Invoice{}, err
	}
	dueT, err := parseDate(in.DueDate, "due_date")
	if err != nil {
		return Invoice{}, err
	}
	var number string
	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusDraft {
			return httperror.Conflict("only drafts can be issued")
		}
		var total int64
		var lineCount int
		if err := tx.QueryRow(ctx, `
			SELECT total_cents, (SELECT count(*) FROM invoice_lines WHERE invoice_id = $1)
			  FROM invoices WHERE id = $1`, id).Scan(&total, &lineCount); err != nil {
			return err
		}
		if lineCount == 0 {
			return httperror.Validation("an invoice needs at least one line before it can be issued", nil)
		}
		var n int
		if err := tx.QueryRow(ctx,
			`UPDATE invoice_counter SET last = last + 1 WHERE id = 1 RETURNING last`).Scan(&n); err != nil {
			return err
		}
		number = fmt.Sprintf("FIN-%04d", n)
		issue := time.Now()
		if issueT != nil {
			issue = *issueT
		}
		due := issue.AddDate(0, 0, 30)
		if dueT != nil {
			due = *dueT
		}
		_, err = tx.Exec(ctx, `
			UPDATE invoices SET status = 'issued', number = $2, issue_date = $3, due_date = $4,
			       issued_at = now(), updated_at = now()
			 WHERE id = $1`, id, number, issue, due)
		return err
	})
	if err != nil {
		return Invoice{}, renderDBErr(err)
	}
	if err := s.audit.Record(ctx, actorName, "invoice.issued", "invoice", id, map[string]string{"number": number}); err != nil {
		return Invoice{}, err
	}
	return s.Get(ctx, id)
}

// Void cancels an issued invoice. Its number stays taken.
func (s *InvoiceService) Void(ctx context.Context, actorName string, id uuid.UUID) (Invoice, error) {
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusIssued {
			if status == domain.StatusDraft {
				return httperror.Conflict("drafts are deleted, not voided")
			}
			return httperror.Conflict("invoice is already void")
		}
		_, err = tx.Exec(ctx,
			`UPDATE invoices SET status = 'void', voided_at = now(), updated_at = now() WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return Invoice{}, renderDBErr(err)
	}
	if err := s.audit.Record(ctx, actorName, "invoice.voided", "invoice", id, nil); err != nil {
		return Invoice{}, err
	}
	return s.Get(ctx, id)
}

// AddPayment records money received against an issued invoice. The amount
// may not exceed what is still owed.
func (s *InvoiceService) AddPayment(ctx context.Context, actorName string, id uuid.UUID, in PaymentInput) (Invoice, error) {
	if in.AmountCents <= 0 {
		return Invoice{}, httperror.Validation("amount must be positive", map[string]string{"amount_cents": "must be > 0"})
	}
	if !domain.ValidPaymentMethod(in.Method) {
		return Invoice{}, httperror.Validation(
			"method must be one of: "+strings.Join(domain.PaymentMethods, ", "),
			map[string]string{"method": "invalid"})
	}
	paidOnT, err := parseDate(in.PaidOn, "paid_on")
	if err != nil {
		return Invoice{}, err
	}
	paidOn := time.Now()
	if paidOnT != nil {
		paidOn = *paidOnT
	}
	err = db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusIssued {
			return httperror.Conflict("payments can only be recorded on issued invoices")
		}
		var total, paid int64
		if err := tx.QueryRow(ctx, `
			SELECT total_cents, COALESCE((SELECT sum(amount_cents) FROM payments WHERE invoice_id = $1), 0)
			  FROM invoices WHERE id = $1`, id).Scan(&total, &paid); err != nil {
			return err
		}
		if remaining := total - paid; in.AmountCents > remaining {
			return httperror.Validation(
				fmt.Sprintf("amount exceeds what is still owed (%d cents)", remaining),
				map[string]string{"amount_cents": "too high"})
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO payments (invoice_id, paid_on, amount_cents, method, reference, note)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			id, paidOn, in.AmountCents, in.Method, strings.TrimSpace(in.Reference), strings.TrimSpace(in.Note)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE invoices SET updated_at = now() WHERE id = $1`, id)
		return err
	})
	if err != nil {
		return Invoice{}, renderDBErr(err)
	}
	if err := s.audit.Record(ctx, actorName, "payment.recorded", "invoice", id,
		map[string]any{"amount_cents": in.AmountCents, "method": in.Method, "reference": in.Reference}); err != nil {
		return Invoice{}, err
	}
	return s.Get(ctx, id)
}

const summaryCols = `
	i.id, COALESCE(i.number, ''), i.customer_id, c.name, i.status, i.currency,
	i.issue_date, i.due_date, i.subtotal_cents, i.tax_cents, i.total_cents,
	COALESCE((SELECT sum(amount_cents) FROM payments WHERE invoice_id = i.id), 0),
	i.created_at, i.updated_at`

func scanSummary(row pgx.Row) (InvoiceSummary, error) {
	var s InvoiceSummary
	var issueDate, dueDate *time.Time
	err := row.Scan(&s.ID, &s.Number, &s.CustomerID, &s.CustomerName, &s.Status, &s.Currency,
		&issueDate, &dueDate, &s.SubtotalCents, &s.TaxCents, &s.TotalCents, &s.PaidCents,
		&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	if issueDate != nil {
		s.IssueDate = issueDate.Format(dateLayout)
	}
	if dueDate != nil {
		s.DueDate = dueDate.Format(dateLayout)
	}
	s.Status = domain.DeriveStatus(s.Status, s.TotalCents, s.PaidCents)
	s.Overdue = (s.Status == domain.StatusIssued || s.Status == domain.StatusPartial) &&
		dueDate != nil && dueDate.Before(time.Now().Truncate(24*time.Hour))
	return s, nil
}

// List returns invoices, newest first. status filters on the derived status;
// customerID narrows to one customer.
func (s *InvoiceService) List(ctx context.Context, status string, customerID *uuid.UUID) ([]InvoiceSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+summaryCols+`
		  FROM invoices i JOIN customers c ON c.id = i.customer_id
		 WHERE $1::uuid IS NULL OR i.customer_id = $1
		 ORDER BY i.created_at DESC`, customerID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []InvoiceSummary{}
	for rows.Next() {
		sum, err := scanSummary(rows)
		if err != nil {
			return nil, httperror.Internal(err)
		}
		if status != "" && sum.Status != status {
			continue
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// Get returns the full invoice with lines and payments.
func (s *InvoiceService) Get(ctx context.Context, id uuid.UUID) (Invoice, error) {
	var inv Invoice
	sum, err := scanSummary(s.pool.QueryRow(ctx, `
		SELECT `+summaryCols+`
		  FROM invoices i JOIN customers c ON c.id = i.customer_id
		 WHERE i.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return inv, httperror.NotFound("invoice")
	}
	if err != nil {
		return inv, httperror.Internal(err)
	}
	inv.InvoiceSummary = sum

	if err := s.pool.QueryRow(ctx, `SELECT notes FROM invoices WHERE id = $1`, id).Scan(&inv.Notes); err != nil {
		return inv, httperror.Internal(err)
	}

	lineRows, err := s.pool.Query(ctx, `
		SELECT id, description, quantity_hundredths, unit_price_cents, tax_rate_bp
		  FROM invoice_lines WHERE invoice_id = $1 ORDER BY position`, id)
	if err != nil {
		return inv, httperror.Internal(err)
	}
	defer lineRows.Close()
	inv.Lines = []Line{}
	for lineRows.Next() {
		var l Line
		var qtyH int64
		if err := lineRows.Scan(&l.ID, &l.Description, &qtyH, &l.UnitPriceCents, &l.TaxRateBp); err != nil {
			return inv, httperror.Internal(err)
		}
		l.Quantity = float64(qtyH) / 100
		l.SubtotalCents = domain.LineSubtotal(qtyH, l.UnitPriceCents)
		l.TaxCents = domain.LineTax(l.SubtotalCents, l.TaxRateBp)
		l.TotalCents = l.SubtotalCents + l.TaxCents
		inv.Lines = append(inv.Lines, l)
	}
	if err := lineRows.Err(); err != nil {
		return inv, httperror.Internal(err)
	}

	payRows, err := s.pool.Query(ctx, `
		SELECT id, paid_on, amount_cents, method, reference, note, created_at
		  FROM payments WHERE invoice_id = $1 ORDER BY paid_on, created_at`, id)
	if err != nil {
		return inv, httperror.Internal(err)
	}
	defer payRows.Close()
	inv.Payments = []Payment{}
	for payRows.Next() {
		var p Payment
		var paidOn time.Time
		if err := payRows.Scan(&p.ID, &paidOn, &p.AmountCents, &p.Method, &p.Reference, &p.Note, &p.CreatedAt); err != nil {
			return inv, httperror.Internal(err)
		}
		p.PaidOn = paidOn.Format(dateLayout)
		inv.Payments = append(inv.Payments, p)
	}
	return inv, payRows.Err()
}

// lockInvoice takes a row lock and returns the stored status.
func lockInvoice(ctx context.Context, tx pgx.Tx, id uuid.UUID) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM invoices WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httperror.NotFound("invoice")
	}
	return status, err
}

func isForeignKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23503")
}

// renderDBErr keeps typed errors and wraps everything else as internal.
func renderDBErr(err error) error {
	var typed *httperror.Error
	if errors.As(err, &typed) {
		return typed
	}
	return httperror.Internal(err)
}
