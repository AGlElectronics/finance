package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// LedgerEntry is one money movement: a payment coming in, or an expense or
// refund going out.
type LedgerEntry struct {
	ID  uuid.UUID `json:"id"`
	Day string    `json:"day"` // YYYY-MM-DD
	// Direction is "in" (payment) or "out" (expense, refund).
	Direction string `json:"direction"`
	// Kind is payment, expense or refund.
	Kind        string `json:"kind"`
	AmountCents int64  `json:"amount_cents"`
	// Counterparty is the customer (payment, refund) or vendor (expense).
	Counterparty string `json:"counterparty"`
	// Detail is the invoice number (payment), credit note number (refund)
	// or expense category (expense).
	Detail string `json:"detail"`
	Method string `json:"method"`
	// InvoiceID links payments and refunds back to their invoice.
	InvoiceID *uuid.UUID `json:"invoice_id,omitempty"`
	// BalanceCents is the running cash position after this entry.
	BalanceCents int64 `json:"balance_cents"`
}

// Ledger is one page of movements, newest first, plus the overall position.
type Ledger struct {
	InCents  int64         `json:"in_cents"`
	OutCents int64         `json:"out_cents"`
	NetCents int64         `json:"net_cents"`
	Entries  []LedgerEntry `json:"entries"`
	Total    int64         `json:"total"`
	Page
}

const ledgerMoves = `
	WITH moves AS (
		SELECT p.id, p.paid_on AS day, p.created_at, 'in' AS direction, 'payment' AS kind,
		       p.amount_cents, COALESCE(i.customer_name, c.name) AS counterparty,
		       COALESCE(i.number, '') AS detail, p.method, i.id AS invoice_id
		  FROM payments p
		  JOIN invoices i ON i.id = p.invoice_id
		  JOIN customers c ON c.id = i.customer_id
		UNION ALL
		SELECT r.id, r.refunded_on, r.created_at, 'out', 'refund',
		       r.amount_cents, cn.customer_name, cn.number, r.method, r.invoice_id
		  FROM refunds r
		  JOIN credit_notes cn ON cn.id = r.credit_note_id
		UNION ALL
		SELECT e.id, e.occurred_on, e.created_at, 'out', 'expense',
		       e.amount_cents, e.vendor, e.category, e.method, NULL
		  FROM expenses e
		 WHERE e.deleted_at IS NULL
	), running AS (
		SELECT *, sum(CASE WHEN direction = 'in' THEN amount_cents ELSE -amount_cents END)
		            OVER (ORDER BY day, created_at, id) AS balance
		  FROM moves
	)`

// GetLedger returns one page of the ledger with a running balance.
func (s *Service) GetLedger(ctx context.Context, p Page) (Ledger, error) {
	p = p.Normalize()
	out := Ledger{Entries: []LedgerEntry{}, Page: p}
	if err := s.pool.QueryRow(ctx, ledgerMoves+`
		SELECT count(*),
		       COALESCE(sum(amount_cents) FILTER (WHERE direction = 'in'), 0),
		       COALESCE(sum(amount_cents) FILTER (WHERE direction = 'out'), 0)
		  FROM moves`).Scan(&out.Total, &out.InCents, &out.OutCents); err != nil {
		return out, httperror.Internal(err)
	}
	out.NetCents = out.InCents - out.OutCents

	rows, err := s.pool.Query(ctx, ledgerMoves+`
		SELECT id, to_char(day, 'YYYY-MM-DD'), direction, kind, amount_cents,
		       counterparty, detail, method, invoice_id, balance
		  FROM running
		 ORDER BY day DESC, created_at DESC, id DESC
		 LIMIT $1 OFFSET $2`, p.Limit, p.Offset)
	if err != nil {
		return out, httperror.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.Day, &e.Direction, &e.Kind, &e.AmountCents,
			&e.Counterparty, &e.Detail, &e.Method, &e.InvoiceID, &e.BalanceCents); err != nil {
			return out, httperror.Internal(err)
		}
		out.Entries = append(out.Entries, e)
	}
	return out, rows.Err()
}
