package service

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AGlElectronics/finance/backend/internal/domain"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

func TestInvoiceLifecycle(t *testing.T) {
	f := newFixture(t, 0)
	id := f.draft()

	inv, err := f.svc.UpdateDraft(f.ctx, bookkeeper, id, InvoiceInput{
		CustomerID: f.cust, DueDate: "2026-11-30", Notes: "Thanks",
		Lines: []LineInput{
			{Description: "Hall rental", Quantity: 1, UnitPriceCents: 10000, TaxRateBp: 2100},
			{Description: "Cleaning", Quantity: 1.5, UnitPriceCents: 2000, TaxRateBp: 2100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.TotalCents != 15730 || inv.Status != domain.StatusDraft || inv.Seller != nil || inv.HasPDF {
		t.Fatalf("draft = %+v", inv.InvoiceSummary)
	}

	inv, err = f.svc.IssueInvoice(f.ctx, bookkeeper, id, IssueInput{IssueDate: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Number != "FIN-0001" || inv.Status != domain.StatusIssued || inv.DueDate != "2026-11-30" || !inv.HasPDF {
		t.Fatalf("issued = %+v", inv.InvoiceSummary)
	}
	if inv.Seller == nil || inv.Seller.VATNumber != "DE123456789" || inv.Customer.TaxID != "ATU1234" {
		t.Fatalf("snapshot = %+v / %+v", inv.Seller, inv.Customer)
	}

	// The PDF is stored once and served unchanged.
	name, body, err := f.svc.InvoicePDF(f.ctx, id)
	if err != nil || name != "FIN-0001.pdf" || !bytes.HasPrefix(body, []byte("%PDF-1.4")) {
		t.Fatalf("pdf %s %v", name, err)
	}
	if len(f.store.Objects) != 1 {
		t.Fatalf("store has %d objects", len(f.store.Objects))
	}

	// Renaming the customer leaves the issued invoice alone.
	newName := "Hotel Renamed"
	if _, err := f.svc.UpdateCustomer(f.ctx, bookkeeper, f.cust, CustomerInput{Name: &newName}); err != nil {
		t.Fatal(err)
	}
	inv, _ = f.svc.GetInvoice(f.ctx, id)
	if inv.CustomerName != "Hotel Grün" || inv.Customer.Name != "Hotel Grün" {
		t.Fatalf("snapshot changed to %q", inv.CustomerName)
	}

	_, err = f.svc.IssueInvoice(f.ctx, bookkeeper, id, IssueInput{})
	wantCode(t, err, httperror.CodeConflict)
	wantCode(t, f.svc.DeleteDraft(f.ctx, bookkeeper, id), httperror.CodeConflict)
	_, err = f.svc.UpdateDraft(f.ctx, bookkeeper, id, InvoiceInput{CustomerID: f.cust, Lines: []LineInput{{Description: "x", Quantity: 1}}})
	wantCode(t, err, httperror.CodeConflict)

	// The database refuses to change a frozen invoice even without the service.
	if _, err := f.pool.Exec(f.ctx, `UPDATE invoices SET total_cents = 1 WHERE id = $1`, id); err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("frozen trigger: %v", err)
	}

	if n := f.count(`SELECT count(*) FROM audit_events WHERE entity_id = $1`, id); n != 3 {
		t.Fatalf("audit events for the invoice = %d, want 3 (created, updated, issued)", n)
	}
	if n := f.count(`SELECT count(*) FROM audit_events WHERE entity_id = $1 AND actor_subject = 'sub-bea' AND before IS NOT NULL`, id); n != 1 {
		t.Fatalf("the update event keeps before values and the actor subject: %d", n)
	}
}

func TestIssueRollsBackWhenPDFStorageFails(t *testing.T) {
	f := newFixture(t, 0)
	id := f.draft()
	f.store.FailPut = true
	_, err := f.svc.IssueInvoice(f.ctx, bookkeeper, id, IssueInput{})
	wantCode(t, err, httperror.CodeStorageUnavailable)
	inv, _ := f.svc.GetInvoice(f.ctx, id)
	if inv.Status != domain.StatusDraft || inv.Number != "" {
		t.Fatalf("after a failed store the invoice is %s %q", inv.Status, inv.Number)
	}
	f.store.FailPut = false
	inv, err = f.svc.IssueInvoice(f.ctx, bookkeeper, id, IssueInput{})
	if err != nil || inv.Number != "FIN-0001" {
		t.Fatalf("retry: %v %q (the failed attempt must not use up a number)", err, inv.Number)
	}
}

func TestIssueNeedsSellerDetails(t *testing.T) {
	f := newFixture(t, 0)
	empty := ""
	if _, err := f.svc.UpdateSettings(f.ctx, approver, SettingsInput{SellerName: &empty, SellerAddress: &empty}); err != nil {
		t.Fatal(err)
	}
	id := f.draft()
	_, err := f.svc.IssueInvoice(f.ctx, bookkeeper, id, IssueInput{})
	wantCode(t, err, httperror.CodeValidation)

	// Den's values apply when Finance has no override.
	f.svc.opts.Den = fakeDen{DenKeySellerName: "Den Org", DenKeySellerAddress: "Den Street 2"}
	s, err := f.svc.GetSettings(f.ctx)
	if err != nil || s.Seller.Name != "Den Org" || s.Sources.Name != "den" || s.Sources.VATNumber != "finance" {
		t.Fatalf("settings = %+v %v", s, err)
	}
	inv, err := f.svc.IssueInvoice(f.ctx, bookkeeper, id, IssueInput{})
	if err != nil || inv.Seller.Name != "Den Org" {
		t.Fatalf("issue with Den seller: %v %+v", err, inv.Seller)
	}
	// A Finance override wins over Den.
	over := "Override Org"
	if s, _ = f.svc.UpdateSettings(f.ctx, approver, SettingsInput{SellerName: &over}); s.Seller.Name != over || s.Sources.Name != "finance" {
		t.Fatalf("override = %+v", s.Seller)
	}
}

func TestLineAndDateBounds(t *testing.T) {
	f := newFixture(t, 0)
	bad := []LineInput{
		{Description: "neg", Quantity: 1, UnitPriceCents: -1},
		{Description: "huge", Quantity: 1, UnitPriceCents: domain.MaxUnitPriceCents + 1},
		{Description: "qty", Quantity: 0},
		{Description: "qty", Quantity: 2_000_000, UnitPriceCents: 1},
		{Description: "product", Quantity: 1_000_000, UnitPriceCents: domain.MaxUnitPriceCents},
		{Description: "", Quantity: 1},
		{Description: "tax", Quantity: 1, TaxRateBp: 10001},
	}
	for _, l := range bad {
		_, err := f.svc.CreateInvoice(f.ctx, bookkeeper, f.idem(), InvoiceInput{CustomerID: f.cust, Lines: []LineInput{l}})
		wantCode(t, err, httperror.CodeValidation)
	}
	many := make([]LineInput, domain.MaxLines+1)
	for i := range many {
		many[i] = LineInput{Description: "x", Quantity: 1}
	}
	_, err := f.svc.CreateInvoice(f.ctx, bookkeeper, f.idem(), InvoiceInput{CustomerID: f.cust, Lines: many})
	wantCode(t, err, httperror.CodeValidation)

	id := f.draft()
	for _, in := range []IssueInput{
		{IssueDate: "2026-11-12"},                        // future
		{IssueDate: "2025-01-01"},                        // beyond MaxBackdateDays
		{IssueDate: "2026-11-10", DueDate: "2026-11-09"}, // due before issue
		{IssueDate: "2026-11-10", DueDate: "2027-12-01"}, // due too late
		{IssueDate: "11/10/2026"},                        // not a date
	} {
		_, err := f.svc.IssueInvoice(f.ctx, bookkeeper, id, in)
		wantCode(t, err, httperror.CodeValidation)
	}
	_, err = f.svc.CreateInvoice(f.ctx, bookkeeper, f.idem(), InvoiceInput{CustomerID: f.cust, DueDate: "0026-11-01",
		Lines: []LineInput{{Description: "x", Quantity: 1}}})
	wantCode(t, err, httperror.CodeValidation)
}

func TestIdempotentCreate(t *testing.T) {
	f := newFixture(t, 0)
	in := InvoiceInput{CustomerID: f.cust, Lines: []LineInput{{Description: "x", Quantity: 1, UnitPriceCents: 100}}}
	idem := NewIdem("same", "POST /api/invoices", []byte("body-1"))
	a, err := f.svc.CreateInvoice(f.ctx, bookkeeper, idem, in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.svc.CreateInvoice(f.ctx, bookkeeper, idem, in)
	if err != nil || b.ID != a.ID || !b.Replayed {
		t.Fatalf("replay = %+v %v", b, err)
	}
	if n := f.count(`SELECT count(*) FROM invoices`); n != 1 {
		t.Fatalf("invoices = %d", n)
	}
	_, err = f.svc.CreateInvoice(f.ctx, bookkeeper, NewIdem("same", "POST /api/invoices", []byte("body-2")), in)
	wantCode(t, err, httperror.CodeIdempotencyReused)
	// Keys are per person.
	if c, err := f.svc.CreateInvoice(f.ctx, approver, idem, in); err != nil || c.Replayed {
		t.Fatalf("other person, same key: %+v %v", c, err)
	}
	_, err = f.svc.CreateInvoice(f.ctx, bookkeeper, Idem{}, in)
	wantCode(t, err, httperror.CodeIdempotencyRequired)
}

func TestInvoiceListPagination(t *testing.T) {
	f := newFixture(t, 0)
	for i := 0; i < 3; i++ {
		f.draft()
	}
	f.issued()
	page, err := f.svc.ListInvoices(f.ctx, InvoiceFilter{Page: Page{Limit: 2}})
	if err != nil || page.Total != 4 || len(page.Invoices) != 2 {
		t.Fatalf("page 1 = %d of %d, %v", len(page.Invoices), page.Total, err)
	}
	page, _ = f.svc.ListInvoices(f.ctx, InvoiceFilter{Page: Page{Limit: 2, Offset: 2}})
	if len(page.Invoices) != 2 {
		t.Fatalf("page 2 = %d", len(page.Invoices))
	}
	page, _ = f.svc.ListInvoices(f.ctx, InvoiceFilter{Status: "issued"})
	if page.Total != 1 || page.Invoices[0].Number != "FIN-0001" {
		t.Fatalf("issued filter = %+v", page)
	}
	if page, _ = f.svc.ListInvoices(f.ctx, InvoiceFilter{Page: Page{Limit: 1000}}); page.Limit != 200 {
		t.Fatalf("limit cap = %d", page.Limit)
	}
	_, err = f.svc.ListInvoices(f.ctx, InvoiceFilter{Status: "bogus"})
	wantCode(t, err, httperror.CodeBadRequest)
}

func TestAuditIsPartOfTheTransaction(t *testing.T) {
	f := newFixture(t, 0)
	before := f.count(`SELECT count(*) FROM customers`)
	if _, err := f.pool.Exec(f.ctx, `ALTER TABLE audit_events RENAME TO audit_events_off`); err != nil {
		t.Fatal(err)
	}
	name := "Ghost"
	_, err := f.svc.CreateCustomer(f.ctx, bookkeeper, CustomerInput{Name: &name})
	if _, rerr := f.pool.Exec(f.ctx, `ALTER TABLE audit_events_off RENAME TO audit_events`); rerr != nil {
		t.Fatal(rerr)
	}
	if err == nil {
		t.Fatal("create must fail when its audit event cannot be written")
	}
	if after := f.count(`SELECT count(*) FROM customers`); after != before {
		t.Fatalf("customer was committed without its audit event (%d -> %d)", before, after)
	}
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM audit_events`); err == nil {
		t.Fatal("audit_events must be append-only")
	}
}
