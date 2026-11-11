package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/db"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
	"github.com/AGlElectronics/finance/backend/internal/docstore"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

const dateLayout = "2006-01-02"

// Options are the installation settings the services need (see config).
type Options struct {
	Currency               string
	DefaultVATRateBp       int32
	InvoicePrefix          string
	CreditNotePrefix       string
	ApprovalThresholdCents int64
	MaxBackdateDays        int
	// Store keeps issued PDFs; nil means not configured.
	Store docstore.Store
	// Den is the pairing with Den (a *denlink.Client); nil means standalone.
	Den DenLink
}

// DenLink is what the services read from the pairing with Den: the last
// synced bundle and its module settings.
type DenLink interface {
	Bundle() *denlink.Bundle
	ModuleSettings() *denlink.ModuleSettings
}

// Service owns every Finance table. Every write runs in one transaction
// together with its audit event.
type Service struct {
	pool  *pgxpool.Pool
	audit *audit.Writer
	opts  Options
	// now is the clock; tests replace it.
	now func() time.Time
}

// New returns a Service.
func New(pool *pgxpool.Pool, w *audit.Writer, opts Options) *Service {
	if opts.Store == nil {
		opts.Store = docstore.Unconfigured{}
	}
	if opts.InvoicePrefix == "" {
		opts.InvoicePrefix = "FIN-"
	}
	if opts.CreditNotePrefix == "" {
		opts.CreditNotePrefix = "FIN-CN-"
	}
	if opts.Currency == "" {
		opts.Currency = "EUR"
	}
	return &Service{pool: pool, audit: w, opts: opts, now: time.Now}
}

// SetClock replaces the clock (tests only).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Options returns the installation settings.
func (s *Service) Options() Options { return s.opts }

func (s *Service) today() time.Time {
	t := s.now()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return renderDBErr(db.WithTx(ctx, s.pool, fn))
}

// Page is limit/offset pagination. Limit defaults to 50 and is capped at 200.
type Page struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Normalize applies the defaults and caps.
func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// parseDate reads an optional YYYY-MM-DD date.
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

// checkBookingDate keeps a date that lands in the books between
// MaxBackdateDays ago and today.
func (s *Service) checkBookingDate(field string, d time.Time) error {
	today := s.today()
	if d.After(today) {
		return httperror.Validation(field+" cannot be in the future", map[string]string{field: "in the future"})
	}
	if earliest := today.AddDate(0, 0, -s.opts.MaxBackdateDays); d.Before(earliest) {
		return httperror.Validation(
			fmt.Sprintf("%s cannot be more than %d days in the past", field, s.opts.MaxBackdateDays),
			map[string]string{field: "too far in the past"})
	}
	return nil
}

// checkPlainDate rejects dates outside 2000-2100 (typos like 0026-11-01).
func checkPlainDate(field string, d *time.Time) error {
	if d == nil {
		return nil
	}
	if d.Year() < 2000 || d.Year() > 2100 {
		return httperror.Validation(field+" must be between 2000 and 2100", map[string]string{field: "out of range"})
	}
	return nil
}

const periodLockKey = 0x46494e50 // "FINP"

// ensureOpen fails when any of dates falls in a closed month. It takes a
// shared lock that ClosePeriod takes exclusively, so a month cannot close
// while a write dated in it is in flight.
func ensureOpen(ctx context.Context, tx pgx.Tx, dates ...time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, periodLockKey); err != nil {
		return err
	}
	months := make([]time.Time, 0, len(dates))
	for _, d := range dates {
		months = append(months, time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC))
	}
	var closed *time.Time
	err := tx.QueryRow(ctx, `SELECT min(month) FROM period_closures WHERE month = ANY($1::date[])`, months).Scan(&closed)
	if err != nil {
		return err
	}
	if closed != nil {
		return httperror.New(CodePeriodClosed,
			fmt.Sprintf("%s is closed; an admin must reopen it first", closed.Format("2006-01")))
	}
	return nil
}

// Idem identifies one create or payment request for safe retries: the
// Idempotency-Key header, the request path and a hash of the body.
type Idem struct {
	Key   string
	Scope string
	Hash  string
}

// NewIdem builds an Idem from the header value, the path and the raw body.
func NewIdem(key, scope string, body []byte) Idem {
	h := sha256.Sum256(body)
	return Idem{Key: strings.TrimSpace(key), Scope: scope, Hash: hex.EncodeToString(h[:])}
}

// Result is what a create or money request did.
type Result struct {
	// ID is the created or changed record; for a pending request it is the
	// approval.
	ID uuid.UUID
	// Pending is true when the request waits for a second person.
	Pending bool
	// Replayed is true when the Idempotency-Key was seen before and nothing
	// new happened.
	Replayed bool
}

// claimIdem reserves idem for who. When the key was used before it returns
// the earlier result (or an error when the request differs).
func claimIdem(ctx context.Context, tx pgx.Tx, who audit.Actor, idem Idem) (*Result, error) {
	if idem.Key == "" {
		return nil, httperror.New(CodeIdempotencyRequired, "send an Idempotency-Key header (any unique string, e.g. a UUID)")
	}
	if len(idem.Key) > 200 {
		return nil, httperror.BadRequest("Idempotency-Key is longer than 200 characters")
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (actor_issuer, actor_subject, key, scope, request_hash)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
		who.Issuer, subjectOrName(who), idem.Key, idem.Scope, idem.Hash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	var scope, hash, rtype string
	var rid *uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT scope, request_hash, resource_type, resource_id FROM idempotency_keys
		 WHERE actor_issuer = $1 AND actor_subject = $2 AND key = $3`,
		who.Issuer, subjectOrName(who), idem.Key).Scan(&scope, &hash, &rtype, &rid); err != nil {
		return nil, err
	}
	if scope != idem.Scope || hash != idem.Hash {
		return nil, httperror.New(CodeIdempotencyReused,
			"this Idempotency-Key was already used for a different request; use a new key")
	}
	if rid == nil {
		return nil, httperror.Conflict("the first request with this Idempotency-Key is still running")
	}
	return &Result{ID: *rid, Pending: rtype == "approval", Replayed: true}, nil
}

func finishIdem(ctx context.Context, tx pgx.Tx, who audit.Actor, idem Idem, res Result, resourceType string) error {
	if res.Pending {
		resourceType = "approval"
	}
	_, err := tx.Exec(ctx, `
		UPDATE idempotency_keys SET resource_type = $4, resource_id = $5
		 WHERE actor_issuer = $1 AND actor_subject = $2 AND key = $3`,
		who.Issuer, subjectOrName(who), idem.Key, resourceType, res.ID)
	return err
}

func subjectOrName(who audit.Actor) string {
	if who.Subject != "" {
		return who.Subject
	}
	return who.Name
}

func isForeignKey(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23503")
}

// renderDBErr keeps typed errors and wraps everything else as internal.
func renderDBErr(err error) error {
	if err == nil {
		return nil
	}
	var typed *httperror.Error
	if errors.As(err, &typed) {
		return typed
	}
	return httperror.Internal(err)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(dateLayout)
}
