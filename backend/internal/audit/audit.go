// Package audit records every Finance state-changing write to an append-only
// log. Events are written inside the transaction of the change they describe,
// so a change is never committed without its audit row. Finance admins,
// approvers and read-only users read the log through GET /api/audit; it never
// stores secrets.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Actor is who performed a change: the display name plus the issuer and
// subject that identify the person at the IdP.
type Actor struct {
	Name    string `json:"name"`
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// SamePerson reports whether a and b are the same person. Issuer and
// subject decide when both carry them; otherwise the names are compared.
func (a Actor) SamePerson(b Actor) bool {
	if a.Subject != "" && b.Subject != "" {
		return a.Issuer == b.Issuer && a.Subject == b.Subject
	}
	return a.Name == b.Name
}

// Event is one audit row. Before and After hold the values around an update
// (nil when not applicable); Metadata holds anything else worth keeping.
type Event struct {
	Action     string
	EntityType string
	EntityID   uuid.UUID
	Before     any
	After      any
	Metadata   any
}

// Writer appends events to the audit_events table.
type Writer struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewWriter returns an audit Writer. pool is used only by RecordNow, for
// changes that have no database transaction of their own (Den pairing).
func NewWriter(pool *pgxpool.Pool, logger zerolog.Logger) *Writer {
	return &Writer{pool: pool, logger: logger.With().Str("component", "audit").Logger()}
}

// Record inserts ev inside tx. If it fails the caller's transaction must be
// rolled back, so return the error.
func (w *Writer) Record(ctx context.Context, tx pgx.Tx, who Actor, ev Event) error {
	if tx == nil {
		return fmt.Errorf("audit: no transaction")
	}
	before, err := marshal(ev.Before)
	if err != nil {
		return err
	}
	after, err := marshal(ev.After)
	if err != nil {
		return err
	}
	meta, err := marshal(ev.Metadata)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertSQL, who.Name, who.Issuer, who.Subject,
		ev.Action, ev.EntityType, ev.EntityID, meta, before, after); err != nil {
		return fmt.Errorf("audit insert: %w", err)
	}
	w.log(who, ev)
	return nil
}

// RecordNow inserts ev on its own, for changes made outside a Finance
// transaction (Den pairing is handled by the shared denlink package).
func (w *Writer) RecordNow(ctx context.Context, who Actor, ev Event) error {
	if w == nil || w.pool == nil {
		return fmt.Errorf("audit writer not initialised")
	}
	meta, err := marshal(ev.Metadata)
	if err != nil {
		return err
	}
	if _, err := w.pool.Exec(ctx, insertSQL, who.Name, who.Issuer, who.Subject,
		ev.Action, ev.EntityType, ev.EntityID, meta, nil, nil); err != nil {
		w.logger.Error().Err(err).Str("action", ev.Action).Msg("audit insert failed")
		return fmt.Errorf("audit insert: %w", err)
	}
	w.log(who, ev)
	return nil
}

const insertSQL = `
INSERT INTO audit_events (actor_name, actor_issuer, actor_subject, action, entity_type, entity_id, metadata, before, after)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb)`

func (w *Writer) log(who Actor, ev Event) {
	if w == nil {
		return
	}
	w.logger.Info().
		Str("actor", who.Name).
		Str("action", ev.Action).
		Str("entity_type", ev.EntityType).
		Str("entity_id", ev.EntityID.String()).
		Msg("audit event")
}

func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal audit value: %w", err)
	}
	return b, nil
}

// Entry is one audit row as the API returns it.
type Entry struct {
	ID         uuid.UUID       `json:"id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Actor      Actor           `json:"actor"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   uuid.UUID       `json:"entity_id"`
	Metadata   json.RawMessage `json:"metadata"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
}

// Filter narrows List. Empty fields match everything.
type Filter struct {
	EntityType string
	EntityID   *uuid.UUID
	Action     string
	Actor      string
	Limit      int
	Offset     int
}

// List returns audit events newest first plus the number of matching rows.
func (w *Writer) List(ctx context.Context, f Filter) ([]Entry, int64, error) {
	const where = `
		 WHERE ($1 = '' OR entity_type = $1)
		   AND ($2::uuid IS NULL OR entity_id = $2)
		   AND ($3 = '' OR action = $3 OR action LIKE $3 || '.%')
		   AND ($4 = '' OR actor_name ILIKE '%' || $4 || '%' OR actor_subject = $4)`
	var total int64
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`+where,
		f.EntityType, f.EntityID, f.Action, f.Actor).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := w.pool.Query(ctx, `
		SELECT id, occurred_at, actor_name, actor_issuer, actor_subject, action, entity_type, entity_id,
		       COALESCE(metadata, 'null'::jsonb), COALESCE(before, 'null'::jsonb), COALESCE(after, 'null'::jsonb)
		  FROM audit_events`+where+`
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT $5 OFFSET $6`, f.EntityType, f.EntityID, f.Action, f.Actor, f.Limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.Actor.Name, &e.Actor.Issuer, &e.Actor.Subject,
			&e.Action, &e.EntityType, &e.EntityID, &e.Metadata, &e.Before, &e.After); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
