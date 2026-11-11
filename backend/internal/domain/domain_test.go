package domain

import "testing"

func TestLineMath(t *testing.T) {
	cases := []struct {
		name          string
		qtyHundredths int64
		unitCents     int64
		taxBp         int32
		wantSubtotal  int64
		wantTax       int64
	}{
		{"one unit no tax", 100, 5000, 0, 5000, 0},
		{"1.5 at 10.00", 150, 1000, 0, 1500, 0},
		{"21% vat", 100, 10000, 2100, 10000, 2100},
		{"rounding up", 100, 999, 2100, 999, 210},     // 209.79 -> 210
		{"third of a cent", 33, 100, 0, 33, 0},        // 0.33 * 1.00
		{"rounding down", 100, 1001, 2100, 1001, 210}, // 210.21 -> 210
	}
	for _, c := range cases {
		sub := LineSubtotal(c.qtyHundredths, c.unitCents)
		if sub != c.wantSubtotal {
			t.Errorf("%s: subtotal = %d, want %d", c.name, sub, c.wantSubtotal)
		}
		if tax := LineTax(sub, c.taxBp); tax != c.wantTax {
			t.Errorf("%s: tax = %d, want %d", c.name, tax, c.wantTax)
		}
	}
}

func TestBoundsFitInt64(t *testing.T) {
	// The product of the largest quantity and price fits; the subtotal is then
	// checked against MaxLineSubtotalCents before any tax is computed.
	if sub := LineSubtotal(MaxQuantityHundredth, MaxUnitPriceCents); sub <= 0 {
		t.Fatalf("largest quantity x price overflows: %d", sub)
	}
	if tax := LineTax(MaxLineSubtotalCents, 10000); tax != MaxLineSubtotalCents {
		t.Fatalf("100%% tax on the largest subtotal = %d", tax)
	}
	if total := int64(MaxLines) * 2 * MaxLineSubtotalCents; total <= 0 {
		t.Fatalf("largest document total overflows: %d", total)
	}
}

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		stored string
		b      Balance
		want   string
	}{
		{StatusDraft, Balance{TotalCents: 1000}, StatusDraft},
		{StatusVoid, Balance{TotalCents: 1000}, StatusVoid},
		{StatusIssued, Balance{TotalCents: 1000}, StatusIssued},
		{StatusIssued, Balance{TotalCents: 1000, PaidCents: 400}, StatusPartial},
		{StatusIssued, Balance{TotalCents: 1000, PaidCents: 1000}, StatusPaid},
		{StatusIssued, Balance{}, StatusIssued}, // zero-total invoice never auto-pays
		{StatusIssued, Balance{TotalCents: 1000, CreditedCents: 1000}, StatusCredited},
		{StatusIssued, Balance{TotalCents: 1000, CreditedCents: 300, PaidCents: 700}, StatusPaid},
		{StatusIssued, Balance{TotalCents: 1000, CreditedCents: 300, PaidCents: 1000, RefundedCents: 300}, StatusPaid},
	}
	for _, c := range cases {
		if got := DeriveStatus(c.stored, c.b); got != c.want {
			t.Errorf("DeriveStatus(%s, %+v) = %s, want %s", c.stored, c.b, got, c.want)
		}
	}
}

func TestBalance(t *testing.T) {
	b := Balance{TotalCents: 1000, CreditedCents: 300, PaidCents: 1000}
	if b.Due() != 0 || b.Refundable() != 300 || b.Creditable() != 700 {
		t.Fatalf("due %d refundable %d creditable %d", b.Due(), b.Refundable(), b.Creditable())
	}
	b.RefundedCents = 300
	if b.Refundable() != 0 {
		t.Fatalf("refundable after refund = %d", b.Refundable())
	}
	b = Balance{TotalCents: 1000, PaidCents: 400, CreditedCents: 200}
	if b.Due() != 400 || b.Refundable() != 0 {
		t.Fatalf("due %d refundable %d", b.Due(), b.Refundable())
	}
}
