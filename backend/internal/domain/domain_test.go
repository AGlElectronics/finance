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
		{"rounding up", 100, 999, 2100, 999, 210},    // 209.79 -> 210
		{"third of a cent", 33, 100, 0, 33, 0},       // 0.33 * 1.00
		{"negative discount", 100, -500, 2100, -500, -105},
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

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		stored      string
		total, paid int64
		want        string
	}{
		{StatusDraft, 1000, 0, StatusDraft},
		{StatusVoid, 1000, 500, StatusVoid},
		{StatusIssued, 1000, 0, StatusIssued},
		{StatusIssued, 1000, 400, StatusPartial},
		{StatusIssued, 1000, 1000, StatusPaid},
		{StatusIssued, 0, 0, StatusIssued}, // zero-total invoice never auto-pays
	}
	for _, c := range cases {
		if got := DeriveStatus(c.stored, c.total, c.paid); got != c.want {
			t.Errorf("DeriveStatus(%s, %d, %d) = %s, want %s", c.stored, c.total, c.paid, got, c.want)
		}
	}
}
