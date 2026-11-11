-- The shared denlink package (con-services shared/go/denlink) records when
-- Den rejected the module credential (denlink.SchemaUpgrade). The stored
-- credential is encrypted on its next read; no re-pairing is needed.

-- +goose Up
ALTER TABLE den_link ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE den_link DROP COLUMN IF EXISTS revoked_at;
