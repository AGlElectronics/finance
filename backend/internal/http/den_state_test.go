package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/AGlElectronics/finance/backend/internal/denlink"
)

// TestDenPairingGate checks the routing of DEN_MODE=required on the real
// router: the state endpoint is public, the Den admin routes still go
// through authentication, and other API routes answer 503. No database is
// needed: OIDC without a verifier refuses every request before a handler runs.
func TestDenPairingGate(t *testing.T) {
	cfg := oidcConfig()
	cfg.Den.Mode = denlink.ModeRequired
	den := denlink.New(nil, cfg.Den, zerolog.Nop())
	router := NewRouter(Deps{Config: cfg, Logger: zerolog.Nop(), Den: den}, "test")
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/api/den/state")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"mode":"required","paired":false,"revoked":false}` {
		t.Errorf("GET /api/den/state = %d %s", rec.Code, got)
	}

	rec = get("/api/customers")
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Error.Code != "den_pairing_required" {
		t.Errorf("GET /api/customers = %d %s, want 503 den_pairing_required", rec.Code, rec.Body.String())
	}

	rec = get("/api/den/")
	if rec.Code == http.StatusOK || rec.Code == http.StatusNotFound || rec.Code == http.StatusServiceUnavailable {
		t.Errorf("GET /api/den/ = %d %s, want it refused by authentication", rec.Code, rec.Body.String())
	}

	// The role catalog stays reachable while waiting for Den.
	req := httptest.NewRequest(http.MethodGet, "/api/role-catalog", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.HRServiceToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("role catalog while waiting for Den = %d", rec.Code)
	}
}
