// Package store persists invoices, items and stock movements in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

const schema = `
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS items(
  id INTEGER PRIMARY KEY, code TEXT UNIQUE NOT NULL, name TEXT NOT NULL, unit TEXT NOT NULL,
  key TEXT UNIQUE NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS invoices(
  id INTEGER PRIMARY KEY, form TEXT NOT NULL, serial TEXT NOT NULL, number TEXT NOT NULL, date TEXT NOT NULL,
  seller_name TEXT NOT NULL, seller_tax TEXT NOT NULL, seller_addr TEXT NOT NULL,
  buyer_name TEXT NOT NULL, buyer_tax TEXT NOT NULL, buyer_addr TEXT NOT NULL,
  currency TEXT NOT NULL, ex_rate REAL NOT NULL, pay_method TEXT NOT NULL, tax_auth_code TEXT NOT NULL,
  total_before_vat REAL NOT NULL, total_vat REAL NOT NULL, total REAL NOT NULL,
  receipt_no TEXT NOT NULL, source_file TEXT NOT NULL, created_at TEXT NOT NULL,
  UNIQUE(seller_tax, form, serial, number));
CREATE TABLE IF NOT EXISTS invoice_lines(
  id INTEGER PRIMARY KEY, invoice_id INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  stt INTEGER NOT NULL, nature INTEGER NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, unit TEXT NOT NULL,
  qty REAL NOT NULL, price REAL NOT NULL, amount REAL NOT NULL, vat_rate TEXT NOT NULL, vat_amount REAL NOT NULL,
  kind TEXT NOT NULL, reason TEXT NOT NULL, item_id INTEGER REFERENCES items(id), cost REAL NOT NULL);
CREATE TABLE IF NOT EXISTS movements(
  id INTEGER PRIMARY KEY, item_id INTEGER NOT NULL REFERENCES items(id), date TEXT NOT NULL,
  dir TEXT NOT NULL CHECK(dir IN ('IN','OUT')), qty REAL NOT NULL, amount REAL NOT NULL,
  doc_no TEXT NOT NULL, invoice_id INTEGER REFERENCES invoices(id) ON DELETE CASCADE,
  note TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS movements_item ON movements(item_id, date, id);
CREATE TABLE IF NOT EXISTS kind_rules(key TEXT PRIMARY KEY, kind TEXT NOT NULL, updated_at TEXT NOT NULL);
`

// Defaults for settings shown in the UI.
var Defaults = map[string]string{
	"company_name":    "",
	"company_address": "",
	"company_tax":     "",
	"warehouse":       "Kho hàng hóa",
	"form_basis":      "(Ban hành theo Thông tư số 133/2016/TT-BTC ngày 26/08/2016 của Bộ Tài chính)",
	"acct_goods":      "156",
	"acct_service":    "642",
	"acct_vat":        "1331",
	"acct_payable":    "331",
}

// ErrDuplicate means the invoice was already saved.
var ErrDuplicate = errors.New("hóa đơn này đã được nhập trước đó")

// Store wraps the database.
type Store struct{ db *sql.DB }

// Open opens (and creates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("khởi tạo CSDL: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().Format("2006-01-02 15:04:05") }

// Settings returns all settings merged over Defaults.
func (s *Store) Settings() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range Defaults {
		out[k] = v
	}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(k, "seq_") {
			out[k] = v
		}
	}
	return out, rows.Err()
}

// SaveSettings stores known setting keys.
func (s *Store) SaveSettings(m map[string]string) error {
	for k, v := range m {
		if _, ok := Defaults[k]; !ok {
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, strings.TrimSpace(v)); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

// nextDoc returns the next document number with prefix, e.g. PN00001.
func nextDoc(tx execer, prefix string) (string, error) {
	var n int
	err := tx.QueryRow(`SELECT CAST(value AS INTEGER) FROM settings WHERE key=?`, "seq_"+prefix).Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	n++
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "seq_"+prefix, fmt.Sprint(n)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%05d", prefix, n), nil
}

// RuleLookup returns the kind the user chose before for an item key.
func (s *Store) RuleLookup(key string) (model.Kind, bool) {
	var k string
	if err := s.db.QueryRow(`SELECT kind FROM kind_rules WHERE key=?`, key).Scan(&k); err != nil {
		return "", false
	}
	return model.Kind(k), true
}

// KnownItem reports whether an inventory item with this key exists.
func (s *Store) KnownItem(key string) bool {
	var id int64
	return s.db.QueryRow(`SELECT id FROM items WHERE key=?`, key).Scan(&id) == nil
}

// FindDuplicate returns the id of an already saved invoice with the same identity, or 0.
func (s *Store) FindDuplicate(inv *model.Invoice) int64 {
	var id int64
	_ = s.db.QueryRow(`SELECT id FROM invoices WHERE seller_tax=? AND form=? AND serial=? AND number=?`,
		inv.Seller.TaxCode, inv.Form, inv.Serial, strings.TrimLeft(inv.Number, "0")).Scan(&id)
	return id
}

func itemFor(tx *sql.Tx, l model.Line) (int64, string, error) {
	key := textutil.ItemKey(l.Name, l.Unit)
	var id int64
	var code string
	err := tx.QueryRow(`SELECT id, code FROM items WHERE key=?`, key).Scan(&id, &code)
	if err == nil {
		return id, code, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", err
	}
	if code, err = nextDoc(tx, "HH"); err != nil {
		return 0, "", err
	}
	res, err := tx.Exec(`INSERT INTO items(code,name,unit,key,created_at) VALUES(?,?,?,?,?)`, code, textutil.Clean(l.Name), textutil.Clean(l.Unit), key, now())
	if err != nil {
		return 0, "", err
	}
	id, err = res.LastInsertId()
	return id, code, err
}

// SaveInvoice stores the invoice, books goods lines into stock and remembers user corrections.
func (s *Store) SaveInvoice(inv *model.Invoice) (int64, error) {
	inv.Number = strings.TrimLeft(strings.TrimSpace(inv.Number), "0")
	if inv.Number == "" || len(inv.Date) != 10 {
		return 0, errors.New("thiếu số hóa đơn hoặc ngày hóa đơn (YYYY-MM-DD)")
	}
	if _, err := time.Parse("2006-01-02", inv.Date); err != nil {
		return 0, errors.New("ngày hóa đơn không hợp lệ")
	}
	if len(inv.Lines) == 0 {
		return 0, errors.New("hóa đơn không có dòng nào")
	}
	if inv.ExRate <= 0 {
		inv.ExRate = 1
	}
	if inv.Currency == "" {
		inv.Currency = "VND"
	}
	if s.FindDuplicate(inv) != 0 {
		return 0, ErrDuplicate
	}
	allocateDiscount(inv)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	hasGoods := false
	for _, l := range inv.Lines {
		if !l.Kind.Valid() {
			return 0, fmt.Errorf("dòng %d: loại không hợp lệ", l.STT)
		}
		hasGoods = hasGoods || l.Kind == model.KindGoods
	}
	if hasGoods {
		if inv.ReceiptNo, err = nextDoc(tx, "PN"); err != nil {
			return 0, err
		}
	}
	inv.CreatedAt = now()
	res, err := tx.Exec(`INSERT INTO invoices(form,serial,number,date,seller_name,seller_tax,seller_addr,buyer_name,buyer_tax,buyer_addr,
		currency,ex_rate,pay_method,tax_auth_code,total_before_vat,total_vat,total,receipt_no,source_file,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inv.Form, inv.Serial, inv.Number, inv.Date, inv.Seller.Name, inv.Seller.TaxCode, inv.Seller.Address,
		inv.Buyer.Name, inv.Buyer.TaxCode, inv.Buyer.Address, inv.Currency, inv.ExRate, inv.PayMethod, inv.TaxAuthCode,
		inv.TotalBeforeVAT, inv.TotalVAT, inv.Total, inv.ReceiptNo, inv.SourceFile, inv.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrDuplicate
		}
		return 0, err
	}
	inv.ID, _ = res.LastInsertId()
	for i := range inv.Lines {
		l := &inv.Lines[i]
		var itemID any
		if l.Kind == model.KindGoods {
			if l.Qty <= 0 {
				return 0, fmt.Errorf("dòng %d (%s): hàng hóa nhập kho phải có số lượng > 0", l.STT, l.Name)
			}
			id, code, err := itemFor(tx, *l)
			if err != nil {
				return 0, err
			}
			l.ItemID, l.ItemCode, itemID = id, code, id
			note := fmt.Sprintf("Nhập kho theo HĐ %s số %s của %s", inv.SerialFull(), inv.Number, inv.Seller.Name)
			if _, err := tx.Exec(`INSERT INTO movements(item_id,date,dir,qty,amount,doc_no,invoice_id,note,created_at) VALUES(?,?,'IN',?,?,?,?,?,?)`,
				id, inv.Date, l.Qty, l.Cost, inv.ReceiptNo, inv.ID, note, inv.CreatedAt); err != nil {
				return 0, err
			}
		}
		if _, err := tx.Exec(`INSERT INTO invoice_lines(invoice_id,stt,nature,code,name,unit,qty,price,amount,vat_rate,vat_amount,kind,reason,item_id,cost)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, inv.ID, l.STT, l.Nature, l.Code, textutil.Clean(l.Name), textutil.Clean(l.Unit),
			l.Qty, l.Price, l.Amount, l.VATRate, l.VATAmount, string(l.Kind), l.Reason, itemID, l.Cost); err != nil {
			return 0, err
		}
		if l.UserSet && (l.Kind == model.KindGoods || l.Kind == model.KindService) {
			if _, err := tx.Exec(`INSERT INTO kind_rules(key,kind,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET kind=excluded.kind, updated_at=excluded.updated_at`,
				textutil.ItemKey(l.Name, l.Unit), string(l.Kind), now()); err != nil {
				return 0, err
			}
		}
	}
	return inv.ID, tx.Commit()
}

// allocateDiscount sets each goods line's stock cost in VND, spreading trade discounts by amount.
func allocateDiscount(inv *model.Invoice) {
	var disc, base float64
	for _, l := range inv.Lines {
		switch l.Kind {
		case model.KindDiscount:
			disc += abs(l.Amount)
		case model.KindGoods, model.KindService:
			base += l.Amount
		}
	}
	for i := range inv.Lines {
		l := &inv.Lines[i]
		if l.Kind != model.KindGoods {
			l.Cost = 0
			continue
		}
		c := l.Amount
		if base > 0 {
			c -= disc * l.Amount / base
		}
		l.Cost = textutil.Round(c*inv.ExRate, 0)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

const invCols = `id,form,serial,number,date,seller_name,seller_tax,seller_addr,buyer_name,buyer_tax,buyer_addr,
	currency,ex_rate,pay_method,tax_auth_code,total_before_vat,total_vat,total,receipt_no,source_file,created_at`

func scanInvoice(sc interface{ Scan(...any) error }) (*model.Invoice, error) {
	var v model.Invoice
	err := sc.Scan(&v.ID, &v.Form, &v.Serial, &v.Number, &v.Date, &v.Seller.Name, &v.Seller.TaxCode, &v.Seller.Address,
		&v.Buyer.Name, &v.Buyer.TaxCode, &v.Buyer.Address, &v.Currency, &v.ExRate, &v.PayMethod, &v.TaxAuthCode,
		&v.TotalBeforeVAT, &v.TotalVAT, &v.Total, &v.ReceiptNo, &v.SourceFile, &v.CreatedAt)
	return &v, err
}

// Invoices lists invoices dated within [from, to] (empty bounds are open), with lines.
func (s *Store) Invoices(from, to string) ([]*model.Invoice, error) {
	if to == "" {
		to = "9999-12-31"
	}
	rows, err := s.db.Query(`SELECT `+invCols+` FROM invoices WHERE date>=? AND date<=? ORDER BY date, id`, from, to)
	if err != nil {
		return nil, err
	}
	var out []*model.Invoice
	for rows.Next() {
		v, err := scanInvoice(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, v := range out {
		if v.Lines, err = s.lines(v.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Invoice returns one invoice with its lines.
func (s *Store) Invoice(id int64) (*model.Invoice, error) {
	v, err := scanInvoice(s.db.QueryRow(`SELECT `+invCols+` FROM invoices WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	v.Lines, err = s.lines(id)
	return v, err
}

func (s *Store) lines(id int64) ([]model.Line, error) {
	rows, err := s.db.Query(`SELECT l.stt,l.nature,l.code,l.name,l.unit,l.qty,l.price,l.amount,l.vat_rate,l.vat_amount,l.kind,l.reason,
		COALESCE(l.item_id,0),COALESCE(i.code,''),l.cost FROM invoice_lines l LEFT JOIN items i ON i.id=l.item_id WHERE l.invoice_id=? ORDER BY l.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Line
	for rows.Next() {
		var l model.Line
		var kind string
		if err := rows.Scan(&l.STT, &l.Nature, &l.Code, &l.Name, &l.Unit, &l.Qty, &l.Price, &l.Amount, &l.VATRate, &l.VATAmount,
			&kind, &l.Reason, &l.ItemID, &l.ItemCode, &l.Cost); err != nil {
			return nil, err
		}
		l.Kind = model.Kind(kind)
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteInvoice removes an invoice and its stock receipts, unless that would make stock negative.
func (s *Store) DeleteInvoice(id int64) error {
	movs, err := s.movements()
	if err != nil {
		return err
	}
	var keep []Movement
	for _, m := range movs {
		if m.InvoiceID != id {
			keep = append(keep, m)
		}
	}
	if item, date := firstNegative(keep); item != "" {
		return fmt.Errorf("không thể xóa: mặt hàng %s sẽ bị âm kho từ ngày %s (hãy xóa phiếu xuất trước)", item, date)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM movements WHERE invoice_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM invoice_lines WHERE invoice_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM invoices WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
