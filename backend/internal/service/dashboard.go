package service

import (
	"context"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Dashboard is the money position shown on the home page.
type Dashboard struct {
	// OutstandingCents is what customers still owe (after credit notes).
	OutstandingCents int64 `json:"outstanding_cents"`
	// OverdueCents / OverdueCount cover issued invoices past their due date.
	OverdueCents int64 `json:"overdue_cents"`
	OverdueCount int64 `json:"overdue_count"`
	// Cash received, this calendar month and all time.
	ReceivedMonthCents int64 `json:"received_month_cents"`
	ReceivedTotalCents int64 `json:"received_total_cents"`
	// Cash spent (expenses and refunds), this calendar month and all time.
	SpentMonthCents int64 `json:"spent_month_cents"`
	SpentTotalCents int64 `json:"spent_total_cents"`
	// NetTotalCents is the cash position: received minus spent, all time.
	NetTotalCents int64 `json:"net_total_cents"`

	DraftCount int64 `json:"draft_count"`
	// OpenCount is issued invoices not yet fully paid.
	OpenCount int64 `json:"open_count"`
	// PendingApprovals is the number of requests waiting for a second person.
	PendingApprovals int64 `json:"pending_approvals"`

	// Months is money in vs out for the last six calendar months.
	Months []MonthFlow `json:"months"`

	RecentInvoices []InvoiceSummary `json:"recent_invoices"`
}

// MonthFlow is one month of cash in and out.
type MonthFlow struct {
	Month    string `json:"month"` // YYYY-MM
	InCents  int64  `json:"in_cents"`
	OutCents int64  `json:"out_cents"`
}

const cashOut = `
	SELECT occurred_on AS day, amount_cents FROM expenses WHERE deleted_at IS NULL
	UNION ALL
	SELECT refunded_on, amount_cents FROM refunds`

// GetDashboard computes the dashboard.
func (s *Service) GetDashboard(ctx context.Context) (Dashboard, error) {
	var d Dashboard
	today := s.today()
	err := s.pool.QueryRow(ctx, `
		WITH open_invoices AS (
			SELECT b.id, b.due_date, b.total_cents - b.credited - (b.paid - b.refunded) AS remaining
			  FROM (`+invoiceBase+`) b
			 WHERE b.status = 'issued' AND b.total_cents - b.credited - (b.paid - b.refunded) > 0
		), out_moves AS (`+cashOut+`)
		SELECT
			COALESCE((SELECT sum(remaining) FROM open_invoices), 0),
			COALESCE((SELECT sum(remaining) FROM open_invoices WHERE due_date < $1), 0),
			(SELECT count(*) FROM open_invoices WHERE due_date < $1),
			COALESCE((SELECT sum(amount_cents) FROM payments WHERE paid_on >= date_trunc('month', $1::date)), 0),
			COALESCE((SELECT sum(amount_cents) FROM payments), 0),
			COALESCE((SELECT sum(amount_cents) FROM out_moves WHERE day >= date_trunc('month', $1::date)), 0),
			COALESCE((SELECT sum(amount_cents) FROM out_moves), 0),
			(SELECT count(*) FROM invoices WHERE status = 'draft'),
			(SELECT count(*) FROM open_invoices),
			(SELECT count(*) FROM approvals WHERE status = 'pending')`, today).Scan(
		&d.OutstandingCents, &d.OverdueCents, &d.OverdueCount,
		&d.ReceivedMonthCents, &d.ReceivedTotalCents,
		&d.SpentMonthCents, &d.SpentTotalCents, &d.DraftCount, &d.OpenCount, &d.PendingApprovals)
	if err != nil {
		return d, httperror.Internal(err)
	}
	d.NetTotalCents = d.ReceivedTotalCents - d.SpentTotalCents

	if d.Months, err = s.months(ctx); err != nil {
		return d, err
	}
	if d.RecentInvoices, err = s.recent(ctx, 8); err != nil {
		return d, err
	}
	return d, nil
}

// months is cash in vs out for the last six calendar months (oldest first).
func (s *Service) months(ctx context.Context) ([]MonthFlow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(m, 'YYYY-MM'), COALESCE(pin.cents, 0), COALESCE(pout.cents, 0)
		  FROM generate_series(date_trunc('month', $1::date) - interval '5 months',
		                       date_trunc('month', $1::date), '1 month') AS m
		  LEFT JOIN (SELECT date_trunc('month', paid_on) AS mo, sum(amount_cents) AS cents
		               FROM payments GROUP BY 1) pin ON pin.mo = m
		  LEFT JOIN (SELECT date_trunc('month', day) AS mo, sum(amount_cents) AS cents
		               FROM (`+cashOut+`) o GROUP BY 1) pout ON pout.mo = m
		 ORDER BY m`, s.today())
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []MonthFlow{}
	for rows.Next() {
		var mf MonthFlow
		if err := rows.Scan(&mf.Month, &mf.InCents, &mf.OutCents); err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, mf)
	}
	return out, rows.Err()
}

func (s *Service) recent(ctx context.Context, limit int) ([]InvoiceSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+summarySelect+` FROM (`+invoiceBase+`) b
		 ORDER BY b.updated_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []InvoiceSummary{}
	for rows.Next() {
		sum, err := s.scanSummary(rows)
		if err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}
