package pdf

import (
	"fmt"
	"sort"
	"strings"
)

// Party is the seller or the customer as printed.
type Party struct {
	Name    string
	Address string
	Email   string
	// TaxID is the VAT number (seller) or tax ID (customer).
	TaxID string
}

// Line is one printed row.
type Line struct {
	Description    string
	QtyHundredths  int64
	UnitPriceCents int64
	TaxRateBp      int32
	SubtotalCents  int64
	TaxCents       int64
}

// Document is an invoice or a credit note as printed.
type Document struct {
	// Title is "Invoice" or "Credit note".
	Title     string
	Number    string
	IssueDate string
	DueDate   string
	// Reference names the corrected invoice on a credit note.
	Reference string
	Reason    string
	Seller    Party
	Customer  Party
	Currency  string
	Lines     []Line
	Subtotal  int64
	Tax       int64
	Total     int64
	Notes     string
}

const (
	left   = 50.0
	right  = PageWidth - 50
	bottom = PageHeight - 70
)

// Render lays out doc on A4 pages and returns the PDF bytes.
func Render(doc Document) []byte {
	d := New()
	y := 60.0

	d.Text(left, y, 20, true, doc.Title)
	d.TextRight(right, y, 11, true, doc.Number)
	y += 30

	// Seller (left) and document facts (right).
	sy := y
	d.Text(left, sy, 10, true, doc.Seller.Name)
	sy += 13
	for _, l := range Wrap(doc.Seller.Address, 9, 250, false) {
		d.Text(left, sy, 9, false, l)
		sy += 12
	}
	if doc.Seller.TaxID != "" {
		d.Text(left, sy, 9, false, "VAT number: "+doc.Seller.TaxID)
		sy += 12
	}

	fy := y
	facts := [][2]string{{"Date", doc.IssueDate}}
	if doc.DueDate != "" {
		facts = append(facts, [2]string{"Due", doc.DueDate})
	}
	if doc.Reference != "" {
		facts = append(facts, [2]string{"Corrects invoice", doc.Reference})
	}
	facts = append(facts, [2]string{"Currency", doc.Currency})
	for _, f := range facts {
		d.TextRight(right-110, fy, 9, false, f[0])
		d.TextRight(right, fy, 9, true, f[1])
		fy += 13
	}
	y = maxf(sy, fy) + 18

	d.Text(left, y, 8, true, "BILL TO")
	y += 13
	d.Text(left, y, 10, true, doc.Customer.Name)
	y += 13
	for _, l := range Wrap(doc.Customer.Address, 9, 300, false) {
		d.Text(left, y, 9, false, l)
		y += 12
	}
	if doc.Customer.TaxID != "" {
		d.Text(left, y, 9, false, "Tax ID: "+doc.Customer.TaxID)
		y += 12
	}
	if doc.Customer.Email != "" {
		d.Text(left, y, 9, false, doc.Customer.Email)
		y += 12
	}
	if doc.Reason != "" {
		y += 8
		for _, l := range Wrap("Reason: "+doc.Reason, 9, right-left, false) {
			d.Text(left, y, 9, false, l)
			y += 12
		}
	}
	y += 18

	header := func() {
		d.Text(left, y, 8, true, "DESCRIPTION")
		d.TextRight(330, y, 8, true, "QTY")
		d.TextRight(410, y, 8, true, "UNIT PRICE")
		d.TextRight(460, y, 8, true, "VAT")
		d.TextRight(right, y, 8, true, "AMOUNT")
		y += 6
		d.Line(left, y, right, y)
		y += 14
	}
	header()
	for _, l := range doc.Lines {
		desc := Wrap(l.Description, 9, 230, false)
		if y+float64(len(desc))*12 > bottom {
			d.AddPage()
			y = 60
			header()
		}
		d.TextRight(330, y, 9, false, Quantity(l.QtyHundredths))
		d.TextRight(410, y, 9, false, Money(l.UnitPriceCents))
		d.TextRight(460, y, 9, false, Rate(l.TaxRateBp))
		d.TextRight(right, y, 9, false, Money(l.SubtotalCents))
		for _, dl := range desc {
			d.Text(left, y, 9, false, dl)
			y += 12
		}
		y += 4
	}

	if y+120 > bottom {
		d.AddPage()
		y = 60
	}
	d.Line(330, y, right, y)
	y += 16
	total := func(label, value string, bold bool) {
		d.TextRight(460, y, 9, bold, label)
		d.TextRight(right, y, 9, bold, value)
		y += 14
	}
	total("Subtotal", Money(doc.Subtotal), false)
	for _, t := range taxByRate(doc.Lines) {
		total(fmt.Sprintf("VAT %s on %s", Rate(t.rate), Money(t.base)), Money(t.tax), false)
	}
	total("Total "+doc.Currency, Money(doc.Total), true)

	if strings.TrimSpace(doc.Notes) != "" {
		y += 20
		for _, l := range Wrap(doc.Notes, 9, right-left, false) {
			if y > bottom {
				d.AddPage()
				y = 60
			}
			d.Text(left, y, 9, false, l)
			y += 12
		}
	}

	if d.Pages() > 1 {
		// Page numbers are drawn last so every page knows the count.
		n := d.Pages()
		for i := range d.pages {
			d.cur = d.pages[i]
			d.TextRight(right, PageHeight-40, 8, false, fmt.Sprintf("%s - page %d of %d", doc.Number, i+1, n))
		}
	}
	return d.Bytes(doc.Title + " " + doc.Number)
}

type rateTotal struct {
	rate      int32
	base, tax int64
}

func taxByRate(lines []Line) []rateTotal {
	m := map[int32]*rateTotal{}
	for _, l := range lines {
		t, ok := m[l.TaxRateBp]
		if !ok {
			t = &rateTotal{rate: l.TaxRateBp}
			m[l.TaxRateBp] = t
		}
		t.base += l.SubtotalCents
		t.tax += l.TaxCents
	}
	out := make([]rateTotal, 0, len(m))
	for _, t := range m {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rate < out[j].rate })
	return out
}

// Money formats cents as 1,234.56.
func Money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	whole := fmt.Sprintf("%d", cents/100)
	var sb strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return fmt.Sprintf("%s%s.%02d", sign, sb.String(), cents%100)
}

// Quantity formats hundredths without trailing zeros (1.5, 2, 0.25).
func Quantity(h int64) string {
	if h%100 == 0 {
		return fmt.Sprintf("%d", h/100)
	}
	s := fmt.Sprintf("%d.%02d", h/100, h%100)
	return strings.TrimRight(s, "0")
}

// Rate formats basis points as a percentage (2100 -> 21%, 550 -> 5.5%).
func Rate(bp int32) string {
	if bp%100 == 0 {
		return fmt.Sprintf("%d%%", bp/100)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%02d", bp/100, bp%100), "0") + "%"
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
