package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/authbase"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/groupmatch"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRequireAccess(t *testing.T) {
	h := RequireAccess(http.HandlerFunc(ok))
	for name, tc := range map[string]struct {
		a    *actor.Actor
		want int
	}{
		"no actor":   {nil, http.StatusForbidden},
		"volunteer":  {&actor.Actor{Username: "vol", Groups: []string{"stash-volunteer"}}, http.StatusForbidden},
		"bookkeeper": {&actor.Actor{Username: "fin", Roles: []string{access.RoleBookkeeper}, HasAccess: true}, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/invoices", nil)
		if tc.a != nil {
			req = req.WithContext(actor.With(req.Context(), tc.a))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: got %d, want %d", name, rec.Code, tc.want)
		}
	}
}

func TestRequirePermission(t *testing.T) {
	cases := []struct {
		role string
		perm access.Permission
		want int
	}{
		{access.RoleReadOnly, access.Read, http.StatusOK},
		{access.RoleReadOnly, access.Write, http.StatusForbidden},
		{access.RoleBookkeeper, access.Write, http.StatusOK},
		{access.RoleBookkeeper, access.Approve, http.StatusForbidden},
		{access.RoleBookkeeper, access.Audit, http.StatusForbidden},
		{access.RoleApprover, access.Approve, http.StatusOK},
		{access.RoleApprover, access.Write, http.StatusForbidden},
		{access.RoleApprover, access.Admin, http.StatusForbidden},
		{access.RoleAdmin, access.Admin, http.StatusOK},
	}
	for _, c := range cases {
		h := Require(c.perm)(http.HandlerFunc(ok))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(actor.With(req.Context(), &actor.Actor{Username: "u", Roles: []string{c.role}, HasAccess: true}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s with %s: got %d, want %d", c.role, c.perm, rec.Code, c.want)
		}
	}
}

func capture(cfg *config.Config, v *OIDCVerifier, req *http.Request) (*actor.Actor, int) {
	var got *actor.Actor
	h := Middleware(cfg, v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = actor.FromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return got, rec.Code
}

func TestDevMiddlewareMapsGroupsToRoles(t *testing.T) {
	cfg := config.ForTest()
	cfg.DevDefaultGroups = []string{access.RoleAdmin}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Dev-User", "anna")
	req.Header.Set("X-Dev-Groups", "/finance_bookkeeper, other")
	got, _ := capture(cfg, nil, req)
	if got == nil || got.Username != "anna" || len(got.Roles) != 1 || got.Roles[0] != access.RoleBookkeeper {
		b, _ := json.Marshal(got)
		t.Fatalf("actor = %s", b)
	}
	got, _ = capture(cfg, nil, httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil || !got.Can(access.Admin) {
		t.Fatalf("default dev groups should grant admin: %+v", got)
	}
}

const issuer = "https://auth.example.org/realms/conservices"

type signer struct {
	t   *testing.T
	key *rsa.PrivateKey
	sig jose.Signer
}

func newSigner(t *testing.T) *signer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &signer{t: t, key: key, sig: sig}
}

func (s *signer) token(claims map[string]any) string {
	base := map[string]any{"iss": issuer, "sub": "sub-1", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"preferred_username": "bea"}
	for k, v := range claims {
		base[k] = v
	}
	b, _ := json.Marshal(base)
	jws, err := s.sig.Sign(b)
	if err != nil {
		s.t.Fatal(err)
	}
	out, _ := jws.CompactSerialize()
	return out
}

func (s *signer) verifier(gm groupmatch.Config) *OIDCVerifier {
	keys := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&s.key.PublicKey}}
	v := oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: config.DefaultAudience})
	return NewOIDCVerifierFrom(v, issuer, gm)
}

func TestOIDCAccessTokens(t *testing.T) {
	s := newSigner(t)
	cfg := config.ForTest()
	cfg.Auth = authbase.Config{AuthMode: authbase.AuthOIDC, AppEnv: "production"}
	v := s.verifier(groupmatch.Config{})
	call := func(tok string) (*actor.Actor, int) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		return capture(cfg, v, req)
	}

	a, code := call(s.token(map[string]any{"aud": []string{"finance-api", "account"}, "typ": "Bearer", "groups": []string{"/finance_approver"}}))
	if code != http.StatusOK || a == nil || a.Username != "bea" || len(a.Roles) != 1 || a.Roles[0] != access.RoleApprover {
		t.Fatalf("access token: %d %+v", code, a)
	}
	for name, claims := range map[string]map[string]any{
		"ID token":       {"aud": "finance-api", "typ": "ID", "groups": []string{"finance_admin"}},
		"other audience": {"aud": "finance-spa", "typ": "Bearer", "groups": []string{"finance_admin"}},
		"expired":        {"aud": "finance-api", "exp": time.Now().Add(-time.Minute).Unix()},
	} {
		if _, code := call(s.token(claims)); code != http.StatusUnauthorized {
			t.Errorf("%s: got %d, want 401", name, code)
		}
	}
	if _, code := call(""); code != http.StatusUnauthorized {
		t.Errorf("no token: got %d", code)
	}

	// The roles claim is never merged into the groups.
	a, _ = call(s.token(map[string]any{"aud": "finance-api", "roles": []string{"finance_admin"}}))
	if a == nil || len(a.Roles) != 0 || a.HasAccess {
		t.Fatalf("roles claim granted access: %+v", a)
	}

	// Dev headers are ignored outside dev mode (router strips them; the
	// middleware never reads them in OIDC mode).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Dev-User", "mallory")
	if _, code := capture(cfg, v, req); code != http.StatusUnauthorized {
		t.Fatalf("X-Dev-User in OIDC mode: %d", code)
	}
}

func TestOIDCGroupsClaimAndPrefix(t *testing.T) {
	s := newSigner(t)
	cfg := config.ForTest()
	cfg.Auth = authbase.Config{AuthMode: authbase.AuthOIDC}
	cfg.Groups = groupmatch.Config{Claim: "realm_access.groups", Prefix: "/con2026"}
	v := s.verifier(cfg.Groups)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+s.token(map[string]any{"aud": "finance-api",
		"realm_access": map[string]any{"groups": []string{"/con2026/Finance_Read-Only", "/con2025/finance_admin"}}}))
	a, code := capture(cfg, v, req)
	if code != http.StatusOK || a == nil || len(a.Roles) != 1 || a.Roles[0] != access.RoleReadOnly {
		t.Fatalf("nested claim with prefix: %d %+v", code, a)
	}
}
