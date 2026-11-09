package export

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/nhuvaoanh123/hoadon-kho/internal/classify"
	"github.com/nhuvaoanh123/hoadon-kho/internal/einvoice"
	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/store"
)

// roundTrip writes f and reopens it, proving the file is valid xlsx.
func roundTrip(t *testing.T, f *excelize.File) *excelize.File {
	t.Helper()
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	g, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func find(t *testing.T, f *excelize.File, sheet, text string) bool {
	t.Helper()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		for _, c := range r {
			if strings.Contains(c, text) {
				return true
			}
		}
	}
	return false
}

func TestWorkbooks(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "kho.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data, _ := os.ReadFile("../../testdata/hoadon-mau.xml")
	inv, _, err := einvoice.ParseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	var c classify.Classifier
	for i := range inv.Lines {
		inv.Lines[i].Kind, inv.Lines[i].Reason, _ = c.Classify(inv.Lines[i])
	}
	inv.Lines = append(inv.Lines, model.Line{STT: 8, Nature: 1, Name: "Phí vận chuyển", Amount: 1000000, VATRate: "8%", VATAmount: 80000, Kind: model.KindService})
	if _, err := s.SaveInvoice(inv); err != nil {
		t.Fatal(err)
	}
	set, _ := s.Settings()
	set["company_name"] = "CÔNG TY TNHH NGƯỜI MUA MẪU"
	invs, _ := s.Invoices("", "")

	f := roundTrip(t, Receipts(set, invs))
	if !find(t, f, "PN00001", "PHIẾU NHẬP KHO") || !find(t, f, "PN00001", "Gas lạnh R22") {
		t.Error("receipt content missing")
	}
	if find(t, f, "PN00001", "Phí vận chuyển") {
		t.Error("service must not be on the stock receipt")
	}
	if !find(t, f, "PN00001", "Bảy mươi hai triệu hai trăm bốn mươi hai nghìn năm trăm chín mươi ba đồng") {
		t.Error("amount in words missing")
	}

	f = roundTrip(t, PurchaseList(set, invs, "2025-09-01", "2025-09-30"))
	if !find(t, f, "Bang ke mua vao", "chịu thuế suất 8%") || !find(t, f, "Bang ke mua vao", "chịu thuế suất 10%") {
		t.Error("rate groups missing")
	}

	rows, _ := s.Summary("", "")
	if len(rows) != 7 {
		t.Fatalf("stock rows = %d", len(rows))
	}
	f = roundTrip(t, StockSummary(set, rows, "", ""))
	if !find(t, f, "Nhap xuat ton", "Quạt công nghiệp lạnh YWF4D-400S-E5L") {
		t.Error("stock summary missing item")
	}

	moves, _ := s.Ledger(0, "", "")
	var cards []Card
	for _, r := range rows {
		cd := Card{Item: r}
		for _, m := range moves {
			if m.ItemID == r.ItemID {
				cd.Moves = append(cd.Moves, m)
			}
		}
		cards = append(cards, cd)
	}
	f = roundTrip(t, StockCards(set, cards, "", ""))
	if len(f.GetSheetList()) != 7 || !find(t, f, "HH00007", "THẺ KHO") {
		t.Errorf("stock cards: %v", f.GetSheetList())
	}

	f = roundTrip(t, Services(set, invs, "", ""))
	if !find(t, f, "Chi phi dich vu", "Phí vận chuyển") || find(t, f, "Chi phi dich vu", "Gas lạnh") {
		t.Error("service list wrong")
	}
	f = roundTrip(t, AccountingImport(set, invs))
	got, _ := f.GetRows("Chung tu mua hang")
	if len(got) != 9 || got[0][0] != "Ngày hạch toán" {
		t.Errorf("import rows = %d", len(got))
	}
}
