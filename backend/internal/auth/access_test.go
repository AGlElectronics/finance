package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AGlElectronics/finance/backend/internal/actor"
)

func TestInAnyGroup(t *testing.T) {
	cases := []struct {
		have, want []string
		ok         bool
	}{
		{[]string{"/finance-admin"}, []string{"finance-admin"}, true},
		{[]string{"Finance-Admin"}, []string{"finance-admin"}, true},
		{[]string{"stash-volunteer", "dept-ops"}, []string{"finance-admin"}, false},
		{nil, []string{"finance-admin"}, false},
	}
	for _, c := range cases {
		if got := inAnyGroup(c.have, c.want); got != c.ok {
			t.Errorf("inAnyGroup(%v, %v) = %v, want %v", c.have, c.want, got, c.ok)
		}
	}
}

func TestRequireAccess(t *testing.T) {
	h := RequireAccess(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for name, tc := range map[string]struct {
		a    *actor.Actor
		want int
	}{
		"no actor":      {nil, http.StatusForbidden},
		"volunteer":     {&actor.Actor{Username: "vol", Groups: []string{"stash-volunteer"}}, http.StatusForbidden},
		"finance admin": {&actor.Actor{Username: "fin", Groups: []string{"finance-admin"}, HasAccess: true}, http.StatusOK},
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
