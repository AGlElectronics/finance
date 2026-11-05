-- Finance initial schema.
--
--   - `customers` is who an invoice is addressed to.
--   - `invoices` + `invoice_lines`: a draft is editable; issuing takes the
--     next number from `invoice_counter` (FIN-0001) and locks the lines.
--     Stored status is only draft | issued | void; "partial" and "paid" are
--     derived from payments at read time.
--   - `payments` records money received against an issued invoice. All money
--     is integer cents (EUR); quantities are hundredths; tax rates are
--     basis points (21% = 2100).
--   - `audit_events` captures every state-changing write (same shape as HR).

-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd

CREATE TABLE customers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    email           TEXT NOT NULL DEFAULT '',
    billing_address TEXT NOT NULL DEFAULT '',
    tax_id          TEXT NOT NULL DEFAULT '',
    notes           TEXT NOT NULL DEFAULT '',
    archived_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sequential invoice numbers with no gaps: the counter row is bumped inside
-- the issue transaction (a Postgres SEQUENCE would leak numbers on rollback).
CREATE TABLE invoice_counter (
    id   SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last INTEGER NOT NULL DEFAULT 0
);
INSERT INTO invoice_counter (id, last) VALUES (1, 0);

CREATE TABLE invoices (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id    UUID NOT NULL REFERENCES customers(id),
    number         TEXT UNIQUE,  -- NULL until issued
    status         TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'issued', 'void')),
    currency       TEXT NOT NULL DEFAULT 'EUR',
    issue_date     DATE,
    due_date       DATE,
    notes          TEXT NOT NULL DEFAULT '',
    subtotal_cents BIGINT NOT NULL DEFAULT 0,
    tax_cents      BIGINT NOT NULL DEFAULT 0,
    total_cents    BIGINT NOT NULL DEFAULT 0,
    issued_at      TIMESTAMPTZ,
    voided_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX invoices_customer_idx ON invoices (customer_id);
CREATE INDEX invoices_status_idx   ON invoices (status);

CREATE TABLE invoice_lines (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id          UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    position            INTEGER NOT NULL DEFAULT 0,
    description         TEXT NOT NULL,
    quantity_hundredths BIGINT NOT NULL DEFAULT 100 CHECK (quantity_hundredths > 0),
    unit_price_cents    BIGINT NOT NULL DEFAULT 0,
    tax_rate_bp         INTEGER NOT NULL DEFAULT 0 CHECK (tax_rate_bp BETWEEN 0 AND 10000),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id, position);

CREATE TABLE payments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id   UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    paid_on      DATE NOT NULL DEFAULT current_date,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    method       TEXT NOT NULL DEFAULT 'bank_transfer',
    -- reference is what a future bank feed would match transactions against.
    reference    TEXT NOT NULL DEFAULT '',
    note         TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX payments_invoice_idx ON payments (invoice_id);
CREATE INDEX payments_paid_on_idx ON payments (paid_on DESC);

CREATE TABLE audit_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_name    TEXT NOT NULL,
    action        TEXT NOT NULL,
    entity_type   TEXT NOT NULL,
    entity_id     UUID NOT NULL,
    metadata      JSONB,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_entity_idx   ON audit_events (entity_type, entity_id);
CREATE INDEX audit_events_actor_idx    ON audit_events (actor_name);
CREATE INDEX audit_events_occurred_idx ON audit_events (occurred_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS invoice_counter;
DROP TABLE IF EXISTS customers;
