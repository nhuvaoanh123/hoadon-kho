// Package export writes Excel workbooks in the layouts Vietnamese accountants use
// (phiếu nhập kho 01-VT, bảng kê mua vào, nhập-xuất-tồn, thẻ kho, file nhập liệu).
package export

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/store"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

const font = "Times New Roman"

type styles struct {
	title, sub, bold, text, right, th, td, tdC, num, qty, totL, totN, totQ, sign int
}

type book struct {
	f  *excelize.File
	st styles
}

func newBook() *book {
	f := excelize.NewFile()
	b := &book{f: f}
	border := []excelize.Border{{Type: "left", Color: "000000", Style: 1}, {Type: "right", Color: "000000", Style: 1},
		{Type: "top", Color: "000000", Style: 1}, {Type: "bottom", Color: "000000", Style: 1}}
	nf0, nf2 := "#,##0", "#,##0.00"
	mk := func(size float64, bold, italic bool, h string, bd bool, fill bool, nf *string) int {
		s := &excelize.Style{
			Font:      &excelize.Font{Family: font, Size: size, Bold: bold, Italic: italic},
			Alignment: &excelize.Alignment{Horizontal: h, Vertical: "center", WrapText: true},
		}
		if bd {
			s.Border = border
		}
		if fill {
			s.Fill = excelize.Fill{Type: "pattern", Color: []string{"DDEBF7"}, Pattern: 1}
		}
		s.CustomNumFmt = nf
		id, _ := f.NewStyle(s)
		return id
	}
	b.st = styles{
		title: mk(14, true, false, "center", false, false, nil),
		sub:   mk(11, false, true, "center", false, false, nil),
		bold:  mk(11, true, false, "left", false, false, nil),
		text:  mk(11, false, false, "left", false, false, nil),
		right: mk(11, false, true, "right", false, false, nil),
		th:    mk(11, true, false, "center", true, true, nil),
		td:    mk(11, false, false, "left", true, false, nil),
		tdC:   mk(11, false, false, "center", true, false, nil),
		num:   mk(11, false, false, "right", true, false, &nf0),
		qty:   mk(11, false, false, "right", true, false, &nf2),
		totL:  mk(11, true, false, "center", true, false, nil),
		totN:  mk(11, true, false, "right", true, false, &nf0),
		totQ:  mk(11, true, false, "right", true, false, &nf2),
		sign:  mk(11, true, false, "center", false, false, nil),
	}
	return b
}

func cell(c, r int) string { s, _ := excelize.CoordinatesToCellName(c, r); return s }

func (b *book) sheet(name string, first bool) string {
	if first {
		_ = b.f.SetSheetName("Sheet1", name)
	} else {
		_, _ = b.f.NewSheet(name)
	}
	return name
}

func (b *book) set(sh string, c, r int, v any, style int) {
	_ = b.f.SetCellValue(sh, cell(c, r), v)
	_ = b.f.SetCellStyle(sh, cell(c, r), cell(c, r), style)
}

func (b *book) merge(sh string, c1, r1, c2, r2 int, v any, style int) {
	if c1 != c2 || r1 != r2 {
		_ = b.f.MergeCell(sh, cell(c1, r1), cell(c2, r2))
	}
	_ = b.f.SetCellValue(sh, cell(c1, r1), v)
	_ = b.f.SetCellStyle(sh, cell(c1, r1), cell(c2, r2), style)
}

func (b *book) widths(sh string, w ...float64) {
	for i, x := range w {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = b.f.SetColWidth(sh, col, col, x)
	}
}

// header writes the company block and returns the next free row.
func (b *book) header(sh string, set map[string]string, lastCol int, title, sub string) int {
	b.merge(sh, 1, 1, lastCol, 1, "Đơn vị: "+set["company_name"], b.st.bold)
	b.merge(sh, 1, 2, lastCol, 2, "Địa chỉ: "+set["company_address"], b.st.text)
	b.merge(sh, 1, 3, lastCol, 3, "Mã số thuế: "+set["company_tax"], b.st.text)
	b.merge(sh, 1, 5, lastCol, 5, title, b.st.title)
	b.merge(sh, 1, 6, lastCol, 6, sub, b.st.sub)
	return 8
}

func period(from, to string) string {
	return "Từ ngày " + dmy(from, "đầu kỳ") + " đến ngày " + dmy(to, "nay")
}

func dmy(d, empty string) string {
	if len(d) != 10 {
		return empty
	}
	return d[8:10] + "/" + d[5:7] + "/" + d[0:4]
}

func vnd(inv *model.Invoice, v float64) float64 { return textutil.Round(v*inv.ExRate, 0) }

func signOf(l model.Line) float64 {
	if l.Kind == model.KindDiscount {
		return -1
	}
	return 1
}

// PurchaseList is the "Bảng kê hóa đơn, chứng từ hàng hóa, dịch vụ mua vào", grouped by VAT rate.
func PurchaseList(set map[string]string, invs []*model.Invoice, from, to string) *excelize.File {
	b := newBook()
	sh := b.sheet("Bang ke mua vao", true)
	b.widths(sh, 6, 11, 10, 12, 32, 15, 36, 17, 9, 15, 14)
	r := b.header(sh, set, 11, "BẢNG KÊ HÓA ĐƠN, CHỨNG TỪ HÀNG HÓA, DỊCH VỤ MUA VÀO", period(from, to))
	r = b.headRow(sh, r, "STT", "Ký hiệu hóa đơn", "Số hóa đơn", "Ngày lập hóa đơn", "Tên người bán", "Mã số thuế người bán",
		"Mặt hàng", "Giá trị HHDV mua vào chưa có thuế", "Thuế suất", "Thuế GTGT", "Ghi chú")

	type row struct {
		inv         *model.Invoice
		names       []string
		amount, vat float64
	}
	groups := map[string][]*row{}
	for _, inv := range invs {
		byRate := map[string]*row{}
		var order []string
		for _, l := range inv.Lines {
			if l.Kind == model.KindNote {
				continue
			}
			g := byRate[l.VATRate]
			if g == nil {
				g = &row{inv: inv}
				byRate[l.VATRate] = g
				order = append(order, l.VATRate)
			}
			if l.Kind != model.KindDiscount {
				g.names = append(g.names, l.Name)
			}
			g.amount += signOf(l) * vnd(inv, l.Amount)
			g.vat += signOf(l) * vnd(inv, l.VATAmount)
		}
		for _, k := range order {
			groups[k] = append(groups[k], byRate[k])
		}
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return rateOrder(keys[i]) < rateOrder(keys[j]) })

	var ta, tv float64
	for gi, k := range keys {
		b.merge(sh, 1, r, 11, r, fmt.Sprintf("%d. %s", gi+1, rateLabel(k)), b.st.totL)
		r++
		var ga, gv float64
		for i, x := range groups[k] {
			b.set(sh, 1, r, i+1, b.st.tdC)
			b.set(sh, 2, r, x.inv.SerialFull(), b.st.tdC)
			b.set(sh, 3, r, x.inv.Number, b.st.tdC)
			b.set(sh, 4, r, dmy(x.inv.Date, ""), b.st.tdC)
			b.set(sh, 5, r, x.inv.Seller.Name, b.st.td)
			b.set(sh, 6, r, x.inv.Seller.TaxCode, b.st.tdC)
			b.set(sh, 7, r, summarize(x.names), b.st.td)
			b.set(sh, 8, r, x.amount, b.st.num)
			b.set(sh, 9, r, k, b.st.tdC)
			b.set(sh, 10, r, x.vat, b.st.num)
			b.set(sh, 11, r, "", b.st.td)
			ga, gv = ga+x.amount, gv+x.vat
			r++
		}
		b.merge(sh, 1, r, 7, r, "Tổng", b.st.totL)
		b.set(sh, 8, r, ga, b.st.totN)
		b.set(sh, 9, r, "", b.st.td)
		b.set(sh, 10, r, gv, b.st.totN)
		b.set(sh, 11, r, "", b.st.td)
		r++
		ta, tv = ta+ga, tv+gv
	}
	b.merge(sh, 1, r, 7, r, "TỔNG CỘNG", b.st.totL)
	b.set(sh, 8, r, ta, b.st.totN)
	b.set(sh, 9, r, "", b.st.td)
	b.set(sh, 10, r, tv, b.st.totN)
	b.set(sh, 11, r, "", b.st.td)
	b.signatures(sh, r+2, 11, "Người lập biểu", "Kế toán trưởng", "Giám đốc")
	return b.f
}

func rateOrder(k string) int {
	switch k {
	case "0%":
		return 0
	case "5%":
		return 1
	case "8%":
		return 2
	case "10%":
		return 3
	case "KCT":
		return 5
	case "KKKNT":
		return 6
	case "":
		return 8
	}
	return 7
}

func rateLabel(k string) string {
	switch k {
	case "KCT":
		return "Hàng hóa, dịch vụ không chịu thuế GTGT"
	case "KKKNT":
		return "Hàng hóa, dịch vụ không phải kê khai, tính nộp thuế GTGT"
	case "":
		return "Hóa đơn không ghi thuế suất (hóa đơn bán hàng)"
	}
	return "Hàng hóa, dịch vụ chịu thuế suất " + k
}

func summarize(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, "; ")
	}
	return strings.Join(names[:3], "; ") + fmt.Sprintf("; ... (%d mặt hàng)", len(names))
}

func (b *book) headRow(sh string, r int, titles ...string) int {
	for i, t := range titles {
		b.set(sh, i+1, r, t, b.st.th)
	}
	_ = b.f.SetRowHeight(sh, r, 45)
	return r + 1
}

func (b *book) signatures(sh string, r, lastCol int, roles ...string) {
	b.merge(sh, lastCol-3, r, lastCol, r, "Ngày ..... tháng ..... năm ..........", b.st.right)
	span := lastCol / len(roles)
	for i, role := range roles {
		c1 := 1 + i*span
		c2 := c1 + span - 1
		if i == len(roles)-1 {
			c2 = lastCol
		}
		b.merge(sh, c1, r+1, c2, r+1, role, b.st.sign)
		b.merge(sh, c1, r+2, c2, r+2, "(Ký, họ tên)", b.st.sub)
	}
}

// Receipts writes one "Phiếu nhập kho" (mẫu 01-VT) sheet per invoice that has goods.
func Receipts(set map[string]string, invs []*model.Invoice) *excelize.File {
	b := newBook()
	first := true
	for _, inv := range invs {
		if inv.ReceiptNo == "" {
			continue
		}
		b.receipt(b.sheet(inv.ReceiptNo, first), set, inv)
		first = false
	}
	if first {
		sh := b.sheet("Phieu nhap kho", true)
		b.set(sh, 1, 1, "Không có phiếu nhập kho nào trong kỳ", b.st.text)
	}
	return b.f
}

func (b *book) receipt(sh string, set map[string]string, inv *model.Invoice) {
	b.widths(sh, 6, 40, 12, 9, 12, 12, 15, 17)
	b.merge(sh, 1, 1, 4, 1, "Đơn vị: "+set["company_name"], b.st.bold)
	b.merge(sh, 1, 2, 4, 2, "Địa chỉ: "+set["company_address"], b.st.text)
	b.merge(sh, 6, 1, 8, 1, "Mẫu số 01 - VT", b.st.sign)
	b.merge(sh, 5, 2, 8, 3, set["form_basis"], b.st.sub)
	_ = b.f.SetRowHeight(sh, 3, 22)
	b.merge(sh, 1, 5, 8, 5, "PHIẾU NHẬP KHO", b.st.title)
	d := inv.Date
	b.merge(sh, 2, 6, 6, 6, fmt.Sprintf("Ngày %s tháng %s năm %s", d[8:10], d[5:7], d[0:4]), b.st.sub)
	b.merge(sh, 2, 7, 6, 7, "Số: "+inv.ReceiptNo, b.st.sub)
	b.merge(sh, 7, 6, 8, 6, "Nợ: "+set["acct_goods"], b.st.text)
	b.merge(sh, 7, 7, 8, 7, "Có: "+set["acct_payable"], b.st.text)
	b.merge(sh, 1, 8, 8, 8, "- Họ và tên người giao: "+inv.Seller.Name, b.st.text)
	b.merge(sh, 1, 9, 8, 9, fmt.Sprintf("- Theo hóa đơn GTGT ký hiệu %s số %s ngày %s của %s", inv.SerialFull(), inv.Number, dmy(inv.Date, ""), inv.Seller.Name), b.st.text)
	b.merge(sh, 1, 10, 8, 10, "- Nhập tại kho: "+set["warehouse"]+"          Địa điểm: "+set["company_address"], b.st.text)

	r := 12
	b.merge(sh, 1, r, 1, r+1, "STT", b.st.th)
	b.merge(sh, 2, r, 2, r+1, "Tên, nhãn hiệu, quy cách, phẩm chất vật tư, dụng cụ, sản phẩm, hàng hóa", b.st.th)
	b.merge(sh, 3, r, 3, r+1, "Mã số", b.st.th)
	b.merge(sh, 4, r, 4, r+1, "Đơn vị tính", b.st.th)
	b.merge(sh, 5, r, 6, r, "Số lượng", b.st.th)
	b.set(sh, 5, r+1, "Theo chứng từ", b.st.th)
	b.set(sh, 6, r+1, "Thực nhập", b.st.th)
	b.merge(sh, 7, r, 7, r+1, "Đơn giá", b.st.th)
	b.merge(sh, 8, r, 8, r+1, "Thành tiền", b.st.th)
	_ = b.f.SetRowHeight(sh, r, 30)
	_ = b.f.SetRowHeight(sh, r+1, 30)
	r += 2
	for i, s := range []string{"A", "B", "C", "D", "1", "2", "3", "4"} {
		b.set(sh, i+1, r, s, b.st.th)
	}
	r++
	var total float64
	n := 0
	for _, l := range inv.Lines {
		if l.Kind != model.KindGoods {
			continue
		}
		n++
		b.set(sh, 1, r, n, b.st.tdC)
		b.set(sh, 2, r, l.Name, b.st.td)
		b.set(sh, 3, r, l.ItemCode, b.st.tdC)
		b.set(sh, 4, r, l.Unit, b.st.tdC)
		b.set(sh, 5, r, l.Qty, b.st.qty)
		b.set(sh, 6, r, l.Qty, b.st.qty)
		b.set(sh, 7, r, textutil.Round(l.Cost/l.Qty, 2), b.st.qty)
		b.set(sh, 8, r, l.Cost, b.st.num)
		total += l.Cost
		r++
	}
	b.merge(sh, 1, r, 4, r, "Cộng", b.st.totL)
	for c := 5; c <= 7; c++ {
		b.set(sh, c, r, "x", b.st.tdC)
	}
	b.set(sh, 8, r, total, b.st.totN)
	r += 2
	b.merge(sh, 1, r, 8, r, "- Tổng số tiền (Viết bằng chữ): "+textutil.MoneyWords(total), b.st.text)
	b.merge(sh, 1, r+1, 8, r+1, "- Số chứng từ gốc kèm theo: 01 hóa đơn GTGT", b.st.text)
	b.signatures(sh, r+3, 8, "Người lập phiếu", "Người giao hàng", "Thủ kho", "Kế toán trưởng")
}

// StockSummary is the "Bảng tổng hợp nhập - xuất - tồn" valued at weighted average cost.
func StockSummary(set map[string]string, rows []store.StockRow, from, to string) *excelize.File {
	b := newBook()
	sh := b.sheet("Nhap xuat ton", true)
	b.widths(sh, 6, 12, 36, 8, 11, 15, 11, 15, 11, 15, 11, 15)
	r := b.header(sh, set, 12, "BẢNG TỔNG HỢP NHẬP - XUẤT - TỒN KHO HÀNG HÓA", period(from, to)+" — Kho: "+set["warehouse"])
	for i, t := range []string{"STT", "Mã hàng", "Tên hàng hóa", "ĐVT"} {
		b.merge(sh, i+1, r, i+1, r+1, t, b.st.th)
	}
	for i, t := range []string{"Tồn đầu kỳ", "Nhập trong kỳ", "Xuất trong kỳ", "Tồn cuối kỳ"} {
		c := 5 + i*2
		b.merge(sh, c, r, c+1, r, t, b.st.th)
		b.set(sh, c, r+1, "Số lượng", b.st.th)
		b.set(sh, c+1, r+1, "Thành tiền", b.st.th)
	}
	r += 2
	var tot [4]float64
	for i, x := range rows {
		b.set(sh, 1, r, i+1, b.st.tdC)
		b.set(sh, 2, r, x.Code, b.st.tdC)
		b.set(sh, 3, r, x.Name, b.st.td)
		b.set(sh, 4, r, x.Unit, b.st.tdC)
		vals := []float64{x.OpenQty, x.OpenVal, x.InQty, x.InVal, x.OutQty, x.OutVal, x.CloseQty, x.CloseVal}
		for j, v := range vals {
			st := b.st.qty
			if j%2 == 1 {
				st = b.st.num
				tot[j/2] += v
			}
			b.set(sh, 5+j, r, v, st)
		}
		r++
	}
	b.merge(sh, 1, r, 4, r, "Tổng cộng", b.st.totL)
	for j := 0; j < 4; j++ {
		b.set(sh, 5+j*2, r, "", b.st.td)
		b.set(sh, 6+j*2, r, tot[j], b.st.totN)
	}
	_ = b.f.SetPanes(sh, &excelize.Panes{Freeze: true, YSplit: r - len(rows) - 1, TopLeftCell: cell(1, r-len(rows)), ActivePane: "bottomLeft"})
	b.signatures(sh, r+2, 12, "Người lập biểu", "Thủ kho", "Kế toán trưởng")
	return b.f
}

// Card is the data of one stock card (thẻ kho).
type Card struct {
	Item    store.StockRow
	Moves   []store.Movement
	OpenQty float64
}

// StockCards writes one "Thẻ kho" sheet per item (quantities only, as the form requires).
func StockCards(set map[string]string, cards []Card, from, to string) *excelize.File {
	b := newBook()
	if len(cards) == 0 {
		sh := b.sheet("The kho", true)
		b.set(sh, 1, 1, "Không có dữ liệu kho trong kỳ", b.st.text)
		return b.f
	}
	for i, c := range cards {
		sh := b.sheet(c.Item.Code, i == 0)
		b.widths(sh, 6, 11, 11, 11, 38, 11, 11, 11, 11, 14)
		b.merge(sh, 1, 1, 5, 1, "Đơn vị: "+set["company_name"], b.st.bold)
		b.merge(sh, 1, 2, 5, 2, "Địa chỉ: "+set["company_address"], b.st.text)
		b.merge(sh, 1, 4, 10, 4, "THẺ KHO (SỔ KHO)", b.st.title)
		b.merge(sh, 1, 5, 10, 5, period(from, to)+" — Kho: "+set["warehouse"], b.st.sub)
		b.merge(sh, 1, 6, 10, 6, "- Tên, nhãn hiệu, quy cách vật tư, dụng cụ, sản phẩm, hàng hóa: "+c.Item.Name, b.st.text)
		b.merge(sh, 1, 7, 10, 7, "- Đơn vị tính: "+c.Item.Unit+"          - Mã số: "+c.Item.Code, b.st.text)
		r := 9
		b.merge(sh, 1, r, 1, r+1, "STT", b.st.th)
		b.merge(sh, 2, r, 2, r+1, "Ngày, tháng", b.st.th)
		b.merge(sh, 3, r, 4, r, "Số hiệu chứng từ", b.st.th)
		b.set(sh, 3, r+1, "Nhập", b.st.th)
		b.set(sh, 4, r+1, "Xuất", b.st.th)
		b.merge(sh, 5, r, 5, r+1, "Diễn giải", b.st.th)
		b.merge(sh, 6, r, 6, r+1, "Ngày nhập, xuất", b.st.th)
		b.merge(sh, 7, r, 9, r, "Số lượng", b.st.th)
		b.set(sh, 7, r+1, "Nhập", b.st.th)
		b.set(sh, 8, r+1, "Xuất", b.st.th)
		b.set(sh, 9, r+1, "Tồn", b.st.th)
		b.merge(sh, 10, r, 10, r+1, "Ký xác nhận của kế toán", b.st.th)
		r += 2
		b.merge(sh, 1, r, 8, r, "Số tồn đầu kỳ", b.st.totL)
		b.set(sh, 9, r, c.OpenQty, b.st.totQ)
		b.set(sh, 10, r, "", b.st.td)
		r++
		var in, out float64
		for j, m := range c.Moves {
			b.set(sh, 1, r, j+1, b.st.tdC)
			b.set(sh, 2, r, dmy(m.Date, ""), b.st.tdC)
			b.set(sh, 6, r, dmy(m.Date, ""), b.st.tdC)
			b.set(sh, 5, r, m.Note, b.st.td)
			if m.Dir == "IN" {
				b.set(sh, 3, r, m.DocNo, b.st.tdC)
				b.set(sh, 4, r, "", b.st.td)
				b.set(sh, 7, r, m.Qty, b.st.qty)
				b.set(sh, 8, r, "", b.st.td)
				in += m.Qty
			} else {
				b.set(sh, 3, r, "", b.st.td)
				b.set(sh, 4, r, m.DocNo, b.st.tdC)
				b.set(sh, 7, r, "", b.st.td)
				b.set(sh, 8, r, m.Qty, b.st.qty)
				out += m.Qty
			}
			b.set(sh, 9, r, m.BalQty, b.st.qty)
			b.set(sh, 10, r, "", b.st.td)
			r++
		}
		b.merge(sh, 1, r, 6, r, "Cộng cuối kỳ", b.st.totL)
		b.set(sh, 7, r, in, b.st.totQ)
		b.set(sh, 8, r, out, b.st.totQ)
		b.set(sh, 9, r, textutil.Round(c.OpenQty+in-out, 6), b.st.totQ)
		b.set(sh, 10, r, "", b.st.td)
		b.signatures(sh, r+2, 10, "Thủ kho", "Kế toán trưởng", "Giám đốc")
	}
	return b.f
}

// Services lists purchased services (chi phí, không nhập kho).
func Services(set map[string]string, invs []*model.Invoice, from, to string) *excelize.File {
	b := newBook()
	sh := b.sheet("Chi phi dich vu", true)
	b.widths(sh, 6, 11, 10, 10, 30, 14, 34, 8, 9, 14, 15, 9, 14, 15, 9)
	r := b.header(sh, set, 15, "BẢNG KÊ CHI PHÍ DỊCH VỤ MUA VÀO", period(from, to))
	r = b.headRow(sh, r, "STT", "Ngày hóa đơn", "Ký hiệu", "Số hóa đơn", "Người bán", "MST người bán", "Nội dung dịch vụ",
		"ĐVT", "Số lượng", "Đơn giá", "Thành tiền", "Thuế suất", "Tiền thuế GTGT", "Cộng", "TK chi phí")
	var ta, tv float64
	n := 0
	for _, inv := range invs {
		for _, l := range inv.Lines {
			if l.Kind != model.KindService {
				continue
			}
			n++
			a, v := vnd(inv, l.Amount), vnd(inv, l.VATAmount)
			vals := []any{n, dmy(inv.Date, ""), inv.SerialFull(), inv.Number, inv.Seller.Name, inv.Seller.TaxCode, l.Name,
				l.Unit, l.Qty, vnd(inv, l.Price), a, l.VATRate, v, a + v, set["acct_service"]}
			sts := []int{b.st.tdC, b.st.tdC, b.st.tdC, b.st.tdC, b.st.td, b.st.tdC, b.st.td, b.st.tdC, b.st.qty, b.st.num, b.st.num, b.st.tdC, b.st.num, b.st.num, b.st.tdC}
			for c, x := range vals {
				b.set(sh, c+1, r, x, sts[c])
			}
			ta, tv = ta+a, tv+v
			r++
		}
	}
	b.merge(sh, 1, r, 10, r, "Tổng cộng", b.st.totL)
	b.set(sh, 11, r, ta, b.st.totN)
	b.set(sh, 12, r, "", b.st.td)
	b.set(sh, 13, r, tv, b.st.totN)
	b.set(sh, 14, r, ta+tv, b.st.totN)
	b.set(sh, 15, r, "", b.st.td)
	return b.f
}

// AccountingImport is a flat sheet (header on row 1) for the import wizard of MISA, Fast, etc.
func AccountingImport(set map[string]string, invs []*model.Invoice) *excelize.File {
	b := newBook()
	sh := b.sheet("Chung tu mua hang", true)
	cols := []string{"Ngày hạch toán", "Ngày chứng từ", "Số chứng từ", "Loại", "Mã nhà cung cấp", "Tên nhà cung cấp",
		"Địa chỉ", "Diễn giải", "Mã hàng", "Tên hàng", "Kho", "TK Nợ", "TK Có", "ĐVT", "Số lượng", "Đơn giá",
		"Thành tiền", "% thuế GTGT", "Tiền thuế GTGT", "TK thuế GTGT", "Mẫu số HĐ", "Ký hiệu HĐ", "Số hóa đơn", "Ngày hóa đơn", "Mã CQT"}
	b.widths(sh, 12, 12, 12, 10, 14, 32, 30, 36, 11, 34, 14, 8, 8, 8, 11, 14, 15, 9, 14, 9, 8, 10, 10, 12, 36)
	for i, c := range cols {
		b.set(sh, i+1, 1, c, b.st.th)
	}
	r := 2
	for _, inv := range invs {
		for _, l := range inv.Lines {
			if l.Kind == model.KindNote {
				continue
			}
			kind, debit, code, wh, doc := "Dịch vụ", set["acct_service"], "", "", ""
			amount := vnd(inv, l.Amount)
			switch l.Kind {
			case model.KindGoods:
				kind, debit, code, wh, doc = "Hàng hóa", set["acct_goods"], l.ItemCode, set["warehouse"], inv.ReceiptNo
				amount = l.Cost
			case model.KindDiscount:
				kind, debit = "Chiết khấu", set["acct_goods"]
				amount = -amount
			}
			vat := signOf(l) * vnd(inv, l.VATAmount)
			price := vnd(inv, l.Price)
			if l.Kind == model.KindGoods && l.Qty != 0 {
				price = textutil.Round(l.Cost/l.Qty, 2)
			}
			vals := []any{dmy(inv.Date, ""), dmy(inv.Date, ""), doc, kind, inv.Seller.TaxCode, inv.Seller.Name, inv.Seller.Address,
				fmt.Sprintf("Mua hàng theo HĐ %s số %s", inv.SerialFull(), inv.Number), code, l.Name, wh, debit, set["acct_payable"],
				l.Unit, l.Qty, price, amount, l.VATRate, vat, set["acct_vat"], inv.Form, inv.Serial, inv.Number, dmy(inv.Date, ""), inv.TaxAuthCode}
			for c, x := range vals {
				st := b.st.td
				switch c {
				case 14, 15:
					st = b.st.qty
				case 16, 18:
					st = b.st.num
				}
				b.set(sh, c+1, r, x, st)
			}
			r++
		}
	}
	_ = b.f.SetPanes(sh, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	return b.f
}
