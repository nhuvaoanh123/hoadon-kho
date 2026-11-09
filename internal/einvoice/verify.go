package einvoice

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

// Verify sets the issues of the invoice and its lines. Saving needs Block issues fixed
// and every other issue confirmed by the user.
func Verify(inv *model.Invoice) { VerifyKnown(inv, nil) }

// VerifyKnown is Verify where known(item key) reports goods already in stock: a wrapped
// name that rebuilds exactly such an item's name and unit is not flagged.
func VerifyKnown(inv *model.Invoice, known func(key string) bool) {
	inv.Issues = nil
	add := func(block bool, f string, a ...any) {
		inv.Issues = append(inv.Issues, model.Issue{Text: fmt.Sprintf(f, a...), Block: block})
	}
	if strings.TrimSpace(inv.Number) == "" {
		add(true, "Thiếu số hóa đơn")
	}
	if strings.TrimSpace(inv.Serial) == "" {
		add(true, "Thiếu ký hiệu hóa đơn")
	}
	if d, err := time.Parse("2006-01-02", inv.Date); err != nil {
		add(true, "Thiếu hoặc sai ngày hóa đơn")
	} else if d.After(time.Now()) {
		add(true, "Ngày hóa đơn %s ở tương lai: đọc sai ngày", inv.Date)
	}
	switch mst := strings.TrimSpace(inv.Seller.TaxCode); {
	case mst == "":
		add(true, "Thiếu mã số thuế người bán")
	case !textutil.ValidMST(mst):
		add(false, "MST người bán %s sai chữ số kiểm tra: có thể đọc nhầm, hãy đối chiếu", mst)
	}
	if inv.Source == model.SourceOCR || inv.Source == model.SourcePDFRaw {
		add(false, "Thông tin đầu hóa đơn (ký hiệu, số, ngày, người bán, MST) đọc bằng OCR: hãy đối chiếu với bản gốc")
	}
	if inv.Source == model.SourceText {
		add(false, "Chữ dán vào không phải file hóa đơn gốc: đối chiếu số, ngày, MST và các dòng với hóa đơn gốc")
	}

	var sumB, sumV float64
	for i := range inv.Lines {
		l := &inv.Lines[i]
		verifyLine(inv, l, known)
		switch l.Kind {
		case model.KindNote:
		case model.KindDiscount:
			sumB -= math.Abs(l.Amount)
			sumV -= math.Abs(l.VATAmount)
		default:
			sumB += l.Amount
			sumV += l.VATAmount
		}
	}
	st := inv.Stated
	tol := float64(len(inv.Lines)) + 1 // làm tròn thuế từng dòng
	switch {
	case st.Total == 0 && st.BeforeVAT == 0:
		if inv.Source != model.SourceXML {
			add(false, "Không đọc được tổng tiền in trên hóa đơn nên không đối chiếu được tổng các dòng: hãy kiểm tra đủ dòng")
		}
	default:
		if st.BeforeVAT != 0 && math.Abs(st.BeforeVAT-sumB) > 1 {
			add(false, "Tổng tiền hàng các dòng %s ≠ tổng in trên hóa đơn %s: có dòng sai hoặc thiếu", money(sumB), money(st.BeforeVAT))
		}
		if (st.VAT != 0 || st.BeforeVAT != 0) && math.Abs(st.VAT-sumV) > tol {
			add(false, "Tổng thuế các dòng %s ≠ tiền thuế in trên hóa đơn %s", money(sumV), money(st.VAT))
		}
		if st.Total != 0 && math.Abs(st.Total-(sumB+sumV)) > tol {
			add(false, "Tổng thanh toán các dòng %s ≠ tổng in trên hóa đơn %s: có dòng sai hoặc thiếu", money(sumB+sumV), money(st.Total))
		}
	}
	if st.Words != "" && inv.Currency == "VND" {
		total := st.Total
		if total == 0 {
			total = sumB + sumV
		}
		if !textutil.SameMoneyWords(st.Words, total) {
			add(false, "Số tiền bằng chữ trên hóa đơn không khớp tổng %s", money(total))
		}
	}
}

func verifyLine(inv *model.Invoice, l *model.Line, known func(string) bool) {
	l.Issues = nil
	add := func(block bool, f string, a ...any) {
		l.Issues = append(l.Issues, model.Issue{Text: fmt.Sprintf(f, a...), Block: block})
	}
	if l.Kind == model.KindNote {
		return
	}
	if strings.TrimSpace(l.Name) == "" {
		add(true, "Thiếu tên hàng hóa, dịch vụ")
	}
	if l.Kind == model.KindGoods {
		switch {
		case l.Qty <= 0:
			add(true, "Hàng hóa nhập kho phải có số lượng")
		case strings.TrimSpace(l.Unit) == "":
			add(false, "Thiếu đơn vị tính")
		}
	}
	if l.Qty > 0 && l.Price > 0 {
		if d := math.Abs(l.Qty*l.Price - l.Amount); d > math.Max(1, l.Qty*0.01) {
			add(false, "SL × đơn giá = %s nhưng thành tiền là %s", money(l.Qty*l.Price), money(l.Amount))
		}
	}
	if l.Kind != model.KindDiscount && l.Nature != 2 && l.Amount <= 0 {
		add(false, "Thành tiền bằng 0 (hàng tặng, bảo hành?): hàng vào kho với giá vốn 0")
	}
	if r := l.VATRate; r != "" && r != "KCT" && r != "KKKNT" {
		exp := textutil.Round(l.Amount*textutil.ParseRate(r), 0)
		if math.Abs(exp-l.VATAmount) > math.Max(1, exp*0.001) {
			add(false, "Tiền thuế %s ≠ thành tiền × %s = %s", money(l.VATAmount), r, money(exp))
		}
	}
	for _, n := range l.Notes {
		add(false, "Bộ đọc phải suy đoán: %s", n)
	}
	switch inv.Source {
	case model.SourceOCR, model.SourcePDFRaw:
		add(false, "Đọc bằng OCR hoặc từ PDF lỗi font: đối chiếu tên hàng, ĐVT, số lượng, đơn giá với bản gốc")
	case model.SourcePDF, model.SourceHTML, model.SourceText:
		if l.Wrapped && (known == nil || !known(textutil.ItemKey(l.Name, l.Unit))) {
			add(false, "Tên hàng in trên nhiều dòng, đã tự ghép: kiểm tra tên đúng và đủ")
		}
	}
	if !l.Certain && (l.Kind == model.KindGoods || l.Kind == model.KindService) {
		add(false, "Chưa chắc là hàng hóa hay dịch vụ: hãy chọn đúng loại")
	}
}

// CheckBuyer flags an invoice not issued to the user's company (when its tax code is set).
func CheckBuyer(inv *model.Invoice, companyTax string) {
	buyer := strings.ReplaceAll(strings.TrimSpace(inv.Buyer.TaxCode), " ", "")
	companyTax = strings.ReplaceAll(strings.TrimSpace(companyTax), " ", "")
	switch {
	case companyTax == "":
	case buyer == "":
		inv.Issues = append(inv.Issues, model.Issue{Text: "Không có MST người mua: hóa đơn có thể không được khấu trừ thuế"})
	case buyer != companyTax:
		inv.Issues = append(inv.Issues, model.Issue{Text: fmt.Sprintf("Hóa đơn xuất cho MST %s, không phải doanh nghiệp bạn (%s)", buyer, companyTax)})
	}
}

// Problems lists what still prevents saving: blocking issues and unconfirmed ones.
func Problems(inv *model.Invoice) []string {
	var out []string
	for _, is := range inv.Issues {
		if is.Block {
			out = append(out, is.Text)
		} else if !inv.Confirmed {
			out = append(out, "Chưa xác nhận: "+is.Text)
		}
	}
	for _, l := range inv.Lines {
		for _, is := range l.Issues {
			if is.Block {
				out = append(out, fmt.Sprintf("Dòng %d: %s", l.STT, is.Text))
			} else if !l.Confirmed {
				out = append(out, fmt.Sprintf("Dòng %d chưa xác nhận: %s", l.STT, is.Text))
			}
		}
	}
	return out
}
