package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

// Movement is one stock receipt (IN) or issue (OUT).
type Movement struct {
	ID        int64   `json:"id"`
	ItemID    int64   `json:"itemId"`
	ItemCode  string  `json:"itemCode"`
	ItemName  string  `json:"itemName"`
	Unit      string  `json:"unit"`
	Date      string  `json:"date"`
	Dir       string  `json:"dir"`
	Qty       float64 `json:"qty"`
	Amount    float64 `json:"amount"` // OUT: tính theo bình quân gia quyền khi đọc ra
	DocNo     string  `json:"docNo"`
	InvoiceID int64   `json:"invoiceId"`
	Note      string  `json:"note"`
	BalQty    float64 `json:"balQty"`
	BalValue  float64 `json:"balValue"`
}

// StockRow is one line of the nhập-xuất-tồn report.
type StockRow struct {
	ItemID   int64   `json:"itemId"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Unit     string  `json:"unit"`
	OpenQty  float64 `json:"openQty"`
	OpenVal  float64 `json:"openVal"`
	InQty    float64 `json:"inQty"`
	InVal    float64 `json:"inVal"`
	OutQty   float64 `json:"outQty"`
	OutVal   float64 `json:"outVal"`
	CloseQty float64 `json:"closeQty"`
	CloseVal float64 `json:"closeVal"`
	AvgCost  float64 `json:"avgCost"`
}

const eps = 1e-9

// movements loads all movements in booking order and values OUTs at moving average cost.
func (s *Store) movements() ([]Movement, error) {
	rows, err := s.db.Query(`SELECT m.id,m.item_id,i.code,i.name,i.unit,m.date,m.dir,m.qty,m.amount,m.doc_no,COALESCE(m.invoice_id,0),m.note
		FROM movements m JOIN items i ON i.id=m.item_id ORDER BY m.date, CASE m.dir WHEN 'IN' THEN 0 ELSE 1 END, m.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movement
	for rows.Next() {
		var m Movement
		if err := rows.Scan(&m.ID, &m.ItemID, &m.ItemCode, &m.ItemName, &m.Unit, &m.Date, &m.Dir, &m.Qty, &m.Amount, &m.DocNo, &m.InvoiceID, &m.Note); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	value(out)
	return out, nil
}

// value runs the perpetual weighted average and fills OUT amounts and balances.
func value(ms []Movement) {
	type bal struct{ q, v float64 }
	b := map[int64]*bal{}
	for i := range ms {
		m := &ms[i]
		x := b[m.ItemID]
		if x == nil {
			x = &bal{}
			b[m.ItemID] = x
		}
		if m.Dir == "IN" {
			x.q += m.Qty
			x.v += m.Amount
		} else {
			switch {
			case x.q-m.Qty <= eps:
				m.Amount = x.v // xuất hết: lấy toàn bộ giá trị còn lại
			case x.q > eps:
				m.Amount = textutil.Round(x.v/x.q*m.Qty, 0)
			default:
				m.Amount = 0
			}
			x.q -= m.Qty
			x.v -= m.Amount
		}
		m.BalQty, m.BalValue = textutil.Round(x.q, 6), textutil.Round(x.v, 0)
	}
}

// firstNegative returns the first item name and date where stock would go below zero.
func firstNegative(ms []Movement) (string, string) {
	q := map[int64]float64{}
	for _, m := range ms {
		if m.Dir == "IN" {
			q[m.ItemID] += m.Qty
		} else {
			q[m.ItemID] -= m.Qty
		}
		if q[m.ItemID] < -1e-6 {
			return m.ItemName, m.Date
		}
	}
	return "", ""
}

// Summary returns the nhập-xuất-tồn table for [from, to]; empty bounds are open.
func (s *Store) Summary(from, to string) ([]StockRow, error) {
	ms, err := s.movements()
	if err != nil {
		return nil, err
	}
	rows := map[int64]*StockRow{}
	if err := s.eachItem(func(r StockRow) { rr := r; rows[r.ItemID] = &rr }); err != nil {
		return nil, err
	}
	for _, m := range ms {
		r := rows[m.ItemID]
		if r == nil || (to != "" && m.Date > to) {
			continue
		}
		sign := 1.0
		if m.Dir == "OUT" {
			sign = -1
		}
		if from != "" && m.Date < from {
			r.OpenQty += sign * m.Qty
			r.OpenVal += sign * m.Amount
		} else if m.Dir == "IN" {
			r.InQty += m.Qty
			r.InVal += m.Amount
		} else {
			r.OutQty += m.Qty
			r.OutVal += m.Amount
		}
	}
	var out []StockRow
	for _, r := range rows {
		r.CloseQty = textutil.Round(r.OpenQty+r.InQty-r.OutQty, 6)
		r.CloseVal = textutil.Round(r.OpenVal+r.InVal-r.OutVal, 0)
		if r.CloseQty > eps {
			r.AvgCost = textutil.Round(r.CloseVal/r.CloseQty, 2)
		}
		if r.OpenQty == 0 && r.InQty == 0 && r.OutQty == 0 && r.CloseQty == 0 {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

func (s *Store) eachItem(fn func(StockRow)) error {
	rows, err := s.db.Query(`SELECT id, code, name, unit FROM items`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r StockRow
		if err := rows.Scan(&r.ItemID, &r.Code, &r.Name, &r.Unit); err != nil {
			return err
		}
		fn(r)
	}
	return rows.Err()
}

// Ledger returns the stock card (thẻ kho) of one item, or of all items when itemID is 0.
func (s *Store) Ledger(itemID int64, from, to string) ([]Movement, error) {
	ms, err := s.movements()
	if err != nil {
		return nil, err
	}
	var out []Movement
	for _, m := range ms {
		if (itemID == 0 || m.ItemID == itemID) && (from == "" || m.Date >= from) && (to == "" || m.Date <= to) {
			out = append(out, m)
		}
	}
	return out, nil
}

// OpeningBalance returns qty and value of an item before date from.
func (s *Store) OpeningBalance(itemID int64, from string) (float64, float64, error) {
	ms, err := s.movements()
	if err != nil {
		return 0, 0, err
	}
	var q, v float64
	for _, m := range ms {
		if m.ItemID == itemID && from != "" && m.Date < from {
			q, v = m.BalQty, m.BalValue
		}
	}
	return q, v, nil
}

// StockOut records an issue from stock; it fails if stock would go negative at any date.
func (s *Store) StockOut(itemID int64, date string, qty float64, note string) (string, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return "", errors.New("ngày xuất không hợp lệ")
	}
	if qty <= 0 {
		return "", errors.New("số lượng xuất phải > 0")
	}
	ms, err := s.movements()
	if err != nil {
		return "", err
	}
	var name string
	if err := s.db.QueryRow(`SELECT name FROM items WHERE id=?`, itemID).Scan(&name); err != nil {
		return "", errors.New("không tìm thấy mặt hàng")
	}
	trial := append([]Movement(nil), ms...)
	trial = append(trial, Movement{ItemID: itemID, ItemName: name, Date: date, Dir: "OUT", Qty: qty})
	sort.SliceStable(trial, func(i, j int) bool {
		if trial[i].Date != trial[j].Date {
			return trial[i].Date < trial[j].Date
		}
		return trial[i].Dir == "IN" && trial[j].Dir == "OUT"
	})
	if item, d := firstNegative(trial); item != "" {
		return "", fmt.Errorf("không đủ hàng: %s sẽ bị âm kho từ ngày %s", item, d)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	doc, err := nextDoc(tx, "PX")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO movements(item_id,date,dir,qty,amount,doc_no,invoice_id,note,created_at) VALUES(?,?,'OUT',?,0,?,NULL,?,?)`,
		itemID, date, qty, doc, strings.TrimSpace(note), now()); err != nil {
		return "", err
	}
	return doc, tx.Commit()
}

// DeleteStockOut removes a manual issue.
func (s *Store) DeleteStockOut(id int64) error {
	res, err := s.db.Exec(`DELETE FROM movements WHERE id=? AND dir='OUT' AND invoice_id IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("chỉ xóa được phiếu xuất kho nhập tay")
	}
	return nil
}
