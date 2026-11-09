package einvoice

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhuvaoanh123/hoadon-kho/internal/classify"
	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
)

type expLine struct {
	Name    *string  `json:"name"`
	Unit    *string  `json:"unit"`
	Qty     *float64 `json:"qty"`
	Amount  *float64 `json:"amount"`
	VATRate *string  `json:"vat_rate"`
	Kind    *string  `json:"kind"`
}

type expInvoice struct {
	ShouldParse *bool     `json:"should_parse"`
	Form        *string   `json:"form"`
	Serial      *string   `json:"serial"`
	Number      *string   `json:"number"`
	Date        *string   `json:"date"`
	SellerTax   *string   `json:"seller_tax"`
	LineCount   *int      `json:"line_count"`
	Lines       []expLine `json:"lines"`
	BeforeVAT   *float64  `json:"total_before_vat"`
	VAT         *float64  `json:"total_vat"`
	Total       *float64  `json:"total"`
}

type expFile struct {
	expInvoice
	OCR      bool         `json:"ocr"`
	Note     string       `json:"note"`
	Invoices []expInvoice `json:"invoices"`
}

// TestCorpusNoSilentErrors: any value read differently from the expected one must be flagged to the user.
func TestCorpusNoSilentErrors(t *testing.T) {
	sidecars, _ := filepath.Glob("../../testdata/corpus/*.expected.json")
	if len(sidecars) == 0 {
		t.Fatal("no corpus")
	}
	var exact, flagged int
	for _, sc := range sidecars {
		path := strings.TrimSuffix(sc, ".expected.json")
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			var exp expFile
			raw, _ := os.ReadFile(sc)
			if err := json.Unmarshal(raw, &exp); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			res := ParseAny(name, data)
			want := exp.Invoices
			if len(want) == 0 {
				want = []expInvoice{exp.expInvoice}
			}
			if want[0].ShouldParse != nil && !*want[0].ShouldParse {
				for _, p := range res {
					if p.Err == nil {
						t.Errorf("should be rejected, parsed %+v", p.Invoice.Number)
					}
				}
				return
			}
			var got []*model.Invoice
			for _, p := range res {
				if p.Err != nil {
					if exp.OCR && strings.Contains(p.Err.Error(), "OCR") {
						t.Skip("Windows OCR not available here")
					}
					t.Fatalf("parse error: %v", p.Err)
				}
				got = append(got, p.Invoice)
			}
			if len(got) != len(want) {
				t.Fatalf("invoices: got %d, want %d", len(got), len(want))
			}
			var c classify.Classifier
			for i, inv := range got {
				for j := range inv.Lines {
					l := &inv.Lines[j]
					l.Kind, l.Reason, l.Certain = c.Classify(*l)
				}
				Verify(inv)
				e, f := compare(t, inv, want[i])
				exact += e
				flagged += f
			}
		})
	}
	t.Logf("values read exactly: %d, wrong but flagged to the user: %d", exact, flagged)
}

// compare reports silent mismatches as errors and returns (exact, flagged) value counts.
func compare(t *testing.T, inv *model.Invoice, e expInvoice) (int, int) {
	exact, flagged := 0, 0
	check := func(ok, raised bool, what string) {
		switch {
		case ok:
			exact++
		case raised:
			flagged++
			t.Logf("flagged: %s", what)
		default:
			t.Errorf("SILENT error: %s", what)
		}
	}
	invFlag := len(inv.Issues) > 0
	str := func(want *string, got, field string) {
		if want != nil {
			w := *want
			if field == "number" {
				w = strings.TrimLeft(w, "0")
			}
			check(w == got, invFlag, fmt.Sprintf("%s: want %q got %q", field, w, got))
		}
	}
	str(e.Form, inv.Form, "form")
	str(e.Serial, inv.Serial, "serial")
	str(e.Number, inv.Number, "number")
	str(e.Date, inv.Date, "date")
	str(e.SellerTax, inv.Seller.TaxCode, "seller_tax")
	if e.LineCount != nil {
		check(*e.LineCount == len(inv.Lines), invFlag, fmt.Sprintf("line count: want %d got %d", *e.LineCount, len(inv.Lines)))
	}
	for i, el := range e.Lines {
		if i >= len(inv.Lines) {
			break
		}
		l := inv.Lines[i]
		raised := len(l.Issues) > 0 || invFlag && len(inv.Lines) != len(e.Lines)
		near := func(want *float64, got float64, field string) {
			if want != nil {
				check(math.Abs(*want-got) <= 1, raised, fmt.Sprintf("line %d %s: want %v got %v", i+1, field, *want, got))
			}
		}
		near(el.Qty, l.Qty, "qty")
		near(el.Amount, l.Amount, "amount")
		if el.VATRate != nil {
			check(*el.VATRate == l.VATRate, raised, fmt.Sprintf("line %d vat rate: want %q got %q", i+1, *el.VATRate, l.VATRate))
		}
		if el.Kind != nil {
			check(*el.Kind == string(l.Kind), raised, fmt.Sprintf("line %d kind: want %s got %s (%s)", i+1, *el.Kind, l.Kind, l.Name))
		}
		if el.Name != nil {
			check(strings.EqualFold(strings.Join(strings.Fields(*el.Name), " "), l.Name), raised, fmt.Sprintf("line %d name: want %q got %q", i+1, *el.Name, l.Name))
		}
		if el.Unit != nil {
			check(strings.EqualFold(*el.Unit, l.Unit), raised, fmt.Sprintf("line %d unit: want %q got %q", i+1, *el.Unit, l.Unit))
		}
	}
	sum := func(want *float64, got float64, field string) {
		if want != nil {
			check(math.Abs(*want-got) <= 2, invFlag, fmt.Sprintf("%s: want %v got %v", field, *want, got))
		}
	}
	sum(e.BeforeVAT, inv.TotalBeforeVAT, "total before VAT")
	sum(e.VAT, inv.TotalVAT, "total VAT")
	sum(e.Total, inv.Total, "total")
	return exact, flagged
}
