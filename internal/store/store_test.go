package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

func open(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "thư mục có dấu cách")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "kho.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func inv(no, date string, lines ...model.Line) *model.Invoice {
	return &model.Invoice{Form: "1", Serial: "C25AAA", Number: no, Date: date,
		Seller: model.Party{Name: "NCC Mẫu", TaxCode: "0100000003"}, Lines: lines}
}

func goods(name string, qty, amount float64) model.Line {
	return model.Line{Nature: 1, Name: name, Unit: "Cái", Qty: qty, Price: amount / qty, Amount: amount, VATRate: "8%", Kind: model.KindGoods}
}

func TestStockFlow(t *testing.T) {
	s := open(t)
	svc := model.Line{Nature: 1, Name: "Phí vận chuyển", Amount: 500000, VATRate: "8%", Kind: model.KindService, UserSet: true}
	disc := model.Line{Nature: 3, Name: "Chiết khấu", Amount: 100000, Kind: model.KindDiscount}
	id1, err := s.SaveInvoice(inv("1", "2026-01-05", goods("Lọc ẩm", 10, 1000000), svc, disc))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveInvoice(inv("0001", "2026-01-05", goods("Lọc ẩm", 1, 1))); err != ErrDuplicate {
		t.Fatalf("duplicate not detected: %v", err)
	}
	// 10 cái, chiết khấu 100.000 chia theo giá trị: 1.000.000 / 1.500.000 × 100.000 = 66.667
	if _, err := s.SaveInvoice(inv("2", "2026-01-10", goods("Lọc ẩm", 10, 1200000))); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Summary("", "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("summary: %v %+v", err, rows)
	}
	if r := rows[0]; r.CloseQty != 20 || r.CloseVal != 933333+1200000 {
		t.Fatalf("closing: %+v", r)
	}
	doc, err := s.StockOut(rows[0].ItemID, "2026-01-20", 5, "Bán lẻ")
	if err != nil || doc != "PX00001" {
		t.Fatalf("stock out: %q %v", doc, err)
	}
	if _, err := s.StockOut(rows[0].ItemID, "2026-01-06", 11, ""); err == nil {
		t.Fatal("expected negative-stock refusal")
	}
	rows, _ = s.Summary("2026-01-08", "2026-01-31")
	r := rows[0]
	// tồn đầu 10 / 933.333; bình quân sau nhập: 2.133.333/20 = 106.666,65 → xuất 5 = 533.333
	if r.OpenQty != 10 || r.InQty != 10 || r.OutQty != 5 || r.OutVal != 533333 || r.CloseVal != 1600000 {
		t.Fatalf("period: %+v", r)
	}
	if err := s.DeleteInvoice(id1); err != nil {
		t.Fatalf("delete should succeed while 15 remain: %v", err)
	}
	if k, ok := s.RuleLookup("phí vận chuyển|"); !ok || k != model.KindService {
		t.Fatalf("rule not stored: %v %v", k, ok)
	}
	got, err := s.Invoices("", "")
	if err != nil || len(got) != 1 || got[0].ReceiptNo != "PN00002" || got[0].Lines[0].ItemCode != "HH00001" {
		t.Fatalf("invoices: %v %+v", err, got)
	}
}

func TestDeleteBlockedByIssue(t *testing.T) {
	s := open(t)
	id, err := s.SaveInvoice(inv("9", "2026-02-01", goods("Gas R22", 4, 400000)))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Summary("", "")
	if _, err := s.StockOut(rows[0].ItemID, "2026-02-02", 4, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteInvoice(id); err == nil {
		t.Fatal("delete must be refused while stock was issued")
	}
}

// TestMigrateTonePlacement checks that Open folds old-style item/kind_rules
// keys to the new tone placement, so both spellings resolve to one item.
func TestMigrateTonePlacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mig")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "kho.db")
	name, unit := textutil.Clean("Sơn hoà tan"), textutil.Clean("Lít")
	oldItemKey := textutil.Lower(name) + "|" + textutil.Lower(unit)
	oldRuleKey := textutil.Lower("Phí vận chuyển hoà tan") + "|"
	if newKey := textutil.ItemKey(name, unit); newKey == oldItemKey {
		t.Fatalf("fixture key %q does not actually need folding", oldItemKey)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO items(code,name,unit,key,created_at) VALUES(?,?,?,?,?)`,
		"HH00001", name, unit, oldItemKey, now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO kind_rules(key,kind,updated_at) VALUES(?,?,?)`,
		oldRuleKey, string(model.KindService), now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	newItemKey := textutil.ItemKey(name, unit)
	if s2.KnownItem(oldItemKey) {
		t.Fatalf("old key %q should have been migrated away", oldItemKey)
	}
	if !s2.KnownItem(newItemKey) {
		t.Fatalf("new key %q not found after migration", newItemKey)
	}
	if !s2.KnownItem(textutil.ItemKey("Sơn hòa tan", "Lít")) {
		t.Fatal("new-style spelling should resolve to the migrated item")
	}
	newRuleKey := textutil.ToneNew(oldRuleKey)
	if _, ok := s2.RuleLookup(oldRuleKey); ok {
		t.Fatalf("old rule key %q should have been migrated away", oldRuleKey)
	}
	if k, ok := s2.RuleLookup(newRuleKey); !ok || k != model.KindService {
		t.Fatalf("rule not migrated: %v %v", k, ok)
	}
}
