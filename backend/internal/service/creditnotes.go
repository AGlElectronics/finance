package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/domain"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/pdf"
)

// CreditNoteSummary is a credit note as listed on its invoice.
type CreditNoteSummary struct {
	ID            uuid.UUID `json:"id"`
	Number        string    `json:"number"`
	InvoiceID     uuid.UUID `json:"invoice_id"`
	IssueDate     string    `json:"issue_date"`
	Reason        string    `json:"reason"`
	Currency      string    `json:"currency"`
	SubtotalCents int64     `json:"subtotal_cents"`
	TaxCents      int64     `json:"tax_cents"`
	TotalCents    int64     `json:"total_cents"`
	RefundedCents int64     `json:"refunded_cents"`
	CreatedAt     time.Time `json:"created_at"`
}

// CreditNote is the detail shape.
type CreditNote struct {
	CreditNoteSummary
	InvoiceNumber string   `json:"invoice_number"`
	Customer      Party    `json:"customer"`
	Seller        Seller   `json:"seller"`
	Lines         []Line   `json:"lines"`
	Refunds       []Refund `json:"refunds"`
}

// Refund is money paid back against a credit note.
type Refund struct {
	ID           uuid.UUID `json:"id"`
	CreditNoteID uuid.UUID `json:"credit_note_id"`
	InvoiceID    uuid.UUID `json:"invoice_id"`
	RefundedOn   string    `json:"refunded_on"`
	AmountCents  int64     `json:"amount_cents"`
	Method       string    `json:"method"`
	Reference    string    `json:"reference"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreditNoteInput corrects an issued invoice. Either Full copies every
// line of an invoice that has no credit notes yet, or Lines lists what is
// credited.
type CreditNoteInput struct {
	Reason    string      `json:"reason"`
	IssueDate string      `json:"issue_date,omitempty"`
	Full      bool        `json:"full"`
	Lines     []LineInput `json:"lines"`
}

// RefundInput pays money back.
type RefundInput struct {
	RefundedOn  string `json:"refunded_on,omitempty"`
	AmountCents int64  `json:"amount_cents"`
	Method      string `json:"method"`
	Reference   string `json:"reference"`
	Note        string `json:"note"`
}

func (s *Service) normalizeCreditNote(in CreditNoteInput) (CreditNoteInput, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		return in, httperror.Validation("say why the invoice is corrected", map[string]string{"reason": "required"})
	}
	if len(in.Reason) > 2000 {
		return in, httperror.Validation("reason is too long", map[string]string{"reason": "at most 2000 characters"})
	}
	if in.Full && len(in.Lines) > 0 {
		return in, httperror.Validation("send either full or lines, not both", map[string]string{"lines": "not with full"})
	}
	if !in.Full {
		if _, _, _, err := checkLines(in.Lines); err != nil {
			return in, err
		}
	}
	d := s.today()
	if t, err := parseDate(in.IssueDate, "issue_date"); err != nil {
		return in, err
	} else if t != nil {
		d = *t
	}
	if err := s.checkBookingDate("issue_date", d); err != nil {
		return in, err
	}
	in.IssueDate = d.Format(dateLayout)
	return in, nil
}

// RequestCreditNote issues a credit note against an issued invoice, or asks
// for approval first when its total is above the approval threshold. A
// credit note lowers what the customer owes; money already paid beyond the
// new total is paid back with a refund.
func (s *Service) RequestCreditNote(ctx context.Context, who audit.Actor, idem Idem, invoiceID uuid.UUID, in CreditNoteInput) (Result, error) {
	in, err := s.normalizeCreditNote(in)
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
		total, _, err := s.execCreditNote(ctx, tx, who, invoiceID, in, true, nil)
		if err != nil {
			return err
		}
		if s.needsApproval(total) {
			res.Pending = true
			if res.ID, err = s.requestApproval(ctx, tx, who, ActionCreditNote, &invoiceID, total, in); err != nil {
				return err
			}
		} else if _, res.ID, err = s.execCreditNote(ctx, tx, who, invoiceID, in, false, nil); err != nil {
			return err
		}
		return finishIdem(ctx, tx, who, idem, res, "credit_note")
	})
	return res, err
}

// execCreditNote checks (dry) or issues a credit note; it returns the total
// and, when not dry, the new credit note's id.
func (s *Service) execCreditNote(ctx context.Context, tx pgx.Tx, who audit.Actor, invoiceID uuid.UUID, in CreditNoteInput, dry bool, approvalID *uuid.UUID) (int64, uuid.UUID, error) {
	status, err := lockInvoice(ctx, tx, invoiceID)
	if err != nil {
		return 0, uuid.Nil, err
	}
	if status != domain.StatusIssued {
		return 0, uuid.Nil, httperror.Conflict("credit notes can only correct issued invoices")
	}
	b, _, err := invoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return 0, uuid.Nil, err
	}
	lineIn := in.Lines
	if in.Full {
		if b.CreditedCents > 0 {
			return 0, uuid.Nil, httperror.Conflict("the invoice already has credit notes; list the lines to credit")
		}
		inv, err := s.loadInvoice(ctx, tx, invoiceID)
		if err != nil {
			return 0, uuid.Nil, err
		}
		for _, l := range inv.Lines {
			lineIn = append(lineIn, LineInput{Description: l.Description, Quantity: l.Quantity, UnitPriceCents: l.UnitPriceCents, TaxRateBp: l.TaxRateBp})
		}
	}
	lines, subtotal, tax, err := checkLines(lineIn)
	if err != nil {
		return 0, uuid.Nil, err
	}
	total := subtotal + tax
	if total <= 0 {
		return 0, uuid.Nil, httperror.Validation("a credit note must credit more than zero", nil)
	}
	if total > b.Creditable() {
		return 0, uuid.Nil, httperror.Validation(
			fmt.Sprintf("the credit note (%d cents) is more than what is left on the invoice (%d cents)", total, b.Creditable()),
			map[string]string{"lines": "too much"})
	}
	issue, _ := time.Parse(dateLayout, in.IssueDate)
	if err := ensureOpen(ctx, tx, issue); err != nil {
		return 0, uuid.Nil, err
	}
	if dry {
		return total, uuid.Nil, nil
	}

	var invNumber, currency string
	var cust Party
	var seller Seller
	if err := tx.QueryRow(ctx, `
		SELECT number, currency, customer_name, customer_email, customer_address, customer_tax_id,
		       seller_name, seller_address, seller_vat_number
		  FROM invoices WHERE id = $1`, invoiceID).
		Scan(&invNumber, &currency, &cust.Name, &cust.Email, &cust.Address, &cust.TaxID,
			&seller.Name, &seller.Address, &seller.VATNumber); err != nil {
		return 0, uuid.Nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, `UPDATE credit_note_counter SET last = last + 1 WHERE id = 1 RETURNING last`).Scan(&n); err != nil {
		return 0, uuid.Nil, err
	}
	number := fmt.Sprintf("%s%04d", s.opts.CreditNotePrefix, n)
	id := uuid.New()

	plines := make([]pdf.Line, 0, len(lines))
	for _, l := range lines {
		plines = append(plines, pdf.Line{Description: strings.TrimSpace(l.in.Description), QtyHundredths: l.qtyHundredths,
			UnitPriceCents: l.in.UnitPriceCents, TaxRateBp: l.in.TaxRateBp, SubtotalCents: l.subtotal, TaxCents: l.tax})
	}
	key := fmt.Sprintf("credit-notes/%s/%s.pdf", id, number)
	sum, err := s.storePDF(ctx, key, pdf.Document{
		Title: "Credit note", Number: number, IssueDate: in.IssueDate, Reference: invNumber, Reason: in.Reason,
		Seller:   pdf.Party{Name: seller.Name, Address: seller.Address, TaxID: seller.VATNumber},
		Customer: pdf.Party{Name: cust.Name, Address: cust.Address, Email: cust.Email, TaxID: cust.TaxID},
		Currency: currency, Lines: plines, Subtotal: subtotal, Tax: tax, Total: total,
	})
	if err != nil {
		return 0, uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO credit_notes (id, number, invoice_id, issue_date, reason, currency, subtotal_cents, tax_cents, total_cents,
		       customer_name, customer_email, customer_address, customer_tax_id,
		       seller_name, seller_address, seller_vat_number, pdf_key, pdf_sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		id, number, invoiceID, issue, in.Reason, currency, subtotal, tax, total,
		cust.Name, cust.Email, cust.Address, cust.TaxID, seller.Name, seller.Address, seller.VATNumber, key, sum); err != nil {
		return 0, uuid.Nil, err
	}
	for i, l := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO credit_note_lines (credit_note_id, position, description, quantity_hundredths, unit_price_cents, tax_rate_bp)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			id, i, strings.TrimSpace(l.in.Description), l.qtyHundredths, l.in.UnitPriceCents, l.in.TaxRateBp); err != nil {
			return 0, uuid.Nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE invoices SET updated_at = now() WHERE id = $1`, invoiceID); err != nil {
		return 0, uuid.Nil, err
	}
	return total, id, s.audit.Record(ctx, tx, who, audit.Event{
		Action: "credit_note.issued", EntityType: "credit_note", EntityID: id,
		After: map[string]any{"number": number, "invoice_id": invoiceID, "invoice_number": invNumber,
			"issue_date": in.IssueDate, "total_cents": total, "reason": in.Reason, "pdf_sha256": sum},
		Metadata: map[string]any{"approval_id": approvalID},
	})
}

func (s *Service) normalizeRefund(in RefundInput) (RefundInput, error) {
	if in.AmountCents <= 0 || in.AmountCents > domain.MaxAmountCents {
		return in, httperror.Validation("amount must be positive", map[string]string{"amount_cents": "must be > 0"})
	}
	if !domain.ValidPaymentMethod(in.Method) {
		return in, httperror.Validation(
			"method must be one of: "+strings.Join(domain.PaymentMethods, ", "),
			map[string]string{"method": "invalid"})
	}
	d := s.today()
	if t, err := parseDate(in.RefundedOn, "refunded_on"); err != nil {
		return in, err
	} else if t != nil {
		d = *t
	}
	if err := s.checkBookingDate("refunded_on", d); err != nil {
		return in, err
	}
	in.RefundedOn = d.Format(dateLayout)
	in.Reference, in.Note = strings.TrimSpace(in.Reference), strings.TrimSpace(in.Note)
	if len(in.Reference) > 200 || len(in.Note) > 2000 {
		return in, httperror.Validation("reference or note is too long", nil)
	}
	return in, nil
}

// RequestRefund pays money back against a credit note, or asks for approval
// first when the amount is above the approval threshold. The amount may not
// exceed what the customer overpaid after credit notes, nor the credit note.
func (s *Service) RequestRefund(ctx context.Context, who audit.Actor, idem Idem, creditNoteID uuid.UUID, in RefundInput) (Result, error) {
	in, err := s.normalizeRefund(in)
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
		if _, err := s.execRefund(ctx, tx, who, creditNoteID, in, true, nil); err != nil {
			return err
		}
		if s.needsApproval(in.AmountCents) {
			res.Pending = true
			if res.ID, err = s.requestApproval(ctx, tx, who, ActionRefund, &creditNoteID, in.AmountCents, in); err != nil {
				return err
			}
		} else if res.ID, err = s.execRefund(ctx, tx, who, creditNoteID, in, false, nil); err != nil {
			return err
		}
		return finishIdem(ctx, tx, who, idem, res, "refund")
	})
	return res, err
}

func (s *Service) execRefund(ctx context.Context, tx pgx.Tx, who audit.Actor, creditNoteID uuid.UUID, in RefundInput, dry bool, approvalID *uuid.UUID) (uuid.UUID, error) {
	var invoiceID uuid.UUID
	var noteTotal int64
	err := tx.QueryRow(ctx, `SELECT invoice_id, total_cents FROM credit_notes WHERE id = $1`, creditNoteID).Scan(&invoiceID, &noteTotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, httperror.NotFound("credit note")
	}
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := lockInvoice(ctx, tx, invoiceID); err != nil {
		return uuid.Nil, err
	}
	b, _, err := invoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return uuid.Nil, err
	}
	var noteRefunded int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_cents), 0) FROM refunds WHERE credit_note_id = $1`, creditNoteID).Scan(&noteRefunded); err != nil {
		return uuid.Nil, err
	}
	limit := b.Refundable()
	if left := noteTotal - noteRefunded; left < limit {
		limit = left
	}
	if in.AmountCents > limit {
		return uuid.Nil, httperror.Validation(
			fmt.Sprintf("amount exceeds what can be refunded (%d cents)", limit),
			map[string]string{"amount_cents": "too high"})
	}
	on, _ := time.Parse(dateLayout, in.RefundedOn)
	if err := ensureOpen(ctx, tx, on); err != nil {
		return uuid.Nil, err
	}
	if dry {
		return uuid.Nil, nil
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO refunds (credit_note_id, invoice_id, refunded_on, amount_cents, method, reference, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		creditNoteID, invoiceID, on, in.AmountCents, in.Method, in.Reference, in.Note).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE invoices SET updated_at = now() WHERE id = $1`, invoiceID); err != nil {
		return uuid.Nil, err
	}
	return id, s.audit.Record(ctx, tx, who, audit.Event{
		Action: "refund.recorded", EntityType: "credit_note", EntityID: creditNoteID, After: in,
		Metadata: map[string]any{"refund_id": id, "invoice_id": invoiceID, "approval_id": approvalID},
	})
}

const creditNoteSummaryCols = `cn.id, cn.number, cn.invoice_id, to_char(cn.issue_date, 'YYYY-MM-DD'), cn.reason, cn.currency,
	cn.subtotal_cents, cn.tax_cents, cn.total_cents,
	COALESCE((SELECT sum(amount_cents) FROM refunds r WHERE r.credit_note_id = cn.id), 0), cn.created_at`

func scanCreditNote(row pgx.Row, c *CreditNoteSummary, extra ...any) error {
	return row.Scan(append([]any{&c.ID, &c.Number, &c.InvoiceID, &c.IssueDate, &c.Reason, &c.Currency,
		&c.SubtotalCents, &c.TaxCents, &c.TotalCents, &c.RefundedCents, &c.CreatedAt}, extra...)...)
}

func (s *Service) creditNotesFor(ctx context.Context, invoiceID uuid.UUID) ([]CreditNoteSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+creditNoteSummaryCols+` FROM credit_notes cn WHERE cn.invoice_id = $1 ORDER BY cn.number`, invoiceID)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []CreditNoteSummary{}
	for rows.Next() {
		var c CreditNoteSummary
		if err := scanCreditNote(rows, &c); err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) refundsWhere(ctx context.Context, col string, id uuid.UUID) ([]Refund, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, credit_note_id, invoice_id, to_char(refunded_on, 'YYYY-MM-DD'), amount_cents, method, reference, note, created_at
		  FROM refunds WHERE `+col+` = $1 ORDER BY refunded_on, created_at`, id)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []Refund{}
	for rows.Next() {
		var r Refund
		if err := rows.Scan(&r.ID, &r.CreditNoteID, &r.InvoiceID, &r.RefundedOn, &r.AmountCents, &r.Method, &r.Reference, &r.Note, &r.CreatedAt); err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) refundsFor(ctx context.Context, invoiceID uuid.UUID) ([]Refund, error) {
	return s.refundsWhere(ctx, "invoice_id", invoiceID)
}

// GetCreditNote returns one credit note with its lines and refunds.
func (s *Service) GetCreditNote(ctx context.Context, id uuid.UUID) (CreditNote, error) {
	var c CreditNote
	err := scanCreditNote(s.pool.QueryRow(ctx, `
		SELECT `+creditNoteSummaryCols+`, i.number,
		       cn.customer_name, cn.customer_email, cn.customer_address, cn.customer_tax_id,
		       cn.seller_name, cn.seller_address, cn.seller_vat_number
		  FROM credit_notes cn JOIN invoices i ON i.id = cn.invoice_id WHERE cn.id = $1`, id),
		&c.CreditNoteSummary, &c.InvoiceNumber,
		&c.Customer.Name, &c.Customer.Email, &c.Customer.Address, &c.Customer.TaxID,
		&c.Seller.Name, &c.Seller.Address, &c.Seller.VATNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, httperror.NotFound("credit note")
	}
	if err != nil {
		return c, httperror.Internal(err)
	}
	if c.Lines, err = readLines(ctx, s.pool, `
		SELECT id, description, quantity_hundredths, unit_price_cents, tax_rate_bp
		  FROM credit_note_lines WHERE credit_note_id = $1 ORDER BY position`, id); err != nil {
		return c, httperror.Internal(err)
	}
	if c.Refunds, err = s.refundsWhere(ctx, "credit_note_id", id); err != nil {
		return c, err
	}
	return c, nil
}

// CreditNotePDF returns the stored PDF of a credit note and its file name.
func (s *Service) CreditNotePDF(ctx context.Context, id uuid.UUID) (string, []byte, error) {
	var number, key, sum string
	err := s.pool.QueryRow(ctx, `SELECT number, pdf_key, pdf_sha256 FROM credit_notes WHERE id = $1`, id).Scan(&number, &key, &sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, httperror.NotFound("credit note")
	}
	if err != nil {
		return "", nil, httperror.Internal(err)
	}
	body, err := s.fetchPDF(ctx, key, sum)
	return number + ".pdf", body, err
}
