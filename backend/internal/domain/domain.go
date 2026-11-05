// Package domain holds Finance's core types and the integer money math.
//
// All money is integer cents in a single currency (EUR). Quantities are
// stored in hundredths (1.5 hours -> 150) so every total is exact integer
// arithmetic; tax rates are basis points (21% -> 2100).
package domain

// Invoice statuses as stored in the database. The API additionally derives
// "partial" and "paid" from the recorded payments (see DeriveStatus).
const (
	StatusDraft  = "draft"
	StatusIssued = "issued"
	StatusVoid   = "void"
)

// Derived statuses (never stored).
const (
	StatusPartial = "partial"
	StatusPaid    = "paid"
)

// Currency is the only currency this proof of concept speaks.
const Currency = "EUR"

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

// DeriveStatus maps the stored status plus payment state to what the UI
// shows: draft, issued, partial, paid or void.
func DeriveStatus(stored string, totalCents, paidCents int64) string {
	if stored != StatusIssued {
		return stored
	}
	switch {
	case totalCents > 0 && paidCents >= totalCents:
		return StatusPaid
	case paidCents > 0:
		return StatusPartial
	default:
		return StatusIssued
	}
}

// LineSubtotal is quantity (hundredths) times unit price, rounded half away
// from zero to whole cents.
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
