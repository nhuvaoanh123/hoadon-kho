package einvoice

import (
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"github.com/klippa-app/go-pdfium/webassembly"

	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

// PDFium (the PDF engine of Chrome) runs as WebAssembly inside the exe: no DLL, no install.
var (
	pdfiumOnce sync.Once
	pdfiumPool pdfium.Pool
	pdfiumErr  error
	pdfiumMu   sync.Mutex
)

type textBox struct {
	text                     string
	left, right, top, bottom float64
}

// WarmUp loads the PDF engine in the background so the first PDF does not wait for it.
func WarmUp() {
	go func() { _, _ = pdfiumLines(nil) }()
}

// pdfiumLines extracts each page's text as visual lines, top to bottom.
func pdfiumLines(data []byte) ([]string, error) {
	pdfiumOnce.Do(func() {
		pdfiumPool, pdfiumErr = webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	})
	if pdfiumErr != nil || data == nil {
		return nil, pdfiumErr
	}
	pdfiumMu.Lock()
	defer pdfiumMu.Unlock()
	inst, err := pdfiumPool.GetInstance(2 * time.Minute)
	if err != nil {
		return nil, err
	}
	defer inst.Close()
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, err
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	pc, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, err
	}
	var out []string
	for i := 0; i < pc.PageCount && i < 50; i++ {
		res, err := inst.GetPageTextStructured(&requests.GetPageTextStructured{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i}},
			Mode: requests.GetPageTextStructuredModeBoth,
		})
		if err != nil {
			return nil, err
		}
		box := func(p responses.CharPosition, t string) textBox {
			return textBox{t, p.Left, p.Right, math.Min(p.Top, p.Bottom), math.Max(p.Top, p.Bottom)}
		}
		var runs, chars []textBox
		for _, r := range res.Rects {
			runs = append(runs, box(r.PointPosition, ""))
		}
		for _, c := range res.Chars {
			chars = append(chars, box(c.PointPosition, c.Text))
		}
		out = append(out, pageLines(runs, chars)...)
	}
	if len(strings.Join(out, "")) == 0 {
		return nil, errors.New("PDF không có chữ")
	}
	return out, nil
}

// pageLines uses PDFium's runs for layout and its characters for content: each character
// belongs to exactly one run, so kerned pairs ("Ta", "11") are neither doubled nor split.
func pageLines(runs, chars []textBox) []string {
	var spaces []textBox
	members := make([][]textBox, len(runs))
	for _, c := range chars {
		if strings.TrimSpace(c.text) == "" {
			spaces = append(spaces, c)
			continue
		}
		cx, cy := (c.left+c.right)/2, (c.top+c.bottom)/2
		best, bestD := -1, math.Inf(1)
		for i, r := range runs {
			if cx < r.left-0.5 || cx > r.right+0.5 || cy < r.top-1 || cy > r.bottom+1 {
				continue
			}
			if d := math.Abs(cy - (r.top+r.bottom)/2); d < bestD {
				best, bestD = i, d
			}
		}
		if best < 0 {
			runs = append(runs, c)
			members = append(members, nil)
			best = len(runs) - 1
		}
		members[best] = append(members[best], c)
	}
	var words []textBox
	for i, r := range runs {
		if len(members[i]) == 0 {
			continue
		}
		r.text = joinGlyphs(members[i], spaces)
		// Run rects can cover a trailing space; glyph bounds keep that space between words.
		r.left, r.right = members[i][0].left, members[i][0].right
		for _, g := range members[i] {
			r.left, r.right = math.Min(r.left, g.left), math.Max(r.right, g.right)
		}
		words = append(words, r)
	}
	return boxLines(words, spaces)
}

// spaced reports whether a space separates a and b: PDFium reported one between them, or the
// gap is as wide as a column gap.
func spaced(a, b textBox, spaces []textBox) bool {
	h := math.Max(a.bottom-a.top, b.bottom-b.top)
	if b.left-a.right > h*0.8 {
		return true
	}
	for _, sp := range spaces {
		x, y := (sp.left+sp.right)/2, (sp.top+sp.bottom)/2
		if x >= a.right-0.6 && x <= b.left+0.6 && y >= math.Min(a.top, b.top)-1 && y <= math.Max(a.bottom, b.bottom)+1 {
			return true
		}
	}
	return false
}

func joinGlyphs(gs, spaces []textBox) string {
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].left < gs[j].left })
	var b strings.Builder
	for i, g := range gs {
		if i > 0 && spaced(gs[i-1], g, spaces) {
			b.WriteByte(' ')
		}
		b.WriteString(g.text)
	}
	return b.String()
}

// boxLines joins runs whose vertical centres are close into one line (PDF y grows upwards).
func boxLines(bs, spaces []textBox) []string {
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].top+bs[i].bottom > bs[j].top+bs[j].bottom })
	var out []string
	var row []textBox
	flush := func() {
		if len(row) == 0 {
			return
		}
		sort.SliceStable(row, func(i, j int) bool { return row[i].left < row[j].left })
		var b strings.Builder
		for i, x := range row {
			if i > 0 && spaced(row[i-1], x, spaces) {
				b.WriteByte(' ')
			}
			b.WriteString(x.text)
		}
		if s := textutil.Clean(b.String()); s != "" {
			out = append(out, s)
		}
		row = nil
	}
	for _, x := range bs {
		if len(row) > 0 {
			f := row[0]
			c, fc := (x.top+x.bottom)/2, (f.top+f.bottom)/2
			if fc-c > math.Max(1.5, math.Min(x.bottom-x.top, f.bottom-f.top)*0.5) {
				flush()
			}
		}
		row = append(row, x)
	}
	flush()
	return out
}
