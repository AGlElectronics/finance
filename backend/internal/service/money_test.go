package service

import (
	"strings"
	"testing"

	"github.com/AGlElectronics/finance/backend/internal/domain"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

func TestPaymentNeedsASecondPerson(t *testing.T) {
	f := newFixture(t, 0)
	inv := f.issued()
	idem := f.idem()
	res, err := f.svc.RecordPayment(f.ctx, bookkeeper, idem, inv.ID, PaymentInput{PaidOn: "2026-11-05", AmountCents: 5000, Method: "bank_transfer"})
	if err != nil || !res.Pending {
		t.Fatalf("with threshold 0 every payment waits: %+v %v", res, err)
	}
	if got, _ := f.svc.GetInvoice(f.ctx, inv.ID); got.PaidCents != 0 || len(got.PendingApprovals) != 1 {
		t.Fatalf("pending payment counted: paid %d, pending %d", got.PaidCents, len(got.PendingApprovals))
	}
	// A retry returns the same approval instead of a second request.
	again, err := f.svc.RecordPayment(f.ctx, bookkeeper, idem, inv.ID, PaymentInput{PaidOn: "2026-11-05", AmountCents: 5000, Method: "bank_transfer"})
	if err != nil || again.ID != res.ID || !again.Replayed || !again.Pending {
		t.Fatalf("replay = %+v %v", again, err)
	}

	_, err = f.svc.Approve(f.ctx, bookkeeper, res.ID, DecisionInput{})
	wantCode(t, err, httperror.CodeSameApprover)

	a, err := f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{Note: "matches bank statement"})
	if err != nil || a.Status != "approved" || a.ResultID == nil || a.DecidedBy.Subject != "sub-aki" {
		t.Fatalf("approve = %+v %v", a, err)
	}
	got, _ := f.svc.GetInvoice(f.ctx, inv.ID)
	if got.PaidCents != 5000 || got.Status != domain.StatusPartial || got.DueCents != 7100 {
		t.Fatalf("after approval: %+v", got.InvoiceSummary)
	}
	_, err = f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{})
	wantCode(t, err, httperror.CodeConflict)

	// The approved payment is audited as done by the approver, with the approval.
	if n := f.count(`SELECT count(*) FROM audit_events WHERE action = 'payment.recorded' AND actor_subject = 'sub-aki' AND metadata->>'approval_id' = $1`, res.ID.String()); n != 1 {
		t.Fatalf("payment audit = %d", n)
	}
}

func TestApprovalThreshold(t *testing.T) {
	f := newFixture(t, 10000)
	inv := f.issued()
	res, err := f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), inv.ID, PaymentInput{AmountCents: 10000, Method: "cash"})
	if err != nil || res.Pending {
		t.Fatalf("at the threshold no approval is needed: %+v %v", res, err)
	}
	res, err = f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), inv.ID, PaymentInput{AmountCents: 2101, Method: "cash"})
	wantCode(t, err, httperror.CodeValidation) // more than the 2100 still owed
	res, err = f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), inv.ID, PaymentInput{AmountCents: 2100, Method: "cash"})
	if err != nil || res.Pending {
		t.Fatalf("second payment: %+v %v", res, err)
	}
	if got, _ := f.svc.GetInvoice(f.ctx, inv.ID); got.Status != domain.StatusPaid {
		t.Fatalf("status = %s", got.Status)
	}

	big := f.issued() // 12100 > threshold
	res, err = f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), big.ID, PaymentInput{AmountCents: 12100, Method: "card"})
	if err != nil || !res.Pending {
		t.Fatalf("above the threshold an approval is needed: %+v %v", res, err)
	}
	// The requester can withdraw; rejecting needs a reason.
	_, err = f.svc.Reject(f.ctx, bookkeeper, res.ID, DecisionInput{})
	wantCode(t, err, httperror.CodeValidation)
	if a, err := f.svc.Reject(f.ctx, bookkeeper, res.ID, DecisionInput{Note: "typo"}); err != nil || a.Status != "rejected" {
		t.Fatalf("reject = %+v %v", a, err)
	}
}

func TestPaidInvoicesAreNeverVoided(t *testing.T) {
	f := newFixture(t, 0)
	paid := f.issued()
	f.pay(paid.ID, 100, "2026-11-03")
	_, err := f.svc.RequestVoid(f.ctx, bookkeeper, paid.ID, VoidInput{Reason: "wrong"})
	wantCode(t, err, httperror.CodeConflict)

	open := f.issued()
	_, err = f.svc.RequestVoid(f.ctx, bookkeeper, open.ID, VoidInput{})
	wantCode(t, err, httperror.CodeValidation)
	res, err := f.svc.RequestVoid(f.ctx, bookkeeper, open.ID, VoidInput{Reason: "sent twice"})
	if err != nil || !res.Pending {
		t.Fatalf("void request = %+v %v", res, err)
	}
	_, err = f.svc.RequestVoid(f.ctx, bookkeeper, open.ID, VoidInput{Reason: "again"})
	wantCode(t, err, httperror.CodeConflict)

	// Paid while the void waited: the approval cannot go through.
	f.pay(open.ID, 100, "2026-11-04")
	_, err = f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{})
	wantCode(t, err, httperror.CodeConflict)

	third := f.issued()
	res, _ = f.svc.RequestVoid(f.ctx, bookkeeper, third.ID, VoidInput{Reason: "cancelled event"})
	if _, err := f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{}); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.GetInvoice(f.ctx, third.ID)
	if got.Status != domain.StatusVoid || got.Number == "" {
		t.Fatalf("voided invoice = %s %q (the number stays taken)", got.Status, got.Number)
	}
	_, err = f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), third.ID, PaymentInput{AmountCents: 1, Method: "cash"})
	wantCode(t, err, httperror.CodeConflict)
}

func TestCreditNotesAndRefunds(t *testing.T) {
	f := newFixture(t, 0)
	inv := f.issued() // 121.00
	f.pay(inv.ID, 12100, "2026-11-05")

	approveIf := func(res Result, err error) Result {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if res.Pending {
			a, err := f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{})
			if err != nil {
				t.Fatal(err)
			}
			res.ID = *a.ResultID
		}
		return res
	}

	_, err := f.svc.RequestCreditNote(f.ctx, bookkeeper, f.idem(), inv.ID, CreditNoteInput{
		Lines: []LineInput{{Description: "Discount", Quantity: 1, UnitPriceCents: 3000, TaxRateBp: 2100}}})
	wantCode(t, err, httperror.CodeValidation) // reason missing
	cn := approveIf(f.svc.RequestCreditNote(f.ctx, bookkeeper, f.idem(), inv.ID, CreditNoteInput{
		Reason: "Hall was smaller than booked",
		Lines:  []LineInput{{Description: "Discount", Quantity: 1, UnitPriceCents: 3000, TaxRateBp: 2100}}}))
	note, err := f.svc.GetCreditNote(f.ctx, cn.ID)
	if err != nil || note.Number != "FIN-CN-0001" || note.TotalCents != 3630 || note.InvoiceNumber != "FIN-0001" || note.Customer.Name != "Hotel Grün" {
		t.Fatalf("credit note = %+v %v", note, err)
	}
	if _, body, err := f.svc.CreditNotePDF(f.ctx, cn.ID); err != nil || !strings.Contains(string(body), "Credit note") {
		t.Fatalf("credit note pdf: %v", err)
	}

	got, _ := f.svc.GetInvoice(f.ctx, inv.ID)
	if got.Status != domain.StatusPaid || got.CreditedCents != 3630 || got.RefundableCents != 3630 || got.DueCents != 0 {
		t.Fatalf("after credit note: %+v", got.InvoiceSummary)
	}
	_, err = f.svc.RequestCreditNote(f.ctx, bookkeeper, f.idem(), inv.ID, CreditNoteInput{Reason: "all", Full: true})
	wantCode(t, err, httperror.CodeConflict) // already has credit notes
	_, err = f.svc.RequestCreditNote(f.ctx, bookkeeper, f.idem(), inv.ID, CreditNoteInput{Reason: "too much",
		Lines: []LineInput{{Description: "x", Quantity: 1, UnitPriceCents: 9000, TaxRateBp: 2100}}})
	wantCode(t, err, httperror.CodeValidation) // 108.90 > 84.70 left

	_, err = f.svc.RequestRefund(f.ctx, bookkeeper, f.idem(), cn.ID, RefundInput{AmountCents: 3631, Method: "bank_transfer"})
	wantCode(t, err, httperror.CodeValidation)
	approveIf(f.svc.RequestRefund(f.ctx, bookkeeper, f.idem(), cn.ID, RefundInput{AmountCents: 3630, Method: "bank_transfer", Reference: "R-1"}))
	got, _ = f.svc.GetInvoice(f.ctx, inv.ID)
	if got.RefundableCents != 0 || got.RefundedCents != 3630 || got.Status != domain.StatusPaid || len(got.Refunds) != 1 {
		t.Fatalf("after refund: %+v", got.InvoiceSummary)
	}

	// The ledger shows the refund going out.
	l, err := f.svc.GetLedger(f.ctx, Page{})
	if err != nil || l.InCents != 12100 || l.OutCents != 3630 || l.Entries[0].Kind != "refund" {
		t.Fatalf("ledger = %+v %v", l, err)
	}

	// A full credit note on an unpaid invoice: the second number in the series.
	other := f.issued()
	full := approveIf(f.svc.RequestCreditNote(f.ctx, bookkeeper, f.idem(), other.ID, CreditNoteInput{Reason: "cancelled", Full: true}))
	note, _ = f.svc.GetCreditNote(f.ctx, full.ID)
	got, _ = f.svc.GetInvoice(f.ctx, other.ID)
	if note.Number != "FIN-CN-0002" || got.Status != domain.StatusCredited || got.DueCents != 0 {
		t.Fatalf("full credit: %s %s", note.Number, got.Status)
	}
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM credit_notes WHERE id = $1`, full.ID); err == nil {
		t.Fatal("credit notes must be immutable")
	}
}

func TestPeriodLock(t *testing.T) {
	f := newFixture(t, 1_000_000)
	inv := f.issued() // issued 2026-11-02
	if _, err := f.svc.ClosePeriod(f.ctx, approver, "2026-10"); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.ClosePeriod(f.ctx, approver, "2026-10")
	wantCode(t, err, httperror.CodeConflict)
	_, err = f.svc.ClosePeriod(f.ctx, approver, "2026-12")
	wantCode(t, err, httperror.CodeValidation)

	_, err = f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), inv.ID, PaymentInput{PaidOn: "2026-10-31", AmountCents: 100, Method: "cash"})
	wantCode(t, err, httperror.CodePeriodClosed)
	amount, day := int64(500), "2026-10-15"
	vendor := "Printer"
	_, err = f.svc.CreateExpense(f.ctx, bookkeeper, f.idem(), ExpenseInput{Vendor: &vendor, AmountCents: &amount, OccurredOn: &day})
	wantCode(t, err, httperror.CodePeriodClosed)
	d := f.draft()
	_, err = f.svc.IssueInvoice(f.ctx, bookkeeper, d, IssueInput{IssueDate: "2026-10-20"})
	wantCode(t, err, httperror.CodePeriodClosed)

	// November is still open.
	f.pay(inv.ID, 100, "2026-11-03")

	// Closing November blocks the void of an invoice issued in November.
	if _, err := f.svc.ClosePeriod(f.ctx, approver, "2026-11"); err != nil {
		t.Fatal(err)
	}
	other, err := f.svc.GetInvoice(f.ctx, d)
	if err != nil || other.Status != domain.StatusDraft {
		t.Fatal(err)
	}
	_, err = f.svc.RecordPayment(f.ctx, bookkeeper, f.idem(), inv.ID, PaymentInput{PaidOn: "2026-11-10", AmountCents: 100, Method: "cash"})
	wantCode(t, err, httperror.CodePeriodClosed)

	wantCode(t, f.svc.ReopenPeriod(f.ctx, approver, "2026-11", ReopenInput{}), httperror.CodeValidation)
	if err := f.svc.ReopenPeriod(f.ctx, approver, "2026-11", ReopenInput{Reason: "late bank statement"}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.svc.ReopenPeriod(f.ctx, approver, "2026-11", ReopenInput{Reason: "again"}), httperror.CodeConflict)
	f.pay(inv.ID, 100, "2026-11-10")

	periods, _ := f.svc.ListPeriods(f.ctx)
	if len(periods) != 1 || periods[0].Month != "2026-10" || periods[0].ClosedBy.Subject != "sub-aki" {
		t.Fatalf("periods = %+v", periods)
	}
	log, err := f.svc.AuditLog(f.ctx, AuditFilter{Action: "period"})
	if err != nil || log.Total != 3 { // closed 2026-10, closed and reopened 2026-11
		t.Fatalf("period audit = %d %v", log.Total, err)
	}
	if !strings.Contains(string(log.Events[0].Metadata), "late bank statement") || log.Events[0].Action != "period.reopened" {
		t.Fatalf("reopen event = %+v", log.Events[0])
	}
}

func TestExpenses(t *testing.T) {
	f := newFixture(t, 0)
	vendor, amount := "Sound hire", int64(25000)
	res, err := f.svc.CreateExpense(f.ctx, bookkeeper, f.idem(), ExpenseInput{Vendor: &vendor, AmountCents: &amount})
	if err != nil || !res.Pending {
		t.Fatalf("expense request = %+v %v", res, err)
	}
	if l, _ := f.svc.ListExpenses(f.ctx, ExpenseFilter{}); l.Total != 0 {
		t.Fatal("a pending expense is not in the books")
	}
	a, err := f.svc.Approve(f.ctx, approver, res.ID, DecisionInput{})
	if err != nil {
		t.Fatal(err)
	}
	id := *a.ResultID
	e, _ := f.svc.GetExpense(f.ctx, id)
	if e.OccurredOn != "2026-11-11" || e.AmountCents != 25000 {
		t.Fatalf("expense = %+v", e)
	}

	more := int64(30000)
	_, err = f.svc.UpdateExpense(f.ctx, bookkeeper, id, ExpenseInput{AmountCents: &more})
	wantCode(t, err, httperror.CodeValidation)
	desc := "PA and mics"
	if e, err = f.svc.UpdateExpense(f.ctx, bookkeeper, id, ExpenseInput{Description: &desc}); err != nil || e.Description != desc {
		t.Fatalf("update = %+v %v", e, err)
	}

	wantCode(t, f.svc.DeleteExpense(f.ctx, bookkeeper, id, DeleteInput{}), httperror.CodeValidation)
	if err := f.svc.DeleteExpense(f.ctx, bookkeeper, id, DeleteInput{Reason: "duplicate"}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.svc.DeleteExpense(f.ctx, bookkeeper, id, DeleteInput{Reason: "again"}), httperror.CodeConflict)
	if l, _ := f.svc.ListExpenses(f.ctx, ExpenseFilter{}); l.Total != 0 {
		t.Fatal("deleted expenses leave the list")
	}
	if l, _ := f.svc.ListExpenses(f.ctx, ExpenseFilter{IncludeDeleted: true}); l.Total != 1 || !l.Expenses[0].Deleted || l.Expenses[0].DeletedReason != "duplicate" {
		t.Fatalf("include_deleted = %+v", l)
	}
	if l, _ := f.svc.GetLedger(f.ctx, Page{}); l.OutCents != 0 {
		t.Fatal("deleted expenses leave the ledger")
	}
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM expenses WHERE id = $1`, id); err == nil {
		t.Fatal("expenses must never be hard-deleted")
	}
	log, _ := f.svc.AuditLog(f.ctx, AuditFilter{EntityType: "expense"})
	if log.Total != 3 {
		t.Fatalf("expense audit events = %d, want created, updated, deleted", log.Total)
	}
}

func TestLedgerAndDashboard(t *testing.T) {
	f := newFixture(t, 1_000_000)
	inv := f.issued()
	f.pay(inv.ID, 5000, "2026-11-03")
	for i := 0; i < 3; i++ {
		v, amt, day := "Vendor", int64(1000), "2026-11-0"+string(rune('4'+i))
		if _, err := f.svc.CreateExpense(f.ctx, bookkeeper, f.idem(), ExpenseInput{Vendor: &v, AmountCents: &amt, OccurredOn: &day}); err != nil {
			t.Fatal(err)
		}
	}
	l, err := f.svc.GetLedger(f.ctx, Page{Limit: 2})
	if err != nil || l.Total != 4 || len(l.Entries) != 2 || l.NetCents != 2000 || l.Entries[0].BalanceCents != 2000 {
		t.Fatalf("ledger = %+v %v", l, err)
	}
	l, _ = f.svc.GetLedger(f.ctx, Page{Limit: 2, Offset: 2})
	if l.Entries[1].Kind != "payment" || l.Entries[1].BalanceCents != 5000 {
		t.Fatalf("ledger page 2 = %+v", l.Entries)
	}
	d, err := f.svc.GetDashboard(f.ctx)
	if err != nil || d.OutstandingCents != 7100 || d.ReceivedMonthCents != 5000 || d.SpentMonthCents != 3000 || d.NetTotalCents != 2000 {
		t.Fatalf("dashboard = %+v %v", d, err)
	}
}
