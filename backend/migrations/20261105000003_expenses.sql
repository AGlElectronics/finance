-- Expenses: money going out, so the ledger and dashboard can show the full
-- cash position instead of just invoices. Amounts are integer cents (EUR),
-- same as everywhere else in Finance.

-- +goose Up
CREATE TABLE expenses (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_on  DATE NOT NULL DEFAULT current_date,
    vendor       TEXT NOT NULL,
    category     TEXT NOT NULL DEFAULT 'other' CHECK (category IN
                   ('venue', 'equipment', 'travel', 'supplies', 'services', 'fees', 'other')),
    description  TEXT NOT NULL DEFAULT '',
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    method       TEXT NOT NULL DEFAULT 'bank_transfer',
    -- reference is what a future bank feed would match transactions against.
    reference    TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX expenses_occurred_idx ON expenses (occurred_on DESC);
CREATE INDEX expenses_category_idx ON expenses (category);

-- +goose Down
DROP TABLE IF EXISTS expenses;
