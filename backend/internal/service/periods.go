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

// PeriodClosure is one closed month.
type PeriodClosure struct {
	Month    string      `json:"month"` // YYYY-MM
	ClosedAt time.Time   `json:"closed_at"`
	ClosedBy audit.Actor `json:"closed_by"`
}

// ReopenInput explains why a closed month is reopened.
type ReopenInput struct {
	Reason string `json:"reason"`
}

func parseMonth(s string) (time.Time, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return time.Time{}, httperror.Validation("month must look like 2026-10", map[string]string{"month": "invalid"})
	}
	return t, nil
}

// periodEntity gives each month a stable audit entity id.
func periodEntity(month time.Time) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("finance:period:"+month.Format("2006-01")))
}

// ListPeriods returns the closed months, newest first.
func (s *Service) ListPeriods(ctx context.Context) ([]PeriodClosure, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(month, 'YYYY-MM'), closed_at, closed_by, closed_issuer, closed_subject
		  FROM period_closures ORDER BY month DESC`)
	if err != nil {
		return nil, httperror.Internal(err)
	}
	defer rows.Close()
	out := []PeriodClosure{}
	for rows.Next() {
		var p PeriodClosure
		if err := rows.Scan(&p.Month, &p.ClosedAt, &p.ClosedBy.Name, &p.ClosedBy.Issuer, &p.ClosedBy.Subject); err != nil {
			return nil, httperror.Internal(err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ClosePeriod closes month (YYYY-MM). Nothing dated in it can change
// afterwards. Months after the current one cannot be closed.
func (s *Service) ClosePeriod(ctx context.Context, who audit.Actor, month string) (PeriodClosure, error) {
	m, err := parseMonth(month)
	if err != nil {
		return PeriodClosure{}, err
	}
	today := s.today()
	if m.After(time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		return PeriodClosure{}, httperror.Validation("a month in the future cannot be closed", map[string]string{"month": "in the future"})
	}
	var out PeriodClosure
	err = s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, periodLockKey); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO period_closures (month, closed_by, closed_issuer, closed_subject)
			VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, m, who.Name, who.Issuer, who.Subject)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httperror.Conflict(month + " is already closed")
		}
		if err := tx.QueryRow(ctx, `SELECT closed_at FROM period_closures WHERE month = $1`, m).Scan(&out.ClosedAt); err != nil {
			return err
		}
		out.Month, out.ClosedBy = month, who
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "period.closed", EntityType: "period", EntityID: periodEntity(m),
			Metadata: map[string]string{"month": month},
		})
	})
	return out, err
}

// ReopenPeriod reopens a closed month. Admins only (checked by the route);
// the reason is kept in the audit log.
func (s *Service) ReopenPeriod(ctx context.Context, who audit.Actor, month string, in ReopenInput) error {
	m, err := parseMonth(month)
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return httperror.Validation("say why the month is reopened", map[string]string{"reason": "required"})
	}
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, periodLockKey); err != nil {
			return err
		}
		var before PeriodClosure
		err := tx.QueryRow(ctx, `
			DELETE FROM period_closures WHERE month = $1
			RETURNING to_char(month, 'YYYY-MM'), closed_at, closed_by, closed_issuer, closed_subject`, m).
			Scan(&before.Month, &before.ClosedAt, &before.ClosedBy.Name, &before.ClosedBy.Issuer, &before.ClosedBy.Subject)
		if errors.Is(err, pgx.ErrNoRows) {
			return httperror.Conflict(month + " is not closed")
		}
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, who, audit.Event{
			Action: "period.reopened", EntityType: "period", EntityID: periodEntity(m),
			Before: before, Metadata: map[string]string{"month": month, "reason": reason},
		})
	})
}
