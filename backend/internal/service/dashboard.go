package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Dashboard is the money position shown on the home page.
type Dashboard struct {
	// OutstandingCents is what customers still owe (issued minus paid).
	OutstandingCents int64 `json:"outstanding_cents"`
	// OverdueCents / OverdueCount cover issued invoices past their due date.
	OverdueCents int64 `json:"overdue_cents"`
	OverdueCount int64 `json:"overdue_count"`
	// Cash received, this calendar month and all time.
	ReceivedMonthCents int64 `json:"received_month_cents"`
	ReceivedTotalCents int64 `json:"received_total_cents"`

	DraftCount int64 `json:"draft_count"`
	// OpenCount is issued invoices not yet fully paid.
	OpenCount int64 `json:"open_count"`

	RecentInvoices []InvoiceSummary `json:"recent_invoices"`
}

// DashboardService aggregates invoices and payments.
type DashboardService struct {
	pool     *pgxpool.Pool
	invoices *InvoiceService
}

func NewDashboardService(pool *pgxpool.Pool, invoices *InvoiceService) *DashboardService {
	return &DashboardService{pool: pool, invoices: invoices}
}

// Get computes the dashboard.
func (s *DashboardService) Get(ctx context.Context) (Dashboard, error) {
	var d Dashboard
	err := s.pool.QueryRow(ctx, `
		WITH paid AS (
			SELECT invoice_id, sum(amount_cents) AS cents FROM payments GROUP BY invoice_id
		), open_invoices AS (
			SELECT i.id, i.due_date, i.total_cents - COALESCE(p.cents, 0) AS remaining
			  FROM invoices i LEFT JOIN paid p ON p.invoice_id = i.id
			 WHERE i.status = 'issued' AND i.total_cents - COALESCE(p.cents, 0) > 0
		)
		SELECT
			COALESCE((SELECT sum(remaining) FROM open_invoices), 0),
			COALESCE((SELECT sum(remaining) FROM open_invoices WHERE due_date < current_date), 0),
			(SELECT count(*) FROM open_invoices WHERE due_date < current_date),
			COALESCE((SELECT sum(amount_cents) FROM payments
			           WHERE paid_on >= date_trunc('month', current_date)), 0),
			COALESCE((SELECT sum(amount_cents) FROM payments), 0),
			(SELECT count(*) FROM invoices WHERE status = 'draft'),
			(SELECT count(*) FROM open_invoices)`).Scan(
		&d.OutstandingCents, &d.OverdueCents, &d.OverdueCount,
		&d.ReceivedMonthCents, &d.ReceivedTotalCents, &d.DraftCount, &d.OpenCount)
	if err != nil {
		return d, httperror.Internal(err)
	}

	recent, err := s.recent(ctx, 8)
	if err != nil {
		return d, err
	}
	d.RecentInvoices = recent
	return d, nil
}

func (s *DashboardService) recent(ctx context.Context, limit int) ([]InvoiceSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+summaryCols+`
		  FROM invoices i JOIN customers c ON c.id = i.customer_id
		 ORDER BY i.updated_at DESC LIMIT $1`, limit)
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
		out = append(out, sum)
	}
	return out, rows.Err()
}
