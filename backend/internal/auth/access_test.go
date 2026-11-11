package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/config"
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

func TestDevMiddlewareMapsGroupsToRoles(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModeDev, DevDefaultGroups: []string{access.RoleAdmin}, RoleGroups: config.DefaultRoleGroups()}
	var got *actor.Actor
	h := Middleware(cfg, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = actor.FromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Dev-User", "anna")
	req.Header.Set("X-Dev-Groups", "/finance_bookkeeper, other")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil || got.Username != "anna" || len(got.Roles) != 1 || got.Roles[0] != access.RoleBookkeeper {
		b, _ := json.Marshal(got)
		t.Fatalf("actor = %s", b)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil || !got.Can(access.Admin) {
		t.Fatalf("default dev groups should grant admin: %+v", got)
	}
}
