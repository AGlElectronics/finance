-- Money integrity for 1.0.0.
--
--   - `audit_events` records who acted (issuer + subject, not only a name)
--     and the values before and after a change. Rows are append-only.
--   - Issued invoices keep a frozen copy of the customer and seller details
--     and the key and SHA-256 of the PDF stored in S3.
--   - `credit_notes` correct issued invoices in their own gap-free series
--     (FIN-CN-0001); `refunds` pay money back against a credit note.
--   - `expenses` are soft-deleted (`deleted_at`), never removed.
--   - `period_closures` holds closed months; nothing dated in a closed month
--     may change.
--   - `approvals` holds four-eyes requests (voids, payments, expenses,
--     credit notes, refunds) until a second person decides.
--   - `idempotency_keys` makes create and payment requests safe to retry.
--   - `settings` holds Finance's own seller details (override Den's).

-- +goose Up
ALTER TABLE audit_events
    ADD COLUMN actor_issuer  TEXT NOT NULL DEFAULT '',
    ADD COLUMN actor_subject TEXT NOT NULL DEFAULT '',
    ADD COLUMN before        JSONB,
    ADD COLUMN after         JSONB;

CREATE INDEX audit_events_action_idx ON audit_events (action);
CREATE INDEX audit_events_subject_idx ON audit_events (actor_issuer, actor_subject);

-- +goose StatementBegin
CREATE FUNCTION finance_audit_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION finance_audit_append_only();

ALTER TABLE invoices
    ADD COLUMN customer_name     TEXT,
    ADD COLUMN customer_email    TEXT,
    ADD COLUMN customer_address  TEXT,
    ADD COLUMN customer_tax_id   TEXT,
    ADD COLUMN seller_name       TEXT,
    ADD COLUMN seller_address    TEXT,
    ADD COLUMN seller_vat_number TEXT,
    ADD COLUMN pdf_key           TEXT,
    ADD COLUMN pdf_sha256        TEXT;

-- Back-fill the snapshot of invoices issued before this migration.
UPDATE invoices i
   SET customer_name = c.name, customer_email = c.email,
       customer_address = c.billing_address, customer_tax_id = c.tax_id,
       seller_name = '', seller_address = '', seller_vat_number = ''
  FROM customers c
 WHERE c.id = i.customer_id AND i.status <> 'draft';

-- Issued and void invoices are frozen: only the void stamp and the PDF
-- reference may still change.
-- +goose StatementBegin
CREATE FUNCTION finance_invoice_frozen() RETURNS trigger AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.number IS DISTINCT FROM OLD.number OR
        NEW.customer_id IS DISTINCT FROM OLD.customer_id OR
        NEW.currency IS DISTINCT FROM OLD.currency OR
        NEW.issue_date IS DISTINCT FROM OLD.issue_date OR
        NEW.due_date IS DISTINCT FROM OLD.due_date OR
        NEW.subtotal_cents IS DISTINCT FROM OLD.subtotal_cents OR
        NEW.tax_cents IS DISTINCT FROM OLD.tax_cents OR
        NEW.total_cents IS DISTINCT FROM OLD.total_cents OR
        NEW.customer_name IS DISTINCT FROM OLD.customer_name OR
        NEW.customer_email IS DISTINCT FROM OLD.customer_email OR
        NEW.customer_address IS DISTINCT FROM OLD.customer_address OR
        NEW.customer_tax_id IS DISTINCT FROM OLD.customer_tax_id OR
        NEW.seller_name IS DISTINCT FROM OLD.seller_name OR
        NEW.seller_address IS DISTINCT FROM OLD.seller_address OR
        NEW.seller_vat_number IS DISTINCT FROM OLD.seller_vat_number OR
        (OLD.status = 'void' AND NEW.status <> 'void') OR
        (OLD.pdf_sha256 IS NOT NULL AND NEW.pdf_sha256 IS DISTINCT FROM OLD.pdf_sha256)
    ) THEN
        RAISE EXCEPTION 'issued invoice % is frozen', OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER invoices_frozen
    BEFORE UPDATE ON invoices
    FOR EACH ROW EXECUTE FUNCTION finance_invoice_frozen();

CREATE TABLE credit_note_counter (
    id   SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last INTEGER NOT NULL DEFAULT 0
);
INSERT INTO credit_note_counter (id, last) VALUES (1, 0);

CREATE TABLE credit_notes (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    number            TEXT NOT NULL UNIQUE,
    invoice_id        UUID NOT NULL REFERENCES invoices(id),
    issue_date        DATE NOT NULL,
    reason            TEXT NOT NULL,
    currency          TEXT NOT NULL,
    subtotal_cents    BIGINT NOT NULL,
    tax_cents         BIGINT NOT NULL,
    total_cents       BIGINT NOT NULL CHECK (total_cents > 0),
    customer_name     TEXT NOT NULL,
    customer_email    TEXT NOT NULL,
    customer_address  TEXT NOT NULL,
    customer_tax_id   TEXT NOT NULL,
    seller_name       TEXT NOT NULL,
    seller_address    TEXT NOT NULL,
    seller_vat_number TEXT NOT NULL,
    pdf_key           TEXT NOT NULL,
    pdf_sha256        TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX credit_notes_invoice_idx ON credit_notes (invoice_id);

CREATE TABLE credit_note_lines (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credit_note_id      UUID NOT NULL REFERENCES credit_notes(id),
    position            INTEGER NOT NULL,
    description         TEXT NOT NULL,
    quantity_hundredths BIGINT NOT NULL CHECK (quantity_hundredths > 0),
    unit_price_cents    BIGINT NOT NULL CHECK (unit_price_cents >= 0),
    tax_rate_bp         INTEGER NOT NULL CHECK (tax_rate_bp BETWEEN 0 AND 10000)
);

CREATE INDEX credit_note_lines_note_idx ON credit_note_lines (credit_note_id, position);

CREATE TABLE refunds (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credit_note_id UUID NOT NULL REFERENCES credit_notes(id),
    invoice_id     UUID NOT NULL REFERENCES invoices(id),
    refunded_on    DATE NOT NULL,
    amount_cents   BIGINT NOT NULL CHECK (amount_cents > 0),
    method         TEXT NOT NULL,
    reference      TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refunds_invoice_idx ON refunds (invoice_id);
CREATE INDEX refunds_refunded_on_idx ON refunds (refunded_on DESC);

-- Payments, credit notes, their lines and refunds are never changed or
-- removed once written.
-- +goose StatementBegin
CREATE FUNCTION finance_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% rows are never changed or deleted', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER payments_immutable BEFORE UPDATE OR DELETE ON payments
    FOR EACH ROW EXECUTE FUNCTION finance_immutable();
CREATE TRIGGER credit_notes_immutable BEFORE UPDATE OR DELETE ON credit_notes
    FOR EACH ROW EXECUTE FUNCTION finance_immutable();
CREATE TRIGGER credit_note_lines_immutable BEFORE UPDATE OR DELETE ON credit_note_lines
    FOR EACH ROW EXECUTE FUNCTION finance_immutable();
CREATE TRIGGER refunds_immutable BEFORE UPDATE OR DELETE ON refunds
    FOR EACH ROW EXECUTE FUNCTION finance_immutable();

ALTER TABLE expenses
    ADD COLUMN deleted_at     TIMESTAMPTZ,
    ADD COLUMN deleted_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX expenses_live_idx ON expenses (occurred_on DESC) WHERE deleted_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION finance_expense_no_delete() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'expenses are soft-deleted (deleted_at), never removed';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER expenses_no_delete BEFORE DELETE ON expenses
    FOR EACH ROW EXECUTE FUNCTION finance_expense_no_delete();

CREATE TABLE period_closures (
    month          DATE PRIMARY KEY CHECK (date_trunc('month', month)::date = month),
    closed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_by      TEXT NOT NULL,
    closed_issuer  TEXT NOT NULL DEFAULT '',
    closed_subject TEXT NOT NULL DEFAULT ''
);

CREATE TABLE approvals (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action            TEXT NOT NULL CHECK (action IN
                        ('invoice.void', 'payment.record', 'expense.create', 'credit_note.issue', 'refund.record')),
    -- The invoice (void, payment, credit note) or credit note (refund) the
    -- request is about; NULL for a new expense.
    target_id         UUID,
    amount_cents      BIGINT NOT NULL,
    payload           JSONB NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    requested_by      TEXT NOT NULL,
    requested_issuer  TEXT NOT NULL DEFAULT '',
    requested_subject TEXT NOT NULL DEFAULT '',
    requested_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_by        TEXT,
    decided_issuer    TEXT,
    decided_subject   TEXT,
    decided_at        TIMESTAMPTZ,
    decision_note     TEXT NOT NULL DEFAULT '',
    -- What the approved request created (payment, expense, credit note, refund).
    result_id         UUID
);

CREATE INDEX approvals_status_idx ON approvals (status, requested_at DESC);
CREATE INDEX approvals_target_idx ON approvals (target_id);
CREATE UNIQUE INDEX approvals_one_pending_void ON approvals (target_id)
    WHERE action = 'invoice.void' AND status = 'pending';

CREATE TABLE idempotency_keys (
    actor_issuer  TEXT NOT NULL,
    actor_subject TEXT NOT NULL,
    key           TEXT NOT NULL,
    scope         TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   UUID,
    status_code   INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_issuer, actor_subject, key)
);

CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);

CREATE TABLE settings (
    id                SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    seller_name       TEXT NOT NULL DEFAULT '',
    seller_address    TEXT NOT NULL DEFAULT '',
    seller_vat_number TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        TEXT NOT NULL DEFAULT ''
);
INSERT INTO settings (id) VALUES (1);

-- +goose Down
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS approvals;
DROP TABLE IF EXISTS period_closures;
DROP TRIGGER IF EXISTS expenses_no_delete ON expenses;
DROP FUNCTION IF EXISTS finance_expense_no_delete();
DROP INDEX IF EXISTS expenses_live_idx;
ALTER TABLE expenses DROP COLUMN IF EXISTS deleted_reason, DROP COLUMN IF EXISTS deleted_at;
DROP TRIGGER IF EXISTS payments_immutable ON payments;
DROP TABLE IF EXISTS refunds;
DROP TABLE IF EXISTS credit_note_lines;
DROP TABLE IF EXISTS credit_notes;
DROP FUNCTION IF EXISTS finance_immutable();
DROP TABLE IF EXISTS credit_note_counter;
DROP TRIGGER IF EXISTS invoices_frozen ON invoices;
DROP FUNCTION IF EXISTS finance_invoice_frozen();
ALTER TABLE invoices
    DROP COLUMN IF EXISTS pdf_sha256, DROP COLUMN IF EXISTS pdf_key,
    DROP COLUMN IF EXISTS seller_vat_number, DROP COLUMN IF EXISTS seller_address,
    DROP COLUMN IF EXISTS seller_name, DROP COLUMN IF EXISTS customer_tax_id,
    DROP COLUMN IF EXISTS customer_address, DROP COLUMN IF EXISTS customer_email,
    DROP COLUMN IF EXISTS customer_name;
DROP TRIGGER IF EXISTS audit_events_append_only ON audit_events;
DROP FUNCTION IF EXISTS finance_audit_append_only();
DROP INDEX IF EXISTS audit_events_subject_idx;
DROP INDEX IF EXISTS audit_events_action_idx;
ALTER TABLE audit_events
    DROP COLUMN IF EXISTS after, DROP COLUMN IF EXISTS before,
    DROP COLUMN IF EXISTS actor_subject, DROP COLUMN IF EXISTS actor_issuer;
