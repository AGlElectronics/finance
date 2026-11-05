package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// LedgerEntry is one money movement: an invoice payment coming in or an
// expense going out.
type LedgerEntry struct {
	ID  uuid.UUID `json:"id"`
	Day string    `json:"day"` // YYYY-MM-DD
	// Direction is "in" (payment) or "out" (expense).
	Direction   string `json:"direction"`
	AmountCents int64  `json:"amount_cents"`
	// Counterparty is the customer (in) or vendor (out).
	Counterparty string `json:"counterparty"`
	// Detail is the invoice number (in) or expense category (out).
	Detail string `json:"detail"`
	Method string `json:"method"`
	// InvoiceID links payments back to their invoice.
	InvoiceID *uuid.UUID `json:"invoice_id,omitempty"`
	// BalanceCents is the running cash position after this entry.
	BalanceCents int64 `json:"balance_cents"`
}

// Ledger is every movement, newest first, plus the overall position.
type Ledger struct {
	InCents  int64         `json:"in_cents"`
	OutCents int64         `json:"out_cents"`
	NetCents int64         `json:"net_cents"`
	Entries  []LedgerEntry `json:"entries"`
}

// LedgerService reads payments and expenses as one stream.
type LedgerService struct {
	pool *pgxpool.Pool
}

func NewLedgerService(pool *pgxpool.Pool) *LedgerService {
	return &LedgerService{pool: pool}
}

// Get returns the full ledger with a running balance.
func (s *LedgerService) Get(ctx context.Context) (Ledger, error) {
	rows, err := s.pool.Query(ctx, `
		WITH moves AS (
			SELECT p.id, p.paid_on AS day, p.created_at, 'in' AS direction,
			       p.amount_cents, c.name AS counterparty,
			       COALESCE(i.number, '') AS detail, p.method, i.id AS invoice_id
			  FROM payments p
			  JOIN invoices i ON i.id = p.invoice_id
			  JOIN customers c ON c.id = i.customer_id
			UNION ALL
			SELECT e.id, e.occurred_on, e.created_at, 'out',
			       e.amount_cents, e.vendor, e.category, e.method, NULL
			  FROM expenses e
		)
		SELECT id, to_char(day, 'YYYY-MM-DD'), direction, amount_cents,
		       counterparty, detail, method, invoice_id,
		       sum(CASE WHEN direction = 'in' THEN amount_cents ELSE -amount_cents END)
		         OVER (ORDER BY day, created_at, id)
		  FROM moves
		 ORDER BY day DESC, created_at DESC, id DESC`)
	if err != nil {
		return Ledger{}, httperror.Internal(err)
	}
	defer rows.Close()

	out := Ledger{Entries: []LedgerEntry{}}
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.Day, &e.Direction, &e.AmountCents,
			&e.Counterparty, &e.Detail, &e.Method, &e.InvoiceID, &e.BalanceCents); err != nil {
			return Ledger{}, httperror.Internal(err)
		}
		if e.Direction == "in" {
			out.InCents += e.AmountCents
		} else {
			out.OutCents += e.AmountCents
		}
		out.Entries = append(out.Entries, e)
	}
	out.NetCents = out.InCents - out.OutCents
	return out, rows.Err()
}
