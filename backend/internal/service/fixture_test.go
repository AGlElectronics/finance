package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
	"github.com/AGlElectronics/finance/backend/internal/docstore"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/testdb"
)

var (
	bookkeeper = audit.Actor{Name: "bea", Issuer: "https://idp.example", Subject: "sub-bea"}
	approver   = audit.Actor{Name: "aki", Issuer: "https://idp.example", Subject: "sub-aki"}
	// today is the fixed clock of every service test.
	today = time.Date(2026, 11, 11, 15, 0, 0, 0, time.UTC)
)

type fixture struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	svc   *Service
	store *docstore.Memory
	// den is the service's Den link; Restore it after changing den_link.
	den  *denlink.Client
	cust uuid.UUID
	n    int
}

func newFixture(t *testing.T, threshold int64) *fixture {
	t.Helper()
	pool := testdb.New(t)
	store := docstore.NewMemory()
	den := denlink.New(pool, denlink.Config{Slug: "finance", Mode: denlink.ModeStandalone, AppEnv: "development"}, zerolog.Nop())
	svc := New(pool, audit.NewWriter(pool, zerolog.Nop()), Options{
		Currency: "EUR", DefaultVATRateBp: 2100, InvoicePrefix: "FIN-", CreditNotePrefix: "FIN-CN-",
		ApprovalThresholdCents: threshold, MaxBackdateDays: 366, Store: store, Den: den,
	})
	svc.SetClock(func() time.Time { return today })
	f := &fixture{t: t, ctx: context.Background(), pool: pool, svc: svc, store: store, den: den}
	name, addr, vat := "Con Org e.V.", "Main Street 1\n12345 Town", "DE123456789"
	if _, err := svc.UpdateSettings(f.ctx, approver, SettingsInput{SellerName: &name, SellerAddress: &addr, SellerVATNumber: &vat}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	cname, caddr, ctax := "Hotel Grün", "Hotelweg 5", "ATU1234"
	c, err := svc.CreateCustomer(f.ctx, bookkeeper, CustomerInput{Name: &cname, BillingAddress: &caddr, TaxID: &ctax})
	if err != nil {
		t.Fatalf("customer: %v", err)
	}
	f.cust = c.ID
	return f
}

func (f *fixture) idem() Idem {
	f.n++
	return NewIdem(fmt.Sprintf("key-%d", f.n), "test", []byte(fmt.Sprint(f.n)))
}

// draft creates a draft for 100.00 + 21% VAT = 121.00.
func (f *fixture) draft() uuid.UUID {
	f.t.Helper()
	res, err := f.svc.CreateInvoice(f.ctx, bookkeeper, f.idem(), InvoiceInput{
		CustomerID: f.cust, Lines: []LineInput{{Description: "Hall rental", Quantity: 1, UnitPriceCents: 10000, TaxRateBp: 2100}},
	})
	if err != nil {
		f.t.Fatalf("create invoice: %v", err)
	}
	return res.ID
}

func (f *fixture) issued() Invoice {
	f.t.Helper()
	inv, err := f.svc.IssueInvoice(f.ctx, bookkeeper, f.draft(), IssueInput{IssueDate: "2026-11-02"})
	if err != nil {
		f.t.Fatalf("issue: %v", err)
	}
	return inv
}

// pay records and (when needed) approves a payment.
func (f *fixture) pay(id uuid.UUID, cents int64, on string) {
	f.t.Helper()
	res, err := f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), id, PaymentInput{PaidOn: on, AmountCents: cents, Method: "bank_transfer"})
	if err != nil {
		f.t.Fatalf("payment: %v", err)
	}
	if res.Pending {
		if _, err := f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{}); err != nil {
			f.t.Fatalf("approve payment: %v", err)
		}
	}
}

func (f *fixture) count(sql string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		f.t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// wantCode fails unless err is an *httperror.Error with code.
func wantCode(t *testing.T, err error, code httperror.Code) {
	t.Helper()
	var e *httperror.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}
