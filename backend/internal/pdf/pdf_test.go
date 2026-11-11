package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func sample(lines int) Document {
	doc := Document{
		Title: "Invoice", Number: "FIN-0007", IssueDate: "2026-11-02", DueDate: "2026-12-02",
		Seller:   Party{Name: "Con Org e.V.", Address: "Main Street 1\n12345 Town", TaxID: "DE123456789"},
		Customer: Party{Name: "Hotel Grün", Address: "Hotelweg 5\nStadt", TaxID: "ATU1234", Email: "billing@hotel.example"},
		Currency: "EUR", Notes: "Thank you (and see you next year) \\o/",
	}
	for i := 0; i < lines; i++ {
		doc.Lines = append(doc.Lines, Line{Description: fmt.Sprintf("Room night %d – €", i+1), QtyHundredths: 150,
			UnitPriceCents: 12000, TaxRateBp: 700, SubtotalCents: 18000, TaxCents: 1260})
		doc.Subtotal += 18000
		doc.Tax += 1260
	}
	doc.Total = doc.Subtotal + doc.Tax
	return doc
}

func TestRenderIsDeterministicAndWellFormed(t *testing.T) {
	a, b := Render(sample(3)), Render(sample(3))
	if !bytes.Equal(a, b) {
		t.Fatal("same input gave different bytes")
	}
	s := string(a)
	for _, want := range []string{"%PDF-1.4", "/Count 1", "FIN-0007", "Hotel Gr\xfcn", "(Thank you \\(and see you next year\\) \\\\o/)", "%%EOF", "VAT 7% on 540.00"} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF misses %q", want)
		}
	}
	// The xref offsets must point at the objects.
	idx := strings.LastIndex(s, "startxref\n")
	var xref int
	if _, err := fmt.Sscanf(s[idx+len("startxref\n"):], "%d", &xref); err != nil || !strings.HasPrefix(s[xref:], "xref") {
		t.Fatalf("startxref %d does not point at the xref table", xref)
	}
	var off int
	entry := strings.Split(s[xref:], "\n")[3] // object 1
	if _, err := fmt.Sscanf(entry, "%d", &off); err != nil || !strings.HasPrefix(s[off:], "1 0 obj") {
		t.Fatalf("xref entry %q does not point at object 1", entry)
	}
}

func TestRenderPaginates(t *testing.T) {
	s := string(Render(sample(80)))
	if !strings.Contains(s, "/Count 3") && !strings.Contains(s, "/Count 2") {
		t.Fatal("80 lines should need more than one page")
	}
	if !strings.Contains(s, "page 1 of") {
		t.Fatal("multi-page documents carry page numbers")
	}
}

func TestFormatting(t *testing.T) {
	cases := map[string]string{
		Money(123456789): "1,234,567.89", Money(-5): "-0.05", Money(100): "1.00",
		Quantity(150): "1.5", Quantity(200): "2", Quantity(25): "0.25",
		Rate(2100): "21%", Rate(550): "5.5%", Rate(0): "0%",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}
