package einvoice

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
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
func ParsePDF(data []byte) (*model.Invoice, []string, error) { return parsePDF(data, nil) }

// parsePDF parses lines already read by PDFium, or reads them when lines is nil.
func parsePDF(data []byte, lines []string) (inv *model.Invoice, warns []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			inv, warns, err = nil, nil, fmt.Errorf("không đọc được PDF: %v", r)
		}
	}()
	scrambled := false
	if len(strings.Join(lines, "")) < 20 {
		lines, scrambled, err = readPDF(data)
	}
	if err == nil && len(strings.TrimSpace(strings.Join(lines, ""))) < 20 {
		err = ErrNoText
	}
	if err == nil {
		if inv, warns, err = ParseText(lines); err == nil {
			inv.Source = model.SourcePDF
			if scrambled {
				inv.Source = model.SourcePDFRaw
			}
			return inv, warns, nil
		}
	}
	// Không có chữ hoặc chữ lỗi font: thử OCR trang PDF.
	if oinv, owarns, oerr := ParseImage(data, ".pdf"); oerr == nil {
		return oinv, owarns, nil
	}
	return nil, nil, err
}

// pdfInvoices splits a PDF's pages into invoices: a page printing another serial and number
// starts a new invoice, pages without them (continuation pages) stay with the previous one.
func pdfInvoices(data []byte) (groups [][]string) {
	defer func() {
		if recover() != nil {
			groups = nil
		}
	}()
	pages, err := pdfiumPages(data)
	if err != nil {
		return nil
	}
	cur := ""
	for _, pg := range pages {
		key := pageKey(pg)
		if len(groups) == 0 || (key != "" && cur != "" && key != cur) {
			groups = append(groups, nil)
		}
		if key != "" {
			cur = key
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], pg...)
	}
	return groups
}

// pageKey is the serial and number printed on a page, or "" when the page has none.
func pageKey(lines []string) string {
	all := strings.Join(lines, "\n")
	ser := ""
	if m := reSerial.FindStringSubmatch(all); m != nil {
		ser = strings.ToUpper(m[1])
	} else if m := reSerial2.FindStringSubmatch(all); m != nil {
		ser = m[2]
	} else if m := reSerialOld.FindStringSubmatch(all); m != nil {
		ser = strings.ToUpper(m[1])
	}
	for _, re := range []*regexp.Regexp{reNumber, reNumber2} {
		if m := re.FindStringSubmatch(all); m != nil && ser != "" {
			return strings.TrimLeft(ser, "123456") + "|" + m[1]
		}
	}
	return ""
}

// PDFLines returns the text of a PDF as visual lines, top to bottom.
func PDFLines(data []byte) ([]string, error) {
	lines, _, err := readPDF(data)
	return lines, err
}

// readPDF also reports whether any page needed the glyph fallback (text order unreliable).
func readPDF(data []byte) (lines []string, scrambled bool, err error) {
	if ls, perr := pdfiumLines(data); perr == nil && len(strings.Join(ls, "")) >= 20 {
		return ls, false, nil
	}
	return legacyPDF(data)
}

// legacyPDF is the pure-Go fallback reader for PDFs PDFium cannot open.
func legacyPDF(data []byte) (lines []string, scrambled bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			lines, err = nil, fmt.Errorf("không đọc được PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, false, fmt.Errorf("không đọc được PDF: %w", err)
	}
	lastFlip := false
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		rows, err := p.GetTextByRow()
		if err != nil {
			continue
		}
		page := joinRows(rows)
		if n := len(strings.Join(page, "")); len(page) <= 3 && n > 200 {
			if g := glyphLines(p); len(g) > len(page) {
				page, scrambled = g, true
			}
		}
		flip, known := flipped(page)
		if !known {
			flip = lastFlip
		}
		lastFlip = flip
		if flip {
			for a, b := 0, len(page)-1; a < b; a, b = a+1, b-1 {
				page[a], page[b] = page[b], page[a]
			}
		}
		lines = append(lines, page...)
	}
	return lines, scrambled, nil
}

var (
	headMarks = []string{"ký hiệu", "hóa đơn giá trị", "mã số thuế"}
	footMarks = []string{"tổng cộng", "tổng tiền thanh toán", "người mua hàng", "ký bởi"}
)

// flipped reports whether a page's y axis is inverted: header below footer, or item numbers descending.
func flipped(ls []string) (flip, known bool) {
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
	if h >= 0 && f >= 0 {
		return h > f, true
	}
	var stts []int
	for _, l := range ls {
		if fs := strings.Fields(l); len(fs) >= 4 {
			if n, err := strconv.Atoi(fs[0]); err == nil && n > 0 {
				stts = append(stts, n)
			}
		}
	}
	up, down := 0, 0
	for i := 1; i < len(stts); i++ {
		if stts[i] == stts[i-1]+1 {
			up++
		} else if stts[i] == stts[i-1]-1 {
			down++
		}
	}
	if up+down >= 2 {
		return down > up, true
	}
	return false, false
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
	reSerial   = regexp.MustCompile(`(?i)k[ýy] hi[ệe]u[^:]*:\s*([1-6]?[CK][0-9]{2}[A-Z]{2,4})\b`)
	reNumber   = regexp.MustCompile(`(?i)\bs[ốo]\s*(?:h[óo]a đ[ơo]n)?\s*(?:\(\s*no\.?\s*\))?\s*:\s*0*([0-9]{1,8})\b`)
	reDate     = regexp.MustCompile(`(?i)ng[àa]y\D{0,20}?(\d{1,2})\s*th[áa]ng\D{0,20}?(\d{1,2})\s*n[ăa]m\D{0,20}?(\d{4})`)
	reTax      = regexp.MustCompile(`(?i)(?:m[ãa] s[ốo] thu[ếe]|\bmst\b)[^:0-9]{0,30}:?\s*((?:[0-9] ){9,12}[0-9]\b|[0-9][0-9 ]{8,13}[0-9](?:-[0-9]{3})?)`)
	reCQT      = regexp.MustCompile(`(?i)m[ãa] (?:c[ủu]a )?cqt[^:]*:\s*([0-9A-F]{20,40})`)
	reBuyer    = regexp.MustCompile(`(?i)(?:t[êe]n đ[ơo]n v[ịi]|đ[ơo]n v[ịi] mua h[àa]ng)[^:]*:\s*(.+)`)
	reNameEnd  = regexp.MustCompile(`(?i)\s+(?:mst|m[ãa] s[ốo] thu[ếe]|tax code)\b.*$`)
	reDateISO  = regexp.MustCompile(`(?i)ng[àa]y[^0-9]{0,20}(20\d{2})-(\d{1,2})-(\d{1,2})\b`)
	reDateDMY  = regexp.MustCompile(`(?i)ng[àa]y[^0-9]{0,20}(\d{1,2})[/.-](\d{1,2})[/.-](20\d{2})\b`)
	reSeller   = regexp.MustCompile(`(?i)(?:đ[ơo]n v[ịi] b[áa]n h[àa]ng|ng[ưu][ờo]i b[áa]n)[^:]*:\s*(.+)`)
	reAddr     = regexp.MustCompile(`(?i)^đ[ịi]a ch[ỉi][^:]*:\s*(.+)`)
	reCompany  = regexp.MustCompile(`^(CÔNG TY|CTY|DOANH NGHIỆP|HỘ KINH DOANH|CHI NHÁNH|TẬP ĐOÀN|HỢP TÁC XÃ)\b`)
	reSerial2  = regexp.MustCompile(`\b([1-6Il|])([CK]\d{2}[A-Z]{3})\b`)
	reNumber2  = regexp.MustCompile(`(?i)\(no\.?\)\s*[:.]?\s*0*([0-9]{1,8})\b`)
	reNumber3  = regexp.MustCompile(`\b0{2,}([1-9][0-9]{0,6})\b`)
	reDate2    = regexp.MustCompile(`(?i)\(date\)\D{0,15}?(\d{1,2})\D{1,25}?(\d{1,2})\D{1,25}?(\d{4})`)
	reDate3    = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(20\d{2})\b`)
	reTax2     = regexp.MustCompile(`(?i)tax code\)?\s*[:.]?\s*((?:[0-9] ){9,12}[0-9]\b|[0-9][0-9 ]{8,13}[0-9](?:-[0-9]{3})?)`)
	reCurrency = regexp.MustCompile(`(?i)(?:đồng tiền thanh toán|loại tiền(?: tệ)?|currency)[^:\n]{0,30}:\s*([A-Z]{3})\b`)
	reExRate   = regexp.MustCompile(`(?i)(?:tỷ giá|tỉ giá|exchange rate)[^:\n]{0,30}:\s*([0-9][0-9.,]*)\s*(?:VND|đ)?\s*(?:/\s*([A-Z]{3}))?`)
	// Hóa đơn mẫu cũ (trước 7/2022): mẫu số 01GTKT0/001, ký hiệu KT/18E.
	reFormOld   = regexp.MustCompile(`(?i)m[ẫa]u s[ốo][^:\n]{0,20}:\s*([0-9]{2}[A-Z]{4}[0-9]/[0-9]{3})`)
	reSerialOld = regexp.MustCompile(`(?i)k[ýy] hi[ệe]u[^:\n]{0,20}:\s*([A-Z]{2}/[0-9]{2}[A-Z])\b`)
	reRate      = regexp.MustCompile(`(?i)^(\d{1,2}(?:[.,]\d+)?%|KCT|KKKNT)$`)
)

// ParseText parses the text lines of a printed VAT invoice.
func ParseText(lines []string) (*model.Invoice, []string, error) {
	lines = vnNumbers(lines)
	inv := &model.Invoice{Currency: "VND", ExRate: 1}
	var warns []string
	all := strings.Join(lines, "\n")
	if m := reSerial.FindStringSubmatch(all); m != nil {
		s := strings.ToUpper(m[1])
		if s[0] == 'C' || s[0] == 'K' { // ký hiệu in không kèm số mẫu
			s = defaultForm(all) + s
		}
		inv.Form, inv.Serial = s[:1], s[1:]
	} else if m := reSerial2.FindStringSubmatch(all); m != nil {
		inv.Form, inv.Serial = m[1], m[2]
		if m[1] < "1" || m[1] > "6" { // OCR đọc "1" thành I, l, |
			inv.Form = "1"
		}
	} else if m := reSerialOld.FindStringSubmatch(all); m != nil {
		inv.Serial = strings.ToUpper(m[1])
		if f := reFormOld.FindStringSubmatch(all); f != nil {
			inv.Form = strings.ToUpper(f[1])
		}
	}
	for _, re := range []*regexp.Regexp{reNumber, reNumber2, reNumber3} {
		if m := re.FindStringSubmatch(all); m != nil {
			inv.Number = m[1]
			break
		}
	}
	for _, re := range []*regexp.Regexp{reDate, reDateISO, reDateDMY, reDate2, reDate3} {
		m := re.FindStringSubmatch(all)
		if m == nil {
			continue
		}
		if re == reDateISO {
			m = []string{m[0], m[3], m[2], m[1]}
		}
		d, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		if d >= 1 && d <= 31 && mo >= 1 && mo <= 12 {
			inv.Date = fmt.Sprintf("%s-%02d-%02d", m[3], mo, d)
			break
		}
	}
	if m := reCurrency.FindStringSubmatch(all); m != nil && strings.ToUpper(m[1]) != "VND" {
		inv.Currency = strings.ToUpper(m[1])
	}
	if m := reExRate.FindStringSubmatch(all); m != nil {
		if v, ok := textutil.ParseNumber(m[1]); ok && v > 1 {
			inv.ExRate = v
			if m[2] != "" && strings.ToUpper(m[2]) != "VND" {
				inv.Currency = strings.ToUpper(m[2])
			}
		}
	}
	if m := reCQT.FindStringSubmatch(all); m != nil {
		inv.TaxAuthCode = m[1]
	}
	taxes := reTax.FindAllStringSubmatch(all, -1)
	if len(taxes) == 0 {
		taxes = reTax2.FindAllStringSubmatch(all, -1)
	}
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
	inv.Seller.Name = reNameEnd.ReplaceAllString(inv.Seller.Name, "")
	inv.Buyer.Name = reNameEnd.ReplaceAllString(inv.Buyer.Name, "")
	if len(addrs) > 0 {
		inv.Seller.Address = addrs[0]
	}
	if len(addrs) > 1 {
		inv.Buyer.Address = addrs[len(addrs)-1]
	}

	parseItems(lines, inv) // số, ngày, MST thiếu: Verify báo lỗi phải sửa, không cảnh báo lặp lại
	if len(inv.Lines) == 0 {
		return nil, nil, errors.New("không tìm thấy dòng hàng hóa nào: hãy dùng file XML hoặc nhập tay")
	}
	if st := &inv.Stated; st.VAT == 0 && st.ByRateVAT > 0 {
		st.VAT = st.ByRateVAT
	}
	if st := &inv.Stated; st.BeforeVAT == 0 && st.ByRateBefore > 0 {
		st.BeforeVAT = st.ByRateBefore
	}
	invoiceRate(inv)
	finishTotals(inv, 0, 0, 0, &warns)
	return inv, warns, nil
}

var (
	reEnNum  = regexp.MustCompile(`^\d{1,3}(?:,\d{3})+(?:\.\d+)?$|^\d+\.\d{2}$`)
	reVnNum  = regexp.MustCompile(`^\d{1,3}(?:\.\d{3})+(?:,\d+)?$|^\d+,\d{2}$`)
	reNumTok = regexp.MustCompile(`^[\d.,]+$`)
)

// vnNumbers rewrites English-style numbers ("1,800,000.00") to Vietnamese style when the
// document mostly uses them, so a lone "288,000" is not read as 288.
func vnNumbers(lines []string) []string {
	en, vn := 0, 0
	for _, l := range lines {
		for _, t := range strings.Fields(l) {
			switch {
			case reEnNum.MatchString(t):
				en++
			case reVnNum.MatchString(t):
				vn++
			}
		}
	}
	if en <= vn {
		return lines
	}
	swap := strings.NewReplacer(",", ".", ".", ",")
	out := make([]string, len(lines))
	for i, l := range lines {
		f := strings.Fields(l)
		for j, t := range f {
			if reNumTok.MatchString(t) && strings.ContainsAny(t, "0123456789") {
				f[j] = swap.Replace(t)
			}
		}
		out[i] = strings.Join(f, " ")
	}
	return out
}

// defaultForm guesses the form number: 2 for hóa đơn bán hàng, else 1 (GTGT).
func defaultForm(all string) string {
	if strings.Contains(textutil.Fold(all), "hoa don ban hang") {
		return "2"
	}
	return "1"
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

var (
	totalMarkers = []string{"tong hop", "cong tien hang", "tong cong", "tong tien", "cong tien", "total", "tong so tien"}
	headerWords  = []string{"gtgt", "thanh toan", "tien thue", "thue suat", "(total", "in words", "bang chu"}
)

var reInvRate = regexp.MustCompile(`^thue (?:suat(?: thue)?(?: gtgt)?|gtgt)\s*(?:\([^)]*\))?\s*:\s*(\d+(?:[.,]\d+)?\s*%)`)
var reReduced = regexp.MustCompile(`^da giam ([0-9][0-9.,]*) dong`)

// firstNumberAfter is the first number (>= 100) after the first word of the line equal to one
// of the folded labels; ln and its folded form low have the same words.
func firstNumberAfter(ln, low string, labels ...string) (float64, bool) {
	f, fl := strings.Fields(ln), strings.Fields(low)
	if len(f) != len(fl) {
		return 0, false
	}
	start := -1
	for i, w := range fl {
		for _, lab := range labels {
			if strings.Trim(w, ":()") == lab {
				start = i
				break
			}
		}
		if start >= 0 {
			break
		}
	}
	for i := start + 1; start >= 0 && i < len(f); i++ {
		if v, ok := textutil.ParseNumber(f[i]); ok && v >= 100 {
			return v, true
		}
	}
	return 0, false
}

// lastNumber is the last number in s.
func lastNumber(s string) (float64, bool) {
	f := strings.Fields(s)
	for i := len(f) - 1; i >= 0; i-- {
		if v, ok := textutil.ParseNumber(f[i]); ok {
			return v, true
		}
	}
	return 0, false
}

// invoiceRate gives lines without a rate the single rate printed under the table, when the
// printed VAT is exactly what that rate makes of the lines.
func invoiceRate(inv *model.Invoice) {
	st := inv.Stated
	derived := false
	if st.Rate == "" && st.VAT > 0 { // không in thuế suất: suy từ tiền thuế, người dùng phải xác nhận
		for _, cand := range []string{"10%", "8%", "5%"} {
			if v := sumVAT(inv, textutil.ParseRate(cand)); v >= 0 && abs(v-st.VAT) <= float64(len(inv.Lines)) {
				st.Rate, derived = cand, true
				break
			}
		}
	}
	if st.Rate == "" {
		return
	}
	r := textutil.ParseRate(st.Rate)
	var vat float64
	for _, l := range inv.Lines {
		if l.VATRate != "" || l.VATAmount != 0 {
			return
		}
		vat += textutil.Round(l.Amount*r, 0)
	}
	if abs(vat-st.VAT) > float64(len(inv.Lines)) {
		return
	}
	for i := range inv.Lines {
		inv.Lines[i].VATRate = st.Rate
		inv.Lines[i].VATAmount = textutil.Round(inv.Lines[i].Amount*r, 0)
		if derived {
			inv.Lines[i].Notes = append(inv.Lines[i].Notes, "hóa đơn không in thuế suất, tính từ tiền thuế được "+st.Rate)
		}
	}
}

// sumVAT is the VAT of all lines at rate r, or -1 when a line already has a rate or VAT.
func sumVAT(inv *model.Invoice, r float64) float64 {
	var v float64
	for _, l := range inv.Lines {
		if l.VATRate != "" || l.VATAmount != 0 {
			return -1
		}
		v += textutil.Round(l.Amount*r, 0)
	}
	return v
}

// bareTotal reports a "Cộng" / "Cộng (Total):" row, not "Cộng tiền hàng".
func bareTotal(low string) bool {
	f := strings.Fields(low)
	return len(f) > 0 && strings.TrimRight(f[0], ":") == "cong" && (len(f) == 1 || !strings.HasPrefix(f[1], "tien"))
}

// endOfTable reports whether a folded line starts the totals block below the item table;
// column-header words count only once items were seen (headers repeat them).
func endOfTable(low string, seenItems bool) bool {
	for _, m := range totalMarkers {
		if strings.HasPrefix(low, m) {
			return true
		}
	}
	if !seenItems {
		return false
	}
	if bareTotal(low) {
		return true
	}
	for _, w := range []string{"tien hang", "thanh toan", "bang chu", "in words"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	n := 0
	for _, w := range headerWords {
		if strings.Contains(low, w) {
			n++
		}
	}
	return n >= 2
}

// parseItems fills inv.Lines and inv.Stated (totals printed below the table).
func parseItems(lines []string, inv *model.Invoice) {
	start := 0
	var cols tableCols
	for i, ln := range lines {
		l := textutil.Fold(ln)
		if strings.Contains(l, "stt") || strings.Contains(l, "ten hang") || strings.Contains(l, "name of goods") {
			start = i + 1
			cols.code = strings.Contains(l, "ma hang") || strings.Contains(l, "ma hh") || strings.Contains(l, "ma vt") || strings.Contains(l, "item code")
			head := textutil.Fold(strings.Join(lines[max(0, i-1):min(len(lines), i+3)], " "))
			cols.noPrice = !strings.Contains(head, "don gia") && !strings.Contains(head, "price") && !strings.Contains(head, "gia ban")
			cols.lot = strings.Contains(head, "so lo") || strings.Contains(head, "han dung") || strings.Contains(head, "lot") || strings.Contains(head, "exp")
			cols.noUnit = !strings.Contains(head, "don vi") && !strings.Contains(head, "vi tinh") && !strings.Contains(head, "dvt") && !strings.Contains(head, "(unit)") && !strings.Contains(head, "uom")
			break
		}
	}
	var pending []string // dòng chữ bị xuống dòng, chưa biết thuộc dòng hàng nào
	var heads []int      // số dòng tên nằm phía trên dòng số của từng mặt hàng
	inTable, newPage := true, false
	var lone float64
	for _, ln := range lines[start:] {
		low := textutil.Fold(ln)
		if inTable && endOfTable(low, len(inv.Lines) > 0) {
			inTable = false
		}
		if !inTable {
			if v, ok := textutil.ParseNumber(ln); ok {
				lone = v // số tổng nằm giữa hai dòng nhãn (ô gộp)
				continue
			}
			if lone > 0 && !hasDigit(ln) && (strings.Contains(low, "thanh toan") || strings.HasPrefix(low, "tong cong")) && inv.Stated.Total == 0 {
				inv.Stated.Total = lone
			}
			lone = 0
			if m := reReduced.FindStringSubmatch(low); m != nil && len(inv.Lines) > 0 {
				if v, ok := textutil.ParseNumber(m[1]); ok && v > 0 {
					inv.Lines = append(inv.Lines, model.Line{STT: len(inv.Lines) + 1, Nature: 3, Name: textutil.Clean(ln), Amount: v})
				}
				continue
			}
			statedLine(ln, low, &inv.Stated)
			continue
		}
		if pageHeader(low) {
			// Sang trang: dòng chờ là phần dưới tên mặt hàng trước; tiêu đề lặp lại bị bỏ như ở trang đầu.
			if !newPage && len(pending) > 0 && len(inv.Lines) > 0 {
				prev := &inv.Lines[len(inv.Lines)-1]
				prev.Name = joinName(prev.Name, pending)
				prev.Wrapped = true
			}
			pending, newPage = nil, true
			continue
		}
		l, ok := itemRow(ln, cols)
		if !ok {
			pending = append(pending, ln)
			continue
		}
		var tail int // số dòng chờ thuộc về mặt hàng trước (phần dưới của tên)
		if len(inv.Lines) == 0 || newPage {
			newPage = false
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
				prev.Name = joinName(prev.Name, pending[:tail])
				prev.Wrapped = true
			}
		}
		head := pending[min(tail, len(pending)):]
		l.Name = joinName("", append(head, l.Name))
		l.Wrapped = len(head) > 0
		heads = append(heads, len(head))
		pending = nil
		inv.Lines = append(inv.Lines, l)
	}
	if len(pending) > 0 && len(inv.Lines) > 0 {
		prev := &inv.Lines[len(inv.Lines)-1]
		prev.Name = joinName(prev.Name, pending)
		prev.Wrapped = true
	}
}

// joinName appends wrapped name lines; a line ending in a code hyphen ("CM-536-") joins without a space.
func joinName(name string, parts []string) string {
	for _, p := range parts {
		p = textutil.Clean(p)
		switch {
		case p == "":
		case name == "":
			name = p
		case codeHyphen(name):
			name += p
		default:
			name += " " + p
		}
	}
	return textutil.Clean(name)
}

func codeHyphen(s string) bool {
	r := []rune(s)
	n := len(r)
	return n >= 2 && r[n-1] == '-' && (unicode.IsDigit(r[n-2]) || unicode.IsUpper(r[n-2]))
}

var columnWords = []string{"stt", "ten hang", "don vi tinh", "dvt", "so luong", "don gia", "thanh tien", "thue suat",
	"tien thue", "(unit)", "(quantity)", "(amount)", "(description)", "(unit price)", "(vat rate)"}

// pageHeader reports a repeated column header, "(1) (2) (3)" numbering or a "tiếp theo trang trước" line.
func pageHeader(low string) bool {
	if strings.Contains(low, "tiep theo trang") || strings.Contains(low, "trang truoc") ||
		strings.HasPrefix(low, "(1) (2)") || columnNumbers(low) {
		return true
	}
	n := 0
	for _, w := range columnWords {
		if strings.Contains(low, w) {
			n++
		}
	}
	return n >= 2
}

var reColLabel = regexp.MustCompile(`^\(?(?:[0-9]{1,2}[a-z]?|[a-z])\)?$|^[0-9a-z]{1,2}(?:\s*[x*+=-]\s*[0-9a-z]{1,2})+$|^[=+x*-]$`)

// columnNumbers reports the numbering row under a table header: "1 2 3", "A B 1 2 3=1*2", "1 2a 2b 4x5".
func columnNumbers(low string) bool {
	f := strings.Fields(low)
	if len(f) < 3 {
		return false
	}
	for _, w := range f {
		if !reColLabel.MatchString(w) {
			return false
		}
	}
	return strings.HasPrefix(f[0], "1") || strings.HasPrefix(f[0], "a") || strings.HasPrefix(f[0], "(")
}

// statedLine picks printed totals and the amount in words from the block under the table.
func statedLine(ln, low string, st *model.Stated) {
	if i := strings.Index(low, "bang chu"); i >= 0 || strings.Contains(low, "in words") {
		if j := strings.LastIndex(ln, ":"); j >= 0 {
			st.Words = strings.TrimSpace(ln[j+1:])
		}
		return
	}
	if m := reInvRate.FindStringSubmatch(low); m != nil { // "Thuế suất thuế GTGT: 10% Tiền thuế GTGT: 4.727"
		st.Rate = textutil.NormRate(m[1])
		if v, ok := lastNumber(ln); ok && strings.Contains(low, "tien thue") {
			st.VAT = v
		}
		return
	}
	if strings.Contains(low, "thue suat") || strings.Contains(low, "khong chiu thue") || strings.Contains(low, "khong ke khai") {
		return // dòng tổng theo từng thuế suất
	}
	var nums []float64
	for _, f := range strings.Fields(ln) {
		if v, ok := textutil.ParseNumber(f); ok && v >= 100 {
			nums = append(nums, v)
		}
	}
	if len(nums) == 0 {
		return
	}
	last := nums[len(nums)-1]
	if reRatePart.MatchString(low) && !strings.HasPrefix(low, "tong cong") {
		if strings.Contains(low, "tien thue") {
			st.ByRateVAT += last
		}
		if i := strings.Index(low, "tien hang"); i >= 0 && (len(nums) > 1 || !strings.Contains(low, "tien thue")) {
			st.ByRateBefore += nums[0]
		}
		return
	}
	if bareTotal(low) { // "Cộng (Total): tiền hàng, tiền thuế[, tổng]"
		switch {
		case len(nums) >= 3:
			st.BeforeVAT, st.VAT, st.Total = nums[len(nums)-3], nums[len(nums)-2], last
		case len(nums) == 2:
			st.BeforeVAT, st.VAT = nums[0], nums[1]
		}
		return
	}
	switch {
	case strings.HasPrefix(low, "tong cong") || strings.Contains(low, "thanh toan") || strings.Contains(low, "(total)"):
		if len(nums) >= 3 {
			st.BeforeVAT, st.VAT = nums[len(nums)-3], nums[len(nums)-2]
		}
		st.Total = last
	case strings.Contains(low, "tien thue"):
		st.VAT = last
	case strings.Contains(low, "tien hang") || strings.Contains(low, "chua co thue") || strings.Contains(low, "truoc thue"):
		if v, ok := firstNumberAfter(ln, low, "hang", "thue", "chua co thue"); ok {
			st.BeforeVAT = v // nhiều số: các cột tổng (trước thuế, sau thuế), hoặc tỷ giá in phía trước
		}
	}
}

// itemRow parses "STT name [unit] qty price amount [rate] [vat]". Columns are
// found by arithmetic (qty × price = amount, vat = amount × rate), not position.
// tableCols is what the table header says about its columns.
type tableCols struct {
	code    bool // mã hàng
	noPrice bool // no unit price column (telecom bills)
	noUnit  bool // no unit column
	lot     bool // số lô / hạn dùng columns between name and unit (pharmacy)
}

func itemRow(ln string, cols tableCols) (model.Line, bool) {
	f := strings.Fields(ln)
	if len(f) < 3 {
		return model.Line{}, false
	}
	stt, err := strconv.Atoi(f[0])
	if err != nil || stt < 1 || stt > 9999 {
		return model.Line{}, false
	}
	end := len(f)
	for end > 1 && len(f)-end < 8 && (isNum(f[end-1]) || isRate(f[end-1])) {
		end--
	}
	type cell struct {
		v  float64
		at int
	}
	var nums []cell
	rate, rateAt := "", -1
	for i := end; i < len(f); i++ {
		if isRate(f[i]) && !isNum(f[i]) {
			rate, rateAt = f[i], i
			continue
		}
		v, _ := textutil.ParseNumber(f[i])
		nums = append(nums, cell{v, i})
	}
	if len(nums) < 2 && !(len(nums) == 1 && rate != "") {
		return model.Line{}, false
	}
	l := model.Line{STT: stt, Nature: 1, VATRate: textutil.NormRate(rate)}
	if len(nums) == 1 && rateAt == len(f)-1 && nums[0].v == math.Trunc(nums[0].v) && nums[0].v < 1000 {
		l.Qty = nums[0].v // món trong combo/hàng kèm: có SL, không có giá
		l.Name = strings.Join(f[1:nums[0].at], " ")
		if !cols.noUnit {
			l.Name, l.Unit = splitUnit(f[1:nums[0].at])
		}
		return l, true
	}
	qi, pi, ai := -1, -1, -1
	for k := len(nums) - 1; k >= 2 && ai < 0; k-- {
		for j := k - 1; j >= 1 && ai < 0; j-- {
			for i := j - 1; i >= 0; i-- {
				q, p, a := nums[i].v, nums[j].v, nums[k].v
				if q > 0 && (p > 0 && a > 0 && abs(q*p-a) <= max(1, a*0.0005) || p == 0 && a == 0 && j == i+1) {
					qi, pi, ai = i, j, k
					break
				}
			}
		}
	}
	firstNum := nums[0].at
	if ai >= 0 {
		l.Qty, l.Price, l.Amount = nums[qi].v, nums[pi].v, nums[ai].v
		firstNum = nums[qi].at
	} else {
		// Không khớp phép nhân: tìm cặp (thành tiền, tiền thuế), số đứng trước là SL/đơn giá.
		for j := len(nums) - 1; j >= 1 && ai < 0; j-- {
			for i := j - 1; i >= 0; i-- {
				if _, ok := vatRate(nums[i].v, nums[j].v, l.VATRate); ok && nums[i].v > 0 {
					ai = i
					break
				}
			}
		}
		if ai < 0 {
			n := len(nums)
			if rateAt >= 0 {
				n = 0
				for _, c := range nums {
					if c.at < rateAt {
						n++
					}
				}
			}
			if n == 0 {
				return model.Line{}, false
			}
			ai = n - 1
		}
		l.Amount = nums[ai].v
		prior := nums[:ai]
		switch len(prior) {
		case 0:
		case 1:
			l.Qty = prior[0].v
		default:
			l.Qty, l.Price = prior[len(prior)-2].v, prior[len(prior)-1].v
		}
		if l.Qty > 0 && l.Price == 0 {
			l.Price = textutil.Round(l.Amount/l.Qty, 2)
		}
		firstNum = nums[ai-min(2, len(prior))].at
		// Bảng không có cột đơn giá (hóa đơn viễn thông): thành tiền đã khớp tiền thuế, không phải đoán.
		vatOK := false
		if ai+1 < len(nums) {
			_, vatOK = vatRate(l.Amount, nums[ai+1].v, l.VATRate)
		}
		if !(cols.noPrice && vatOK && len(prior) <= 1) {
			l.Notes = append(l.Notes, "không tìm được SL × đơn giá = thành tiền, cột số đoán theo vị trí")
			if len(prior) == 1 && l.Qty > 0 {
				l.Notes = append(l.Notes, "đơn giá không đọc được, tính lại = thành tiền / SL")
			}
		}
	}
	// Thuế: số sau thành tiền khớp với thuế suất ghi trên dòng hoặc một thuế suất chuẩn.
	vatSet := false
	for i := len(nums) - 1; i > ai && !vatSet; i-- {
		if r, ok := vatRate(l.Amount, nums[i].v, l.VATRate); ok {
			l.VATAmount, vatSet = nums[i].v, true
			got := textutil.NormRate(strconv.FormatFloat(r*100, 'f', -1, 64) + "%")
			if l.VATRate != "" && got != l.VATRate {
				l.Notes = append(l.Notes, "thuế suất đọc là "+l.VATRate+", sửa theo tiền thuế thành "+got)
			}
			l.VATRate = got
		}
	}
	// Cột cuối là thành tiền đã có thuế: thuế = cột đó - thành tiền.
	for i := len(nums) - 1; i > ai && !vatSet && l.Amount > 0; i-- {
		if r, ok := vatRate(l.Amount, nums[i].v-l.Amount, l.VATRate); ok && nums[i].v > l.Amount {
			l.VATAmount, vatSet = nums[i].v-l.Amount, true
			l.VATRate = textutil.NormRate(strconv.FormatFloat(r*100, 'f', -1, 64) + "%")
		}
	}
	if !vatSet {
		l.VATAmount = textutil.Round(l.Amount*textutil.ParseRate(l.VATRate), 0)
		if ai < len(nums)-1 {
			l.Notes = append(l.Notes, "số sau thành tiền không khớp tiền thuế của thuế suất nào")
		}
	}
	nameEnd := min(firstNum, end)
	if nameEnd > 1 && !cols.noUnit {
		var unit string
		if _, unit = splitUnit(f[1:nameEnd]); unit != "" {
			nameEnd -= len(strings.Fields(unit))
			l.Unit = unit
		}
	}
	for k := 0; cols.lot && k < 2 && nameEnd > 1 && (reDayMonth.MatchString(f[nameEnd-1]) || reLot.MatchString(f[nameEnd-1])); k++ {
		nameEnd-- // số lô, hạn dùng: cột riêng, không phải tên hàng
	}
	nameStart := 1
	if cols.code && nameEnd > 2 && hasDigit(f[1]) && !strings.ContainsAny(f[1], ".,") {
		l.Code, nameStart = f[1], 2
	}
	l.Name = strings.Join(f[nameStart:nameEnd], " ")
	if strings.Contains(textutil.Fold(l.Name), "chiet khau") {
		l.Nature = 3
	}
	return l, true
}

// twoWordUnits are units written in two words; the last word alone is not a unit.
var twoWordUnits = map[string]bool{"san pham": true, "lan xuat": true, "goi cuoc": true}

// splitUnit takes the unit off the end of a name: a known two-word unit or a one-word unit.
func splitUnit(f []string) (name, unit string) {
	n := len(f)
	if n >= 2 && twoWordUnits[textutil.Fold(f[n-2]+" "+f[n-1])] {
		return strings.Join(f[:n-2], " "), f[n-2] + " " + f[n-1]
	}
	if n >= 1 && isUnit(f[n-1]) {
		return strings.Join(f[:n-1], " "), f[n-1]
	}
	return strings.Join(f, " "), ""
}

// vatRate returns the rate that turns amount into vat: the printed rate first, then standard ones.
func vatRate(amount, vat float64, given string) (float64, bool) {
	cands := []float64{0.1, 0.08, 0.05}
	if given != "" {
		cands = append([]float64{textutil.ParseRate(given)}, cands...)
	}
	for _, r := range cands {
		if exp := textutil.Round(amount*r, 0); abs(vat-exp) <= max(2, exp*0.002) {
			return r, true
		}
	}
	return 0, false
}

func isRate(s string) bool { return reRate.MatchString(s) }

func hasDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

var reUnit = regexp.MustCompile(`^\pL[\pL.]{0,7}[23]?(?:/\pL[\pL.]{0,7})?$`) // "Cái", "Kg", "m2", "Ly/cốc"

var (
	reDayMonth = regexp.MustCompile(`^\d{1,2}/\d{1,2}/\d{2,4}$`)
	reLot      = regexp.MustCompile(`^[A-Z]{0,4}\d[A-Z0-9/.\-]*$`)
	// "Cộng tiền hàng (5%): ...", "Tiền thuế GTGT (8%): ...": one line per rate
	reRatePart = regexp.MustCompile(`\(\s*\d+(?:[.,]\d+)?\s*%\s*\)`)
)

func isUnit(s string) bool { return reUnit.MatchString(s) && hasLetter(s) }

// glyphLines rebuilds lines from positioned glyphs; used when a PDF's text runs carry no
// usable row positions (the whole page collapses into one row).
func glyphLines(p pdf.Page) []string {
	type row struct {
		y  float64
		gs []pdf.Text
	}
	var rows []*row
	for _, g := range p.Content().Text {
		var r *row
		for _, x := range rows {
			if math.Abs(x.y-g.Y) <= math.Max(1.5, g.FontSize*0.3) {
				r = x
				break
			}
		}
		if r == nil {
			r = &row{y: g.Y}
			rows = append(rows, r)
		}
		r.gs = append(r.gs, g)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].y > rows[j].y })
	var out []string
	for _, r := range rows {
		sort.SliceStable(r.gs, func(i, j int) bool { return r.gs[i].X < r.gs[j].X })
		var b strings.Builder
		for i, g := range r.gs {
			if i > 0 {
				prev := r.gs[i-1]
				if g.X-(prev.X+prev.W) > math.Max(1, g.FontSize*0.2) {
					b.WriteByte(' ')
				}
			}
			b.WriteString(strings.ReplaceAll(g.S, "�", ""))
		}
		if s := textutil.Clean(b.String()); s != "" {
			out = append(out, s)
		}
	}
	return out
}
