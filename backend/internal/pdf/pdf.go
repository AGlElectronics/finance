// Package pdf writes simple A4 PDF documents: text in the standard
// Helvetica fonts (WinAnsi encoding) and lines. It has no dependencies and
// its output is deterministic: the same input always gives the same bytes,
// which lets Finance store a SHA-256 of every issued document.
package pdf

import (
	"bytes"
	"fmt"
	"strings"
)

// A4 page size in points.
const (
	PageWidth  = 595.28
	PageHeight = 841.89
)

// Doc is a document under construction. Coordinates are in points from the
// top-left corner of the page.
type Doc struct {
	pages []*bytes.Buffer
	cur   *bytes.Buffer
}

// New returns a document with one empty page.
func New() *Doc {
	d := &Doc{}
	d.AddPage()
	return d
}

// AddPage starts a new page; later drawing goes there.
func (d *Doc) AddPage() {
	d.cur = &bytes.Buffer{}
	d.pages = append(d.pages, d.cur)
}

// Pages is the number of pages so far.
func (d *Doc) Pages() int { return len(d.pages) }

// Text draws s with its baseline at (x, y).
func (d *Doc) Text(x, y, size float64, bold bool, s string) {
	font := "F1"
	if bold {
		font = "F2"
	}
	fmt.Fprintf(d.cur, "BT /%s %s Tf %s %s Td (%s) Tj ET\n",
		font, num(size), num(x), num(PageHeight-y), escape(encode(s)))
}

// TextRight draws s so that it ends at x.
func (d *Doc) TextRight(x, y, size float64, bold bool, s string) {
	d.Text(x-Width(s, size, bold), y, size, bold, s)
}

// Line draws a thin line from (x1, y1) to (x2, y2).
func (d *Doc) Line(x1, y1, x2, y2 float64) {
	fmt.Fprintf(d.cur, "0.5 w %s %s m %s %s l S\n", num(x1), num(PageHeight-y1), num(x2), num(PageHeight-y2))
}

// Bytes renders the document. title goes into the document information.
func (d *Doc) Bytes(title string) []byte {
	var out bytes.Buffer
	offsets := []int{}
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	n := len(d.pages)
	// Objects: 1 catalog, 2 pages, 3-4 fonts, 5 info, then page + content pairs.
	kids := make([]string, n)
	for i := range d.pages {
		kids[i] = fmt.Sprintf("%d 0 R", 6+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	obj(fmt.Sprintf("<< /Title (%s) /Producer (Con Services Finance) >>", escape(encode(title))))
	for i, p := range d.pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] "+
			"/Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>",
			num(PageWidth), num(PageHeight), 7+2*i))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", p.Len(), p.String()))
	}

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info 5 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes()
}

// Width is the width of s in points at size.
func Width(s string, size float64, bold bool) float64 {
	widths := helvetica
	if bold {
		widths = helveticaBold
	}
	total := 0
	for _, b := range encode(s) {
		if b >= 32 && b <= 126 {
			total += widths[b-32]
		} else {
			total += 556
		}
	}
	return float64(total) * size / 1000
}

// Wrap splits s into lines no wider than maxWidth, breaking at spaces and
// at explicit newlines.
func Wrap(s string, size, maxWidth float64, bold bool) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if Width(line+" "+w, size, bold) <= maxWidth {
				line += " " + w
				continue
			}
			out = append(out, line)
			line = w
		}
		out = append(out, line)
	}
	return out
}

func num(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func escape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch c {
		case '(', ')', '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// winAnsi maps the characters of Windows-1252 that differ from Latin-1.
var winAnsi = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87,
	'ˆ': 0x88, '‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91,
	'’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97, '˜': 0x98,
	'™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// encode turns UTF-8 into WinAnsi bytes; characters it cannot show become "?".
func encode(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			out = append(out, ' ')
		case r >= 0x20 && r <= 0x7E, r >= 0xA0 && r <= 0xFF:
			out = append(out, byte(r))
		default:
			if b, ok := winAnsi[r]; ok {
				out = append(out, b)
			} else if r >= 0x20 {
				out = append(out, '?')
			}
		}
	}
	return out
}

// Glyph widths (1/1000 em) of the printable ASCII characters 32-126.
var helvetica = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
}

var helveticaBold = [95]int{
	278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
	975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
	333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
	611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
}
