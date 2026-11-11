// Package domain holds Finance's core types and the integer money math.
//
// All money is integer cents in the installation's one currency
// (FINANCE_CURRENCY). Quantities are stored in hundredths (1.5 hours -> 150)
// so every total is exact integer arithmetic; tax rates are basis points
// (21% -> 2100).
package domain

// Invoice statuses as stored in the database. The API additionally derives
// "partial", "paid" and "credited" (see DeriveStatus).
const (
	StatusDraft  = "draft"
	StatusIssued = "issued"
	StatusVoid   = "void"
)

// Derived statuses (never stored).
const (
	StatusPartial  = "partial"
	StatusPaid     = "paid"
	StatusCredited = "credited"
)

// Bounds on invoice and credit note lines. They keep every product and sum
// far inside int64: a line subtotal is at most MaxLineSubtotalCents, its tax
// at most that times 10000, and a document has at most MaxLines lines.
const (
	MaxUnitPriceCents    int64 = 1_000_000_000       // 10,000,000.00
	MaxQuantityHundredth int64 = 100_000_000         // 1,000,000 units
	MaxLineSubtotalCents int64 = 100_000_000_000_000 // 1,000,000,000,000.00
	MaxLines                   = 200
	MaxAmountCents       int64 = 1_000_000_000_000_000 // payments, expenses, refunds
)

// PaymentMethods accepted on a payment. "reference" plus "method" are the
// hook a later bank feed would match transactions against.
var PaymentMethods = []string{"bank_transfer", "cash", "card", "other"}

// ValidPaymentMethod reports whether m is an accepted payment method.
func ValidPaymentMethod(m string) bool {
	for _, v := range PaymentMethods {
		if v == m {
			return true
		}
	}
	return false
}

// ExpenseCategories groups money going out. Mirrors the CHECK constraint on
// the expenses table.
var ExpenseCategories = []string{"venue", "equipment", "travel", "supplies", "services", "fees", "other"}

// ValidExpenseCategory reports whether c is a known expense category.
func ValidExpenseCategory(c string) bool {
	for _, v := range ExpenseCategories {
		if v == c {
			return true
		}
	}
	return false
}

// Balance is the money position of one issued invoice.
type Balance struct {
	TotalCents    int64
	CreditedCents int64 // sum of its credit notes
	PaidCents     int64 // sum of its payments
	RefundedCents int64 // sum of refunds against its credit notes
}

// NetTotal is what the customer owes after credit notes.
func (b Balance) NetTotal() int64 { return b.TotalCents - b.CreditedCents }

// NetPaid is what the customer paid after refunds.
func (b Balance) NetPaid() int64 { return b.PaidCents - b.RefundedCents }

// Due is what the customer still owes (never negative).
func (b Balance) Due() int64 {
	if d := b.NetTotal() - b.NetPaid(); d > 0 {
		return d
	}
	return 0
}

// Refundable is what was paid beyond the credited total and not yet paid
// back (never negative).
func (b Balance) Refundable() int64 {
	if r := b.NetPaid() - b.NetTotal(); r > 0 {
		return r
	}
	return 0
}

// Creditable is the part of the invoice not yet covered by credit notes.
func (b Balance) Creditable() int64 { return b.NetTotal() }

// DeriveStatus maps the stored status plus the balance to what the UI
// shows: draft, issued, partial, paid, credited or void.
func DeriveStatus(stored string, b Balance) string {
	if stored != StatusIssued {
		return stored
	}
	switch {
	case b.TotalCents > 0 && b.NetTotal() <= 0:
		return StatusCredited
	case b.NetTotal() > 0 && b.NetPaid() >= b.NetTotal():
		return StatusPaid
	case b.NetPaid() > 0:
		return StatusPartial
	default:
		return StatusIssued
	}
}

// LineSubtotal is quantity (hundredths) times unit price, rounded half away
// from zero to whole cents. Callers keep the inputs within the bounds above.
func LineSubtotal(quantityHundredths, unitPriceCents int64) int64 {
	return roundDiv(quantityHundredths*unitPriceCents, 100)
}

// LineTax applies a basis-point tax rate to a subtotal.
func LineTax(subtotalCents int64, taxRateBp int32) int64 {
	return roundDiv(subtotalCents*int64(taxRateBp), 10000)
}

// roundDiv divides n by d (d > 0), rounding half away from zero.
func roundDiv(n, d int64) int64 {
	if n >= 0 {
		return (n + d/2) / d
	}
	return (n - d/2) / d
}
