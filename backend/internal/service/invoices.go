package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/docstore"
	"github.com/AGlElectronics/finance/backend/internal/domain"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/pdf"
)

// Line is one row on an invoice or credit note.
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

// Party is the customer as frozen on an issued invoice.
type Party struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Address string `json:"address"`
	TaxID   string `json:"tax_id"`
}

// InvoiceSummary is the list shape.
type InvoiceSummary struct {
	ID         uuid.UUID `json:"id"`
	Number     string    `json:"number,omitempty"`
	CustomerID uuid.UUID `json:"customer_id"`
	// CustomerName is the frozen name on issued invoices, the live name on drafts.
	CustomerName string `json:"customer_name"`
	// Status is derived: draft | issued | partial | paid | credited | void.
	Status    string `json:"status"`
	Currency  string `json:"currency"`
	IssueDate string `json:"issue_date,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
	Overdue   bool   `json:"overdue"`

	SubtotalCents int64 `json:"subtotal_cents"`
	TaxCents      int64 `json:"tax_cents"`
	TotalCents    int64 `json:"total_cents"`
	PaidCents     int64 `json:"paid_cents"`
	// CreditedCents is the sum of the invoice's credit notes.
	CreditedCents int64 `json:"credited_cents"`
	// RefundedCents is the money paid back against those credit notes.
	RefundedCents int64 `json:"refunded_cents"`
	// DueCents is what the customer still owes.
	DueCents int64 `json:"due_cents"`
	// RefundableCents is what was overpaid after credit notes.
	RefundableCents int64 `json:"refundable_cents"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Invoice is the detail shape.
type Invoice struct {
	InvoiceSummary
	Notes string `json:"notes"`
	// Customer is frozen at issue; on drafts it shows the live customer.
	Customer Party `json:"customer"`
	// Seller is frozen at issue; nil on drafts.
	Seller      *Seller             `json:"seller"`
	HasPDF      bool                `json:"has_pdf"`
	Lines       []Line              `json:"lines"`
	Payments    []Payment           `json:"payments"`
	CreditNotes []CreditNoteSummary `json:"credit_notes"`
	Refunds     []Refund            `json:"refunds"`
	// PendingApprovals are open four-eyes requests about this invoice.
	PendingApprovals []Approval `json:"pending_approvals"`
}

// LineInput is one line on a draft or credit note.
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

// IssueInput issues a draft. An empty issue date means today; an empty due
// date means the draft's due date, else 30 days after the issue date.
type IssueInput struct {
	IssueDate string `json:"issue_date,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
}

// VoidInput explains a void.
type VoidInput struct {
	Reason string `json:"reason"`
}

// PaymentInput records money received.
type PaymentInput struct {
	PaidOn      string `json:"paid_on,omitempty"`
	AmountCents int64  `json:"amount_cents"`
	Method      string `json:"method"`
	Reference   string `json:"reference"`
	Note        string `json:"note"`
}

// InvoiceFilter narrows ListInvoices.
type InvoiceFilter struct {
	Status     string
	CustomerID *uuid.UUID
	Page
}

// InvoicePage is one page of invoices.
type InvoicePage struct {
	Invoices []InvoiceSummary `json:"invoices"`
	Total    int64            `json:"total"`
	Page
}

type checkedLine struct {
	in            LineInput
	qtyHundredths int64
	subtotal      int64
	tax           int64
}

func checkLines(lines []LineInput) ([]checkedLine, int64, int64, error) {
	if len(lines) == 0 {
		return nil, 0, 0, httperror.Validation("at least one line is needed", nil)
	}
	if len(lines) > domain.MaxLines {
		return nil, 0, 0, httperror.Validation(fmt.Sprintf("at most %d lines", domain.MaxLines), nil)
	}
	out := make([]checkedLine, 0, len(lines))
	var subtotal, tax int64
	for i, l := range lines {
		issues := map[string]string{}
		if strings.TrimSpace(l.Description) == "" {
			issues["description"] = "required"
		} else if len(l.Description) > 1000 {
			issues["description"] = "at most 1000 characters"
		}
		qty := l.Quantity * 100
		if math.IsNaN(qty) || qty < 1 || qty > float64(domain.MaxQuantityHundredth) {
			issues["quantity"] = "must be between 0.01 and 1000000"
		}
		if l.UnitPriceCents < 0 || l.UnitPriceCents > domain.MaxUnitPriceCents {
			issues["unit_price_cents"] = fmt.Sprintf("must be between 0 and %d", domain.MaxUnitPriceCents)
		}
		if l.TaxRateBp < 0 || l.TaxRateBp > 10000 {
			issues["tax_rate_bp"] = "must be between 0 and 10000"
		}
		if len(issues) == 0 {
			qtyH := int64(math.Round(qty))
			if domain.LineSubtotal(qtyH, l.UnitPriceCents) > domain.MaxLineSubtotalCents {
				issues["unit_price_cents"] = "quantity times price is too large"
			}
		}
		if len(issues) > 0 {
			return nil, 0, 0, httperror.Validation(fmt.Sprintf("line %d is invalid", i+1), issues)
		}
		c := checkedLine{in: l, qtyHundredths: int64(math.Round(qty))}
		c.subtotal = domain.LineSubtotal(c.qtyHundredths, l.UnitPriceCents)
		c.tax = domain.LineTax(c.subtotal, l.TaxRateBp)
		subtotal += c.subtotal
		tax += c.tax
		out = append(out, c)
	}
	return out, subtotal, tax, nil
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

func checkDraft(in InvoiceInput) ([]checkedLine, int64, int64, *time.Time, error) {
	lines, subtotal, tax, err := checkLines(in.Lines)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	due, err := parseDate(in.DueDate, "due_date")
	if err != nil {
		return nil, 0, 0, nil, err
	}
	if err := checkPlainDate("due_date", due); err != nil {
		return nil, 0, 0, nil, err
	}
	if len(in.Notes) > 5000 {
		return nil, 0, 0, nil, httperror.Validation("notes are too long", map[string]string{"notes": "at most 5000 characters"})
	}
	return lines, subtotal, tax, due, nil
}

// CreateInvoice inserts a draft invoice with its lines and stored totals.
func (s *Service) CreateInvoice(ctx context.Context, who audit.Actor, idem Idem, in InvoiceInput) (Result, error) {
	lines, subtotal, tax, due, err := checkDraft(in)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if prev, err := claimIdem(ctx, tx, who, idem); err != nil || prev != nil {
			if prev != nil {
				res = *prev
			}
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO invoices (customer_id, due_date, notes, currency, subtotal_cents, tax_cents, total_cents)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
			in.CustomerID, due, strings.TrimSpace(in.Notes), s.opts.Currency, subtotal, tax, subtotal+tax).Scan(&res.ID); err != nil {
			if isForeignKey(err) {
				return httperror.Validation("customer does not exist", map[string]string{"customer_id": "unknown"})
			}
			return err
		}
		if err := writeLines(ctx, tx, res.ID, lines); err != nil {
			return err
		}
		if err := s.audit.Record(ctx, tx, who, audit.Event{
			Action: "invoice.created", EntityType: "invoice", EntityID: res.ID,
			After: map[string]any{"customer_id": in.CustomerID, "total_cents": subtotal + tax, "lines": len(lines)},
		}); err != nil {
			return err
		}
		return finishIdem(ctx, tx, who, idem, res, "invoice")
	})
	return res, err
}

// UpdateDraft rewrites a draft's customer, dates, notes and lines. Issued
// and void invoices are immutable.
func (s *Service) UpdateDraft(ctx context.Context, who audit.Actor, id uuid.UUID, in InvoiceInput) (Invoice, error) {
	lines, subtotal, tax, due, err := checkDraft(in)
	if err != nil {
		return Invoice{}, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusDraft {
			return httperror.Conflict("only draft invoices can be edited")
		}
		var before struct {
			CustomerID uuid.UUID `json:"customer_id"`
			DueDate    *string   `json:"due_date"`
			TotalCents int64     `json:"total_cents"`
			Notes      string    `json:"notes"`
		}
		if err := tx.QueryRow(ctx, `SELECT customer_id, to_char(due_date, 'YYYY-MM-DD'), total_cents, notes FROM invoices WHERE id = $1`, id).
			Scan(&before.CustomerID, &before.DueDate, &before.TotalCents, &before.Notes); err != nil {
			return err
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
		if err := writeLines(ctx, tx, id, lines); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "invoice.updated", EntityType: "invoice", EntityID: id, Before: before,
			After: map[string]any{"customer_id": in.CustomerID, "due_date": dateString(due), "total_cents": subtotal + tax,
				"notes": strings.TrimSpace(in.Notes), "lines": len(lines)},
		})
	})
	if err != nil {
		return Invoice{}, err
	}
	return s.GetInvoice(ctx, id)
}

// DeleteDraft removes a draft. Issued invoices are never deleted.
func (s *Service) DeleteDraft(ctx context.Context, who audit.Actor, id uuid.UUID) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusDraft {
			return httperror.Conflict("only drafts can be deleted; void the invoice or issue a credit note instead")
		}
		var total int64
		if err := tx.QueryRow(ctx, `DELETE FROM invoices WHERE id = $1 RETURNING total_cents`, id).Scan(&total); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "invoice.deleted", EntityType: "invoice", EntityID: id, Before: map[string]any{"total_cents": total},
		})
	})
}

// IssueInvoice turns a draft into an issued invoice: it takes the next
// number (FIN-0001), freezes the customer and seller details, renders the
// PDF and stores it. If storing the PDF fails nothing is issued, so the
// number series has no gaps.
func (s *Service) IssueInvoice(ctx context.Context, who audit.Actor, id uuid.UUID, in IssueInput) (Invoice, error) {
	issueT, err := parseDate(in.IssueDate, "issue_date")
	if err != nil {
		return Invoice{}, err
	}
	dueT, err := parseDate(in.DueDate, "due_date")
	if err != nil {
		return Invoice{}, err
	}
	issue := s.today()
	if issueT != nil {
		issue = *issueT
	}
	if err := s.checkBookingDate("issue_date", issue); err != nil {
		return Invoice{}, err
	}
	err = s.tx(ctx, func(tx pgx.Tx) error {
		status, err := lockInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != domain.StatusDraft {
			return httperror.Conflict("only drafts can be issued")
		}
		var draftDue *time.Time
		var lineCount int
		var cust Party
		if err := tx.QueryRow(ctx, `
			SELECT i.due_date, (SELECT count(*) FROM invoice_lines WHERE invoice_id = i.id),
			       c.name, c.email, c.billing_address, c.tax_id
			  FROM invoices i JOIN customers c ON c.id = i.customer_id WHERE i.id = $1`, id).
			Scan(&draftDue, &lineCount, &cust.Name, &cust.Email, &cust.Address, &cust.TaxID); err != nil {
			return err
		}
		if lineCount == 0 {
			return httperror.Validation("an invoice needs at least one line before it can be issued", nil)
		}
		due := issue.AddDate(0, 0, 30)
		if dueT != nil {
			due = *dueT
		} else if draftDue != nil {
			due = *draftDue
		}
		if due.Before(issue) {
			return httperror.Validation("due_date cannot be before issue_date", map[string]string{"due_date": "before issue date"})
		}
		if due.After(issue.AddDate(1, 0, 0)) {
			return httperror.Validation("due_date can be at most one year after issue_date", map[string]string{"due_date": "too late"})
		}
		if err := ensureOpen(ctx, tx, issue); err != nil {
			return err
		}
		seller, err := s.sellerForIssue(ctx, tx)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx,
			`UPDATE invoice_counter SET last = last + 1 WHERE id = 1 RETURNING last`).Scan(&n); err != nil {
			return err
		}
		number := fmt.Sprintf("%s%04d", s.opts.InvoicePrefix, n)

		inv, err := s.loadInvoice(ctx, tx, id)
		if err != nil {
			return err
		}
		doc := pdf.Document{
			Title: "Invoice", Number: number, IssueDate: issue.Format(dateLayout), DueDate: due.Format(dateLayout),
			Seller:   pdf.Party{Name: seller.Name, Address: seller.Address, TaxID: seller.VATNumber},
			Customer: pdf.Party{Name: cust.Name, Address: cust.Address, Email: cust.Email, TaxID: cust.TaxID},
			Currency: inv.Currency, Lines: pdfLines(inv.Lines),
			Subtotal: inv.SubtotalCents, Tax: inv.TaxCents, Total: inv.TotalCents, Notes: inv.Notes,
		}
		key := fmt.Sprintf("invoices/%s/%s.pdf", id, number)
		sum, err := s.storePDF(ctx, key, doc)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE invoices SET status = 'issued', number = $2, issue_date = $3, due_date = $4,
			       customer_name = $5, customer_email = $6, customer_address = $7, customer_tax_id = $8,
			       seller_name = $9, seller_address = $10, seller_vat_number = $11,
			       pdf_key = $12, pdf_sha256 = $13, issued_at = now(), updated_at = now()
			 WHERE id = $1`, id, number, issue, due,
			cust.Name, cust.Email, cust.Address, cust.TaxID,
			seller.Name, seller.Address, seller.VATNumber, key, sum); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "invoice.issued", EntityType: "invoice", EntityID: id,
			After: map[string]any{"number": number, "issue_date": issue.Format(dateLayout), "due_date": due.Format(dateLayout),
				"total_cents": inv.TotalCents, "customer": cust, "seller": seller, "pdf_sha256": sum},
		})
	})
	if err != nil {
		return Invoice{}, err
	}
	return s.GetInvoice(ctx, id)
}

func pdfLines(lines []Line) []pdf.Line {
	out := make([]pdf.Line, 0, len(lines))
	for _, l := range lines {
		out = append(out, pdf.Line{
			Description: l.Description, QtyHundredths: int64(math.Round(l.Quantity * 100)),
			UnitPriceCents: l.UnitPriceCents, TaxRateBp: l.TaxRateBp, SubtotalCents: l.SubtotalCents, TaxCents: l.TaxCents,
		})
	}
	return out
}

func (s *Service) storePDF(ctx context.Context, key string, doc pdf.Document) (string, error) {
	body := pdf.Render(doc)
	if err := s.opts.Store.Put(ctx, key, body, "application/pdf"); err != nil {
		if errors.Is(err, docstore.ErrNotConfigured) {
			return "", httperror.New(CodeStorageUnavailable, "PDF storage is not configured; set the S3_* settings")
		}
		return "", httperror.New(CodeStorageUnavailable, "the PDF could not be stored; nothing was issued, try again").WithCause(err)
	}
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:]), nil
}

// fetchPDF reads a stored PDF and checks it against its stored SHA-256.
func (s *Service) fetchPDF(ctx context.Context, key, sum string) ([]byte, error) {
	body, err := s.opts.Store.Get(ctx, key)
	if errors.Is(err, docstore.ErrNotConfigured) {
		return nil, httperror.New(CodeStorageUnavailable, "PDF storage is not configured; set the S3_* settings")
	}
	if errors.Is(err, docstore.ErrNotFound) {
		return nil, httperror.Internal(fmt.Errorf("stored PDF %s is missing", key))
	}
	if err != nil {
		return nil, httperror.New(CodeStorageUnavailable, "the PDF storage did not answer; try again").WithCause(err)
	}
	h := sha256.Sum256(body)
	if hex.EncodeToString(h[:]) != sum {
		return nil, httperror.Internal(fmt.Errorf("stored PDF %s does not match its SHA-256", key))
	}
	return body, nil
}

// InvoicePDF returns the stored PDF of an issued invoice and its file name.
func (s *Service) InvoicePDF(ctx context.Context, id uuid.UUID) (string, []byte, error) {
	var number, key, sum *string
	err := s.pool.QueryRow(ctx, `SELECT number, pdf_key, pdf_sha256 FROM invoices WHERE id = $1`, id).Scan(&number, &key, &sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, httperror.NotFound("invoice")
	}
	if err != nil {
		return "", nil, httperror.Internal(err)
	}
	if key == nil || sum == nil {
		return "", nil, httperror.NotFound("invoice PDF (drafts and invoices issued before PDFs existed have none)")
	}
	body, err := s.fetchPDF(ctx, *key, *sum)
	return deref(number) + ".pdf", body, err
}

// RequestVoid voids an issued invoice that has no payments and no credit
// notes, or asks for approval first when its total is above the approval
// threshold. Paid and partly paid invoices are corrected with credit notes.
func (s *Service) RequestVoid(ctx context.Context, who audit.Actor, id uuid.UUID, in VoidInput) (Result, error) {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return Result{}, httperror.Validation("say why the invoice is voided", map[string]string{"reason": "required"})
	}
	var res Result
	err := s.tx(ctx, func(tx pgx.Tx) error {
		total, err := s.execVoid(ctx, tx, who, id, reason, true, nil)
		if err != nil {
			return err
		}
		if s.needsApproval(total) {
			res.Pending = true
			res.ID, err = s.requestApproval(ctx, tx, who, ActionVoid, &id, total, voidPayload{Reason: reason})
			return err
		}
		res.ID = id
		_, err = s.execVoid(ctx, tx, who, id, reason, false, nil)
		return err
	})
	return res, err
}

type voidPayload struct {
	Reason string `json:"reason"`
}

// execVoid checks (dry) or performs a void and returns the invoice total.
func (s *Service) execVoid(ctx context.Context, tx pgx.Tx, who audit.Actor, id uuid.UUID, reason string, dry bool, approvalID *uuid.UUID) (int64, error) {
	status, err := lockInvoice(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	switch status {
	case domain.StatusDraft:
		return 0, httperror.Conflict("drafts are deleted, not voided")
	case domain.StatusVoid:
		return 0, httperror.Conflict("invoice is already void")
	}
	b, issueDate, err := invoiceBalance(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if b.PaidCents > 0 {
		return 0, httperror.Conflict("an invoice with payments can never be voided; issue a credit note (and a refund) instead")
	}
	if b.CreditedCents > 0 {
		return 0, httperror.Conflict("an invoice with credit notes cannot be voided; issue a credit note for the rest instead")
	}
	if err := ensureOpen(ctx, tx, issueDate); err != nil {
		return 0, err
	}
	if dry {
		return b.TotalCents, nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE invoices SET status = 'void', voided_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
		return 0, err
	}
	return b.TotalCents, s.audit.Record(ctx, tx, who, audit.Event{
		Action: "invoice.voided", EntityType: "invoice", EntityID: id,
		Before: map[string]string{"status": domain.StatusIssued}, After: map[string]string{"status": domain.StatusVoid},
		Metadata: map[string]any{"reason": reason, "total_cents": b.TotalCents, "approval_id": approvalID},
	})
}

// RecordPayment records money received against an issued invoice, or asks
// for approval first when the amount is above the approval threshold.
func (s *Service) RecordPayment(ctx context.Context, who audit.Actor, idem Idem, id uuid.UUID, in PaymentInput) (Result, error) {
	in, err := s.normalizePayment(in)
	if err != nil {
		return Result{}, err
	}
	var res Result
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if prev, err := claimIdem(ctx, tx, who, idem); err != nil || prev != nil {
			if prev != nil {
				res = *prev
			}
			return err
		}
		if _, err := s.execPayment(ctx, tx, who, id, in, true, nil); err != nil {
			return err
		}
		if s.needsApproval(in.AmountCents) {
			res.Pending = true
			if res.ID, err = s.requestApproval(ctx, tx, who, ActionPayment, &id, in.AmountCents, in); err != nil {
				return err
			}
		} else if res.ID, err = s.execPayment(ctx, tx, who, id, in, false, nil); err != nil {
			return err
		}
		return finishIdem(ctx, tx, who, idem, res, "payment")
	})
	return res, err
}

func (s *Service) normalizePayment(in PaymentInput) (PaymentInput, error) {
	if in.AmountCents <= 0 || in.AmountCents > domain.MaxAmountCents {
		return in, httperror.Validation("amount must be positive", map[string]string{"amount_cents": "must be > 0"})
	}
	if !domain.ValidPaymentMethod(in.Method) {
		return in, httperror.Validation(
			"method must be one of: "+strings.Join(domain.PaymentMethods, ", "),
			map[string]string{"method": "invalid"})
	}
	paidOn, err := parseDate(in.PaidOn, "paid_on")
	if err != nil {
		return in, err
	}
	d := s.today()
	if paidOn != nil {
		d = *paidOn
	}
	if err := s.checkBookingDate("paid_on", d); err != nil {
		return in, err
	}
	in.PaidOn = d.Format(dateLayout)
	in.Reference, in.Note = strings.TrimSpace(in.Reference), strings.TrimSpace(in.Note)
	if len(in.Reference) > 200 || len(in.Note) > 2000 {
		return in, httperror.Validation("reference or note is too long", nil)
	}
	return in, nil
}

// execPayment checks (dry) or records a payment. in is normalised.
func (s *Service) execPayment(ctx context.Context, tx pgx.Tx, who audit.Actor, id uuid.UUID, in PaymentInput, dry bool, approvalID *uuid.UUID) (uuid.UUID, error) {
	status, err := lockInvoice(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	if status != domain.StatusIssued {
		return uuid.Nil, httperror.Conflict("payments can only be recorded on issued invoices")
	}
	b, _, err := invoiceBalance(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	if due := b.Due(); in.AmountCents > due {
		return uuid.Nil, httperror.Validation(
			fmt.Sprintf("amount exceeds what is still owed (%d cents)", due),
			map[string]string{"amount_cents": "too high"})
	}
	paidOn, _ := time.Parse(dateLayout, in.PaidOn)
	if err := ensureOpen(ctx, tx, paidOn); err != nil {
		return uuid.Nil, err
	}
	if dry {
		return uuid.Nil, nil
	}
	var pid uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO payments (invoice_id, paid_on, amount_cents, method, reference, note)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		id, paidOn, in.AmountCents, in.Method, in.Reference, in.Note).Scan(&pid); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE invoices SET updated_at = now() WHERE id = $1`, id); err != nil {
		return uuid.Nil, err
	}
	return pid, s.audit.Record(ctx, tx, who, audit.Event{
		Action: "payment.recorded", EntityType: "invoice", EntityID: id, After: in,
		Metadata: map[string]any{"payment_id": pid, "approval_id": approvalID},
	})
}

// invoiceBalance sums the money on an invoice and returns its issue date.
func invoiceBalance(ctx context.Context, q querier, id uuid.UUID) (domain.Balance, time.Time, error) {
	var b domain.Balance
	var issue *time.Time
	err := q.QueryRow(ctx, `
		SELECT i.total_cents, i.issue_date,
		       COALESCE((SELECT sum(amount_cents) FROM payments WHERE invoice_id = i.id), 0),
		       COALESCE((SELECT sum(total_cents) FROM credit_notes WHERE invoice_id = i.id), 0),
		       COALESCE((SELECT sum(amount_cents) FROM refunds WHERE invoice_id = i.id), 0)
		  FROM invoices i WHERE i.id = $1`, id).
		Scan(&b.TotalCents, &issue, &b.PaidCents, &b.CreditedCents, &b.RefundedCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, time.Time{}, httperror.NotFound("invoice")
	}
	if issue == nil {
		return b, time.Time{}, err
	}
	return b, *issue, err
}

// invoiceBase is one row per invoice with its money sums; derivedStatus and
// the filters work on its columns.
const invoiceBase = `
	SELECT i.id, COALESCE(i.number, '') AS number, i.customer_id,
	       COALESCE(i.customer_name, c.name) AS customer_name, i.status, i.currency,
	       i.issue_date, i.due_date, i.subtotal_cents, i.tax_cents, i.total_cents,
	       COALESCE(p.cents, 0) AS paid, COALESCE(cn.cents, 0) AS credited, COALESCE(rf.cents, 0) AS refunded,
	       i.created_at, i.updated_at
	  FROM invoices i
	  JOIN customers c ON c.id = i.customer_id
	  LEFT JOIN LATERAL (SELECT sum(amount_cents) AS cents FROM payments WHERE invoice_id = i.id) p ON true
	  LEFT JOIN LATERAL (SELECT sum(total_cents) AS cents FROM credit_notes WHERE invoice_id = i.id) cn ON true
	  LEFT JOIN LATERAL (SELECT sum(amount_cents) AS cents FROM refunds WHERE invoice_id = i.id) rf ON true`

// derivedStatus mirrors domain.DeriveStatus in SQL over invoiceBase.
const derivedStatus = `
	CASE WHEN b.status <> 'issued' THEN b.status
	     WHEN b.total_cents > 0 AND b.total_cents - b.credited <= 0 THEN 'credited'
	     WHEN b.total_cents - b.credited > 0 AND b.paid - b.refunded >= b.total_cents - b.credited THEN 'paid'
	     WHEN b.paid - b.refunded > 0 THEN 'partial'
	     ELSE 'issued' END`

const summarySelect = `b.id, b.number, b.customer_id, b.customer_name, b.status, b.currency,
	b.issue_date, b.due_date, b.subtotal_cents, b.tax_cents, b.total_cents,
	b.paid, b.credited, b.refunded, b.created_at, b.updated_at`

func (s *Service) scanSummary(row pgx.Row) (InvoiceSummary, error) {
	var out InvoiceSummary
	var issueDate, dueDate *time.Time
	err := row.Scan(&out.ID, &out.Number, &out.CustomerID, &out.CustomerName, &out.Status, &out.Currency,
		&issueDate, &dueDate, &out.SubtotalCents, &out.TaxCents, &out.TotalCents,
		&out.PaidCents, &out.CreditedCents, &out.RefundedCents, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.IssueDate, out.DueDate = dateString(issueDate), dateString(dueDate)
	b := domain.Balance{TotalCents: out.TotalCents, CreditedCents: out.CreditedCents, PaidCents: out.PaidCents, RefundedCents: out.RefundedCents}
	out.Status = domain.DeriveStatus(out.Status, b)
	if out.Status != domain.StatusDraft && out.Status != domain.StatusVoid {
		out.DueCents, out.RefundableCents = b.Due(), b.Refundable()
	}
	out.Overdue = (out.Status == domain.StatusIssued || out.Status == domain.StatusPartial) &&
		dueDate != nil && dueDate.Before(s.today())
	return out, nil
}

var invoiceStatuses = map[string]bool{
	domain.StatusDraft: true, domain.StatusIssued: true, domain.StatusPartial: true,
	domain.StatusPaid: true, domain.StatusCredited: true, domain.StatusVoid: true,
}

// ListInvoices returns one page of invoices, newest first. Status filters on
// the derived status; CustomerID narrows to one customer.
func (s *Service) ListInvoices(ctx context.Context, f InvoiceFilter) (InvoicePage, error) {
	if f.Status != "" && !invoiceStatuses[f.Status] {
		return InvoicePage{}, httperror.BadRequest("unknown status " + f.Status)
	}
	f.Page = f.Normalize()
	out := InvoicePage{Invoices: []InvoiceSummary{}, Page: f.Page}
	where := `
		  FROM (` + invoiceBase + `) b
		 WHERE ($1::uuid IS NULL OR b.customer_id = $1)
		   AND ($2 = '' OR ` + derivedStatus + ` = $2)`
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+where, f.CustomerID, f.Status).Scan(&out.Total); err != nil {
		return out, httperror.Internal(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+summarySelect+where+`
		 ORDER BY b.created_at DESC, b.id DESC LIMIT $3 OFFSET $4`, f.CustomerID, f.Status, f.Limit, f.Offset)
	if err != nil {
		return out, httperror.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		sum, err := s.scanSummary(rows)
		if err != nil {
			return out, httperror.Internal(err)
		}
		out.Invoices = append(out.Invoices, sum)
	}
	return out, rows.Err()
}

// GetInvoice returns the full invoice with lines, money and open requests.
func (s *Service) GetInvoice(ctx context.Context, id uuid.UUID) (Invoice, error) {
	inv, err := s.loadInvoice(ctx, s.pool, id)
	if err != nil {
		return inv, renderDBErr(err)
	}
	if inv.CreditNotes, err = s.creditNotesFor(ctx, id); err != nil {
		return inv, err
	}
	if inv.Refunds, err = s.refundsFor(ctx, id); err != nil {
		return inv, err
	}
	if inv.PendingApprovals, err = s.pendingFor(ctx, id); err != nil {
		return inv, err
	}
	return inv, nil
}

type queryer interface {
	querier
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadInvoice reads an invoice with its lines and payments.
func (s *Service) loadInvoice(ctx context.Context, q queryer, id uuid.UUID) (Invoice, error) {
	var inv Invoice
	sum, err := s.scanSummary(q.QueryRow(ctx, `SELECT `+summarySelect+` FROM (`+invoiceBase+`) b WHERE b.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return inv, httperror.NotFound("invoice")
	}
	if err != nil {
		return inv, err
	}
	inv.InvoiceSummary = sum

	var sellerName, sellerAddress, sellerVAT, pdfKey *string
	if err := q.QueryRow(ctx, `
		SELECT i.notes,
		       COALESCE(i.customer_name, c.name), COALESCE(i.customer_email, c.email),
		       COALESCE(i.customer_address, c.billing_address), COALESCE(i.customer_tax_id, c.tax_id),
		       i.seller_name, i.seller_address, i.seller_vat_number, i.pdf_key
		  FROM invoices i JOIN customers c ON c.id = i.customer_id WHERE i.id = $1`, id).
		Scan(&inv.Notes, &inv.Customer.Name, &inv.Customer.Email, &inv.Customer.Address, &inv.Customer.TaxID,
			&sellerName, &sellerAddress, &sellerVAT, &pdfKey); err != nil {
		return inv, err
	}
	if sellerName != nil {
		inv.Seller = &Seller{Name: *sellerName, Address: deref(sellerAddress), VATNumber: deref(sellerVAT)}
	}
	inv.HasPDF = pdfKey != nil

	if inv.Lines, err = readLines(ctx, q, `
		SELECT id, description, quantity_hundredths, unit_price_cents, tax_rate_bp
		  FROM invoice_lines WHERE invoice_id = $1 ORDER BY position`, id); err != nil {
		return inv, err
	}

	payRows, err := q.Query(ctx, `
		SELECT id, paid_on, amount_cents, method, reference, note, created_at
		  FROM payments WHERE invoice_id = $1 ORDER BY paid_on, created_at`, id)
	if err != nil {
		return inv, err
	}
	defer payRows.Close()
	inv.Payments = []Payment{}
	for payRows.Next() {
		var p Payment
		var paidOn time.Time
		if err := payRows.Scan(&p.ID, &paidOn, &p.AmountCents, &p.Method, &p.Reference, &p.Note, &p.CreatedAt); err != nil {
			return inv, err
		}
		p.PaidOn = paidOn.Format(dateLayout)
		inv.Payments = append(inv.Payments, p)
	}
	return inv, payRows.Err()
}

func readLines(ctx context.Context, q queryer, sql string, id uuid.UUID) ([]Line, error) {
	rows, err := q.Query(ctx, sql, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Line{}
	for rows.Next() {
		var l Line
		var qtyH int64
		if err := rows.Scan(&l.ID, &l.Description, &qtyH, &l.UnitPriceCents, &l.TaxRateBp); err != nil {
			return nil, err
		}
		l.Quantity = float64(qtyH) / 100
		l.SubtotalCents = domain.LineSubtotal(qtyH, l.UnitPriceCents)
		l.TaxCents = domain.LineTax(l.SubtotalCents, l.TaxRateBp)
		l.TotalCents = l.SubtotalCents + l.TaxCents
		out = append(out, l)
	}
	return out, rows.Err()
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
