package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
	"github.com/AGlElectronics/finance/backend/internal/docstore"
	"github.com/AGlElectronics/finance/backend/internal/testdb"
)

// TestDenRevokedAfterRestart runs Finance's migrations (including
// den_link.revoked_at) and checks that a link Den rejected is reported as
// revoked by the public state and the admin status after a restart.
func TestDenRevokedAfterRestart(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO den_link (id, den_base_url, credential, module_id, paired_at, last_error, revoked_at)
		VALUES (1, 'http://den.conservices.test', 'dev-credential', 'mod-1', now(), $1, now())`,
		denlink.ErrRevoked.Error()); err != nil {
		t.Fatalf("seed den_link: %v", err)
	}

	cfg := config.ForTest()
	den := denlink.New(pool, cfg.Den, zerolog.Nop())
	if err := den.Restore(ctx); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !den.Revoked() {
		t.Fatal("Revoked() = false after restart, want true")
	}
	router := NewRouter(Deps{Config: cfg, Pool: pool, Logger: zerolog.Nop(), Den: den, Store: docstore.NewMemory(),
		Clock: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}, "test")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/den/state", nil))
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"mode":"standalone","paired":true,"revoked":true}` {
		t.Errorf("GET /api/den/state = %d %s", rec.Code, got)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/den/", nil)
	req.Header.Set("X-Dev-User", "admin")
	req.Header.Set("X-Dev-Groups", access.RoleAdmin)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var status denlink.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /api/den/ = %d %s", rec.Code, rec.Body.String())
	}
	if !status.Paired || !status.Revoked || status.RevokedAt == nil {
		t.Errorf("status = %+v, want paired and revoked with revoked_at", status)
	}
}
