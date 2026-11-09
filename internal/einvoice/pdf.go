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
func ParsePDF(data []byte) (inv *model.Invoice, warns []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			inv, warns, err = nil, nil, fmt.Errorf("không đọc được PDF: %v", r)
		}
	}()
	lines, scrambled, err := readPDF(data)
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
	reSerial  = regexp.MustCompile(`(?i)k[ýy] hi[ệe]u[^:]*:\s*([1-6]?[CK][0-9]{2}[A-Z]{2,4})\b`)
	reNumber  = regexp.MustCompile(`(?i)\bs[ốo]\s*(?:h[óo]a đ[ơo]n)?\s*(?:\(\s*no\.?\s*\))?\s*:\s*0*([0-9]{1,8})\b`)
	reDate    = regexp.MustCompile(`(?i)ng[àa]y\D{0,20}?(\d{1,2})\s*th[áa]ng\D{0,20}?(\d{1,2})\s*n[ăa]m\D{0,20}?(\d{4})`)
	reTax     = regexp.MustCompile(`(?i)(?:m[ãa] s[ốo] thu[ếe]|\bmst\b)[^:0-9]{0,30}:?\s*([0-9][0-9 ]{8,13}[0-9](?:-[0-9]{3})?)`)
	reCQT     = regexp.MustCompile(`(?i)m[ãa] (?:c[ủu]a )?cqt[^:]*:\s*([0-9A-F]{20,40})`)
	reBuyer   = regexp.MustCompile(`(?i)(?:t[êe]n đ[ơo]n v[ịi]|đ[ơo]n v[ịi] mua h[àa]ng)[^:]*:\s*(.+)`)
	reNameEnd = regexp.MustCompile(`(?i)\s+(?:mst|m[ãa] s[ốo] thu[ếe]|tax code)\b.*$`)
	reDateISO = regexp.MustCompile(`(?i)ng[àa]y[^0-9]{0,20}(20\d{2})-(\d{1,2})-(\d{1,2})\b`)
	reDateDMY = regexp.MustCompile(`(?i)ng[àa]y[^0-9]{0,20}(\d{1,2})[/.-](\d{1,2})[/.-](20\d{2})\b`)
	reSeller  = regexp.MustCompile(`(?i)(?:đ[ơo]n v[ịi] b[áa]n h[àa]ng|ng[ưu][ờo]i b[áa]n)[^:]*:\s*(.+)`)
	reAddr    = regexp.MustCompile(`(?i)^đ[ịi]a ch[ỉi][^:]*:\s*(.+)`)
	reCompany = regexp.MustCompile(`^(CÔNG TY|CTY|DOANH NGHIỆP|HỘ KINH DOANH|CHI NHÁNH|TẬP ĐOÀN|HỢP TÁC XÃ)\b`)
	reSerial2 = regexp.MustCompile(`\b([1-6Il|])([CK]\d{2}[A-Z]{3})\b`)
	reNumber2 = regexp.MustCompile(`(?i)\(no\.?\)\s*[:.]?\s*0*([0-9]{1,8})\b`)
	reNumber3 = regexp.MustCompile(`\b0{2,}([1-9][0-9]{0,6})\b`)
	reDate2   = regexp.MustCompile(`(?i)\(date\)\D{0,15}?(\d{1,2})\D{1,25}?(\d{1,2})\D{1,25}?(\d{4})`)
	reDate3   = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(20\d{2})\b`)
	reTax2    = regexp.MustCompile(`(?i)tax code\)?\s*[:.]?\s*([0-9][0-9 ]{8,13}[0-9](?:-[0-9]{3})?)`)
	reRate    = regexp.MustCompile(`(?i)^(\d{1,2}(?:[.,]\d+)?%|KCT|KKKNT)$`)
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
	start, hasCode := 0, false
	for i, ln := range lines {
		l := textutil.Fold(ln)
		if strings.Contains(l, "stt") || strings.Contains(l, "ten hang") || strings.Contains(l, "name of goods") {
			start = i + 1
			hasCode = strings.Contains(l, "ma hang") || strings.Contains(l, "ma hh") || strings.Contains(l, "ma vt") || strings.Contains(l, "item code")
			break
		}
	}
	var pending []string // dòng chữ bị xuống dòng, chưa biết thuộc dòng hàng nào
	var heads []int      // số dòng tên nằm phía trên dòng số của từng mặt hàng
	inTable, newPage := true, false
	for _, ln := range lines[start:] {
		low := textutil.Fold(ln)
		if inTable && endOfTable(low, len(inv.Lines) > 0) {
			inTable = false
		}
		if !inTable {
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
		l, ok := itemRow(ln, hasCode)
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
		strings.HasPrefix(low, "(1) (2)") || strings.HasPrefix(low, "1 2 3 4 5") {
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

// statedLine picks printed totals and the amount in words from the block under the table.
func statedLine(ln, low string, st *model.Stated) {
	if i := strings.Index(low, "bang chu"); i >= 0 || strings.Contains(low, "in words") {
		if j := strings.LastIndex(ln, ":"); j >= 0 {
			st.Words = strings.TrimSpace(ln[j+1:])
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
	switch {
	case strings.HasPrefix(low, "tong cong") || strings.Contains(low, "thanh toan") || strings.Contains(low, "(total)"):
		if len(nums) >= 3 {
			st.BeforeVAT, st.VAT = nums[len(nums)-3], nums[len(nums)-2]
		}
		st.Total = last
	case strings.Contains(low, "tien thue"):
		st.VAT = last
	case strings.Contains(low, "tien hang") || strings.Contains(low, "chua co thue") || strings.Contains(low, "truoc thue"):
		st.BeforeVAT = last
	}
}

// itemRow parses "STT name [unit] qty price amount [rate] [vat]". Columns are
// found by arithmetic (qty × price = amount, vat = amount × rate), not position.
func itemRow(ln string, hasCode bool) (model.Line, bool) {
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
	qi, pi, ai := -1, -1, -1
	for k := len(nums) - 1; k >= 2 && ai < 0; k-- {
		for j := k - 1; j >= 1 && ai < 0; j-- {
			for i := j - 1; i >= 0; i-- {
				q, p, a := nums[i].v, nums[j].v, nums[k].v
				if q > 0 && p > 0 && a > 0 && abs(q*p-a) <= max(1, a*0.0005) {
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
		l.Notes = append(l.Notes, "không tìm được SL × đơn giá = thành tiền, cột số đoán theo vị trí")
		if len(prior) == 1 && l.Qty > 0 {
			l.Notes = append(l.Notes, "đơn giá không đọc được, tính lại = thành tiền / SL")
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
	if !vatSet {
		l.VATAmount = textutil.Round(l.Amount*textutil.ParseRate(l.VATRate), 0)
		if ai < len(nums)-1 {
			l.Notes = append(l.Notes, "số sau thành tiền không khớp tiền thuế của thuế suất nào")
		}
	}
	nameEnd := min(firstNum, end)
	if nameEnd > 1 && isUnit(f[nameEnd-1]) {
		l.Unit = f[nameEnd-1]
		nameEnd--
	}
	nameStart := 1
	if hasCode && nameEnd > 2 && hasDigit(f[1]) && !strings.ContainsAny(f[1], ".,") {
		l.Code, nameStart = f[1], 2
	}
	l.Name = strings.Join(f[nameStart:nameEnd], " ")
	if strings.Contains(textutil.Fold(l.Name), "chiet khau") {
		l.Nature = 3
	}
	return l, true
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

var reUnit = regexp.MustCompile(`^\pL[\pL.]{0,7}[23]?$`) // "Cái", "Kg", "m2", "M3"

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
