package http

import (
	"bytes"
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

type api struct {
	t      *testing.T
	router http.Handler
}

func newAPI(t *testing.T, threshold int64) *api {
	t.Helper()
	pool := testdb.New(t)
	cfg := &config.Config{
		AuthMode: config.AuthModeDev, DevDefaultUser: "nobody", RoleGroups: config.DefaultRoleGroups(),
		Currency: "EUR", DefaultVATRateBp: 2100, InvoicePrefix: "FIN-", CreditNotePrefix: "FIN-CN-",
		ApprovalThresholdCents: threshold, MaxBackdateDays: 366,
	}
	den := denlink.New(pool, denlink.Config{Slug: "finance", Mode: denlink.ModeStandalone}, zerolog.Nop())
	router := NewRouter(Deps{Config: cfg, Pool: pool, Logger: zerolog.Nop(), Den: den, Store: docstore.NewMemory(),
		Clock: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}, "test")
	return &api{t: t, router: router}
}

// do sends a request as user with role (a group of the same name).
func (a *api) do(user, role, method, path string, body any, headers ...string) *httptest.ResponseRecorder {
	a.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if s, ok := body.(string); ok {
			buf.WriteString(s)
		} else {
			_ = json.NewEncoder(&buf).Encode(body)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Dev-User", user)
	req.Header.Set("X-Dev-Groups", role)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

func want(t *testing.T, rec *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status %d, want %d: %s", rec.Code, code, rec.Body.String())
	}
}

const (
	admin      = access.RoleAdmin
	approver   = access.RoleApprover
	bookkeeper = access.RoleBookkeeper
	readOnly   = access.RoleReadOnly
)

func TestRoleGating(t *testing.T) {
	a := newAPI(t, 0)
	cases := []struct {
		role, method, path string
		want               int
	}{
		{"", "GET", "/api/invoices", 403}, // signed in, no Finance role
		{readOnly, "GET", "/api/invoices", 200},
		{readOnly, "GET", "/api/audit", 200},
		{readOnly, "POST", "/api/customers", 403},
		{bookkeeper, "GET", "/api/audit", 403},
		{bookkeeper, "POST", "/api/periods/2026-10/close", 403},
		{approver, "POST", "/api/customers", 403},
		{approver, "PUT", "/api/settings", 403},
		{approver, "POST", "/api/periods/2026-10/reopen", 403},
		{approver, "GET", "/api/den/", 403},
		{bookkeeper, "POST", "/api/approvals/00000000-0000-0000-0000-000000000001/approve", 403},
		{admin, "GET", "/api/den/", 200},
	}
	for _, c := range cases {
		rec := a.do("u", c.role, c.method, c.path, "{}")
		if rec.Code != c.want {
			t.Errorf("%s %s %s = %d, want %d: %s", c.role, c.method, c.path, rec.Code, c.want, rec.Body.String())
		}
	}

	var me struct {
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	decode(t, a.do("u", "finance_approver,finance_read-only", "GET", "/api/me", nil), &me)
	if len(me.Roles) != 2 || strings.Join(me.Permissions, ",") != "finance.read,finance.approve,finance.audit" {
		t.Fatalf("me = %+v", me)
	}
}

func TestRoleCatalog(t *testing.T) {
	cfg := &config.Config{AuthMode: config.AuthModeDev, RoleGroups: config.DefaultRoleGroups()}
	cfg.RoleGroups[access.RoleApprover] = []string{"treasurer", "board"}
	router := NewRouter(Deps{Config: cfg, Logger: zerolog.Nop(), Den: denlink.New(nil, denlink.Config{Slug: "finance"}, zerolog.Nop())}, "test")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/role-catalog", nil))
	var cat struct {
		Roles []struct {
			Key, Group  string
			Permissions []string
		}
	}
	decode(t, rec, &cat)
	if len(cat.Roles) != 4 || cat.Roles[1].Key != "finance_approver" || cat.Roles[1].Group != "treasurer" {
		t.Fatalf("catalog = %+v", cat)
	}
}

func TestMoneyFlowOverHTTP(t *testing.T) {
	a := newAPI(t, 0)
	want(t, a.do("ada", admin, "PUT", "/api/settings", map[string]string{"seller_name": "Con Org", "seller_address": "Main Street 1"}), 200)

	rec := a.do("bea", bookkeeper, "POST", "/api/customers", map[string]string{"name": "Hotel"})
	want(t, rec, 201)
	var cust struct{ ID string }
	decode(t, rec, &cust)

	draft := map[string]any{"customer_id": cust.ID, "lines": []map[string]any{{"description": "Hall", "quantity": 1, "unit_price_cents": 10000, "tax_rate_bp": 2100}}}
	rec = a.do("bea", bookkeeper, "POST", "/api/invoices", draft)
	want(t, rec, 400) // no Idempotency-Key
	if !strings.Contains(rec.Body.String(), "idempotency_key_required") {
		t.Fatal(rec.Body.String())
	}
	rec = a.do("bea", bookkeeper, "POST", "/api/invoices", draft, "Idempotency-Key", "inv-1")
	want(t, rec, 201)
	var inv struct {
		ID, Number, Status string
	}
	decode(t, rec, &inv)
	rec = a.do("bea", bookkeeper, "POST", "/api/invoices", draft, "Idempotency-Key", "inv-1")
	want(t, rec, 201)
	if rec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("replay header missing")
	}
	want(t, a.do("bea", bookkeeper, "POST", "/api/invoices", `{"customer_id": 1}`, "Idempotency-Key", "inv-1"), 400)

	rec = a.do("bea", bookkeeper, "POST", "/api/invoices/"+inv.ID+"/issue", nil)
	want(t, rec, 200)
	decode(t, rec, &inv)
	if inv.Number != "FIN-0001" {
		t.Fatalf("number %q", inv.Number)
	}
	rec = a.do("ro", readOnly, "GET", "/api/invoices/"+inv.ID+"/pdf", nil)
	want(t, rec, 200)
	if rec.Header().Get("Content-Type") != "application/pdf" || !strings.Contains(rec.Header().Get("Content-Disposition"), "FIN-0001.pdf") {
		t.Fatalf("pdf headers %v", rec.Header())
	}

	rec = a.do("bea", bookkeeper, "POST", "/api/invoices/"+inv.ID+"/payments",
		map[string]any{"amount_cents": 12100, "method": "bank_transfer", "paid_on": "2026-11-05"}, "Idempotency-Key", "pay-1")
	want(t, rec, 202)
	var pending struct {
		Status   string
		Approval struct{ ID, Action string }
	}
	decode(t, rec, &pending)
	if pending.Status != "pending_approval" || pending.Approval.Action != "payment.record" {
		t.Fatalf("pending = %+v", pending)
	}
	approve := "/api/approvals/" + pending.Approval.ID + "/approve"
	want(t, a.do("bea", admin, "POST", approve, nil), 403) // same person, even as admin
	want(t, a.do("aki", approver, "POST", approve, `{"note":"ok"}`), 200)

	rec = a.do("bea", bookkeeper, "POST", "/api/invoices/"+inv.ID+"/void", map[string]string{"reason": "x"})
	want(t, rec, 409) // paid invoices are never voided

	rec = a.do("ro", readOnly, "GET", "/api/invoices?status=paid&limit=10", nil)
	want(t, rec, 200)
	var list struct {
		Invoices []struct{ Number string }
		Total    int
		Limit    int
	}
	decode(t, rec, &list)
	if list.Total != 1 || list.Limit != 10 || list.Invoices[0].Number != "FIN-0001" {
		t.Fatalf("list = %+v", list)
	}
	want(t, a.do("ro", readOnly, "GET", "/api/ledger?limit=abc", nil), 400)

	want(t, a.do("aki", approver, "POST", "/api/periods/2026-10/close", nil), 201)
	want(t, a.do("aki", approver, "POST", "/api/periods/2026-10/close", nil), 409)
	want(t, a.do("ada", admin, "POST", "/api/periods/2026-10/reopen", `{"reason":"late invoice"}`), 204)

	rec = a.do("ro", readOnly, "GET", "/api/audit?action=approval&limit=5", nil)
	want(t, rec, 200)
	var log struct {
		Events []struct {
			Action string
			Actor  struct{ Name string }
		}
		Total int
	}
	decode(t, rec, &log)
	if log.Total != 2 || log.Events[0].Action != "approval.approved" || log.Events[0].Actor.Name != "aki" {
		t.Fatalf("audit = %+v", log)
	}
}

func TestExpenseDeleteOverHTTP(t *testing.T) {
	a := newAPI(t, 1_000_000)
	rec := a.do("bea", bookkeeper, "POST", "/api/expenses", map[string]any{"vendor": "Printer", "amount_cents": 1500}, "Idempotency-Key", "e-1")
	want(t, rec, 201)
	var e struct{ ID string }
	decode(t, rec, &e)
	want(t, a.do("bea", bookkeeper, "DELETE", "/api/expenses/"+e.ID, nil), 422)
	want(t, a.do("bea", bookkeeper, "DELETE", "/api/expenses/"+e.ID+"?reason=duplicate", nil), 204)
	rec = a.do("ro", readOnly, "GET", "/api/expenses?include_deleted=true", nil)
	want(t, rec, 200)
	if !strings.Contains(rec.Body.String(), `"deleted":true`) {
		t.Fatal(rec.Body.String())
	}
}
