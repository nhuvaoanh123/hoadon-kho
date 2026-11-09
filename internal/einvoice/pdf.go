package einvoice

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

// ErrNoText means the PDF is a scanned image without a text layer.
var ErrNoText = errors.New("PDF không có lớp chữ (ảnh scan): hãy tải file XML của hóa đơn từ hoadondientu.gdt.gov.vn hoặc nhập tay")

// ParsePDF extracts text rows from a PDF and parses them with ParseText.
func ParsePDF(data []byte) (inv *model.Invoice, warns []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			inv, warns, err = nil, nil, fmt.Errorf("không đọc được PDF: %v", r)
		}
	}()
	lines, err := PDFLines(data)
	if err != nil {
		return nil, nil, err
	}
	if len(strings.TrimSpace(strings.Join(lines, ""))) < 20 {
		return nil, nil, ErrNoText
	}
	inv, warns, err = ParseText(lines)
	if err == nil {
		warns = append([]string{"Đọc từ PDF: hãy kiểm tra kỹ từng dòng trước khi lưu (XML luôn chính xác hơn)"}, warns...)
	}
	return inv, warns, err
}

// PDFLines returns the text of a PDF as visual lines, top to bottom.
func PDFLines(data []byte) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			lines, err = nil, fmt.Errorf("không đọc được PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("không đọc được PDF: %w", err)
	}
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		rows, err := p.GetTextByRow()
		if err != nil {
			continue
		}
		lines = append(lines, topDown(joinRows(rows))...)
	}
	return lines, nil
}

var (
	headMarks = []string{"ký hiệu", "hóa đơn giá trị", "mã số thuế"}
	footMarks = []string{"tổng cộng", "tổng tiền thanh toán", "người mua hàng", "ký bởi"}
)

// topDown reverses a page whose y axis is flipped (header found below the footer).
func topDown(ls []string) []string {
	first := func(marks []string, last bool) int {
		at := -1
		for i, l := range ls {
			low := strings.ToLower(l)
			for _, m := range marks {
				if strings.Contains(low, m) && (at < 0 || last) {
					at = i
				}
			}
		}
		return at
	}
	h, f := first(headMarks, false), first(footMarks, true)
	if h < 0 || f < 0 || h < f {
		return ls
	}
	for i, j := 0, len(ls)-1; i < j; i, j = i+1, j-1 {
		ls[i], ls[j] = ls[j], ls[i]
	}
	return ls
}

// joinRows merges rows whose baselines differ by under 2pt and joins each row's runs.
func joinRows(rows pdf.Rows) []string {
	var out []string
	var cur []pdf.Text
	var pos int64
	flush := func() {
		if len(cur) == 0 {
			return
		}
		sortByX(cur)
		var b strings.Builder
		for i, t := range cur {
			if i > 0 && !glued(cur[i-1], t) {
				b.WriteByte(' ')
			}
			b.WriteString(t.S)
		}
		if s := textutil.Clean(b.String()); s != "" {
			out = append(out, s)
		}
		cur = nil
	}
	for _, row := range rows {
		if len(cur) > 0 && (pos-row.Position > 2 || row.Position-pos > 2) {
			flush()
		}
		if len(cur) == 0 {
			pos = row.Position
		}
		cur = append(cur, row.Content...)
	}
	flush()
	return out
}

func sortByX(ts []pdf.Text) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].X < ts[j-1].X; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// glued reports whether two runs belong together: pieces of one TJ array share X,
// and PDFs that emit one glyph per run place them close together.
func glued(a, b pdf.Text) bool {
	if b.X-a.X < 0.01 || strings.HasSuffix(a.S, " ") || strings.HasPrefix(b.S, " ") {
		return true
	}
	n := len([]rune(a.S))
	return n <= 2 && b.X-a.X < float64(n)*7.5
}

var (
	reSerial  = regexp.MustCompile(`(?i)k[ýy] hi[ệe]u[^:]*:\s*([0-9][A-Z0-9]{5,7})\b`)
	reNumber  = regexp.MustCompile(`(?i)\bs[ốo]\s*(?:h[óo]a đ[ơo]n)?\s*(?:\(\s*no\.?\s*\))?\s*:\s*0*([0-9]{1,8})\b`)
	reDate    = regexp.MustCompile(`(?i)ng[àa]y\D{0,20}?(\d{1,2})\s*th[áa]ng\D{0,20}?(\d{1,2})\s*n[ăa]m\D{0,20}?(\d{4})`)
	reTax     = regexp.MustCompile(`(?i)m[ãa] s[ốo] thu[ếe][^:]*:\s*([0-9][0-9 ]{8,13}[0-9](?:-[0-9]{3})?)`)
	reCQT     = regexp.MustCompile(`(?i)m[ãa] (?:c[ủu]a )?cqt[^:]*:\s*([0-9A-F]{20,40})`)
	reBuyer   = regexp.MustCompile(`(?i)t[êe]n đ[ơo]n v[ịi][^:]*:\s*(.+)`)
	reSeller  = regexp.MustCompile(`(?i)(?:đ[ơo]n v[ịi] b[áa]n h[àa]ng|ng[ưu][ờo]i b[áa]n)[^:]*:\s*(.+)`)
	reAddr    = regexp.MustCompile(`(?i)^đ[ịi]a ch[ỉi][^:]*:\s*(.+)`)
	reCompany = regexp.MustCompile(`^(CÔNG TY|CTY|DOANH NGHIỆP|HỘ KINH DOANH|CHI NHÁNH|TẬP ĐOÀN|HỢP TÁC XÃ)\b`)
	reRate    = regexp.MustCompile(`(?i)^(\d{1,2}(?:[.,]\d+)?%|KCT|KKKNT)$`)
)

// ParseText parses the text lines of a printed VAT invoice.
func ParseText(lines []string) (*model.Invoice, []string, error) {
	inv := &model.Invoice{Currency: "VND", ExRate: 1}
	var warns []string
	all := strings.Join(lines, "\n")
	if m := reSerial.FindStringSubmatch(all); m != nil {
		s := strings.ToUpper(m[1])
		inv.Form, inv.Serial = s[:1], s[1:]
	}
	if m := reNumber.FindStringSubmatch(all); m != nil {
		inv.Number = m[1]
	}
	if m := reDate.FindStringSubmatch(all); m != nil {
		d, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		inv.Date = fmt.Sprintf("%s-%02d-%02d", m[3], mo, d)
	}
	if m := reCQT.FindStringSubmatch(all); m != nil {
		inv.TaxAuthCode = m[1]
	}
	taxes := reTax.FindAllStringSubmatch(all, -1)
	if len(taxes) > 0 {
		inv.Seller.TaxCode = strings.ReplaceAll(taxes[0][1], " ", "")
	}
	if len(taxes) > 1 {
		inv.Buyer.TaxCode = strings.ReplaceAll(taxes[1][1], " ", "")
	}
	var addrs []string
	for _, ln := range lines {
		switch {
		case inv.Seller.Name == "" && reSeller.MatchString(ln):
			inv.Seller.Name = reSeller.FindStringSubmatch(ln)[1]
		case inv.Seller.Name == "" && reCompany.MatchString(strings.ToUpper(ln)) && ln == strings.ToUpper(ln):
			inv.Seller.Name = ln
		case inv.Buyer.Name == "" && reBuyer.MatchString(ln):
			inv.Buyer.Name = reBuyer.FindStringSubmatch(ln)[1]
		case reAddr.MatchString(ln):
			addrs = append(addrs, reAddr.FindStringSubmatch(ln)[1])
		}
	}
	if len(addrs) > 0 {
		inv.Seller.Address = addrs[0]
	}
	if len(addrs) > 1 {
		inv.Buyer.Address = addrs[len(addrs)-1]
	}

	stated := parseItems(lines, inv, &warns)
	for _, need := range []struct{ v, name string }{{inv.Number, "số hóa đơn"}, {inv.Date, "ngày hóa đơn"}, {inv.Seller.TaxCode, "MST người bán"}} {
		if need.v == "" {
			warns = append(warns, "Không đọc được "+need.name+", hãy nhập tay")
		}
	}
	if len(inv.Lines) == 0 {
		return nil, nil, errors.New("không tìm thấy dòng hàng hóa nào trong PDF: hãy dùng file XML hoặc nhập tay")
	}
	finishTotals(inv, 0, 0, 0, &warns)
	if stated > 0 && abs(stated-inv.Total) > 1 {
		warns = append(warns, fmt.Sprintf("Tổng thanh toán in trên hóa đơn (%s) khác tổng đọc được (%s): có thể thiếu dòng", money(stated), money(inv.Total)))
	}
	return inv, warns, nil
}

func isNum(s string) bool { _, ok := textutil.ParseNumber(s); return ok }

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

var totalMarkers = []string{"tổng hợp", "cộng tiền hàng", "tổng cộng", "tổng tiền", "cộng tiền"}

// parseItems fills inv.Lines and returns the stated grand total (0 if not found).
func parseItems(lines []string, inv *model.Invoice, warns *[]string) float64 {
	start := 0
	for i, ln := range lines {
		l := strings.ToLower(ln)
		if strings.Contains(l, "stt") || strings.Contains(l, "tên hàng") {
			start = i + 1
			break
		}
	}
	var pending []string // dòng chữ bị xuống dòng, chưa biết thuộc dòng hàng nào
	var heads []int      // số dòng tên nằm phía trên dòng số của từng mặt hàng
	var stated float64
	inTable := true
	for _, ln := range lines[start:] {
		low := strings.ToLower(ln)
		if inTable {
			for _, m := range totalMarkers {
				if strings.HasPrefix(low, m) {
					inTable = false
				}
			}
		}
		if !inTable {
			if strings.HasPrefix(low, "tổng cộng") || strings.Contains(low, "tổng tiền thanh toán") || strings.Contains(low, "cộng tiền thanh toán") {
				f := strings.Fields(ln)
				for i := len(f) - 1; i >= 0; i-- {
					if v, ok := textutil.ParseNumber(f[i]); ok && v > stated {
						stated = v
						break
					}
				}
			}
			continue
		}
		l, ok := itemRow(ln)
		if !ok {
			pending = append(pending, ln)
			continue
		}
		var tail int // số dòng chờ thuộc về mặt hàng trước (phần dưới của tên)
		if len(inv.Lines) == 0 {
			pending = pending[max(0, len(pending)-1):] // các dòng trước là tiêu đề cột
			if l.Name != "" {
				pending = nil
			}
		} else {
			prevHead := heads[len(heads)-1]
			switch {
			case l.Name == "":
				tail = max(0, min(prevHead, len(pending)-1)) // tên căn giữa: trên = dưới
			case prevHead == 0:
				tail = len(pending) // tên căn trên: các dòng chờ là phần dưới
			default:
				tail = min(prevHead, len(pending))
			}
			if tail > 0 {
				prev := &inv.Lines[len(inv.Lines)-1]
				prev.Name = textutil.Clean(prev.Name + " " + strings.Join(pending[:tail], " "))
			}
		}
		head := pending[min(tail, len(pending)):]
		l.Name = textutil.Clean(strings.Join(head, " ") + " " + l.Name)
		heads = append(heads, len(head))
		pending = nil
		inv.Lines = append(inv.Lines, l)
	}
	if len(pending) > 0 && len(inv.Lines) > 0 {
		prev := &inv.Lines[len(inv.Lines)-1]
		prev.Name = textutil.Clean(prev.Name + " " + strings.Join(pending, " "))
	}
	for _, l := range inv.Lines {
		if l.Qty != 0 && l.Price != 0 && abs(l.Qty*l.Price-l.Amount) > 1+l.Amount*0.001 {
			*warns = append(*warns, fmt.Sprintf("Dòng %d: số lượng × đơn giá khác thành tiền, hãy kiểm tra", l.STT))
		}
	}
	return stated
}

// itemRow parses "STT name unit qty price amount [rate [vat]]".
func itemRow(ln string) (model.Line, bool) {
	f := strings.Fields(ln)
	if len(f) < 5 {
		return model.Line{}, false
	}
	stt, err := strconv.Atoi(f[0])
	if err != nil || stt < 1 || stt > 9999 {
		return model.Line{}, false
	}
	end := len(f)
	var tail []string // các ô số/thuế suất ở cuối dòng
	for end > 1 && len(tail) < 5 && (isNum(f[end-1]) || reRate.MatchString(f[end-1])) {
		tail = append([]string{f[end-1]}, tail...)
		end--
	}
	rateAt := -1
	for i, t := range tail {
		if reRate.MatchString(t) && !isNum(t) {
			rateAt = i
		}
	}
	var qty, price, amount, vat string
	var rate string
	switch {
	case rateAt >= 3:
		qty, price, amount, rate = tail[rateAt-3], tail[rateAt-2], tail[rateAt-1], tail[rateAt]
		if rateAt+1 < len(tail) {
			vat = tail[rateAt+1]
		}
	case rateAt < 0 && len(tail) >= 3:
		qty, price, amount = tail[len(tail)-3], tail[len(tail)-2], tail[len(tail)-1]
	default:
		return model.Line{}, false
	}
	if end < 2 || !hasLetter(f[end-1]) {
		return model.Line{}, false
	}
	l := model.Line{STT: stt, Nature: 1, Unit: f[end-1], Name: strings.Join(f[1:end-1], " "), VATRate: textutil.NormRate(rate)}
	l.Qty, _ = textutil.ParseNumber(qty)
	l.Price, _ = textutil.ParseNumber(price)
	l.Amount, _ = textutil.ParseNumber(amount)
	if v, ok := textutil.ParseNumber(vat); ok {
		l.VATAmount = v
	} else {
		l.VATAmount = textutil.Round(l.Amount*textutil.ParseRate(l.VATRate), 0)
	}
	if strings.Contains(strings.ToLower(l.Name), "chiết khấu") {
		l.Nature = 3
	}
	return l, true
}
