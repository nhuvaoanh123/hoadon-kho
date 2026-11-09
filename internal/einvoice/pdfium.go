package einvoice

import (
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
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

// glyph is one character as PDFium reports it; idx is its place in the text stream.
type glyph struct {
	text        string
	idx, next   int // next: stream index of the following character
	left, right float64
	base, h     float64 // baseline y (PDF y grows upwards) and font height
	spaceAfter  bool    // the stream has a space or line break right after it
}

// WarmUp loads the PDF engine in the background so the first PDF does not wait for it.
func WarmUp() {
	go func() { _, _ = pdfiumPages(nil) }()
}

// pdfiumLines extracts the text of all pages as visual lines, top to bottom.
func pdfiumLines(data []byte) ([]string, error) {
	pages, err := pdfiumPages(data)
	var out []string
	for _, p := range pages {
		out = append(out, p...)
	}
	return out, err
}

// pdfiumPages extracts each page's text as visual lines, top to bottom.
func pdfiumPages(data []byte) ([][]string, error) {
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
	var out [][]string
	total := 0
	for i := 0; i < pc.PageCount && i < 50; i++ {
		gs, err := pageGlyphs(inst, doc.Document, i)
		if err != nil {
			return nil, err
		}
		lines := glyphRows(gs)
		for _, l := range lines {
			total += len(l)
		}
		out = append(out, lines)
	}
	if total == 0 {
		return nil, errors.New("PDF không có chữ")
	}
	return out, nil
}

// pageGlyphs reads every character of a page with its baseline and font box.
func pageGlyphs(inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, index int) ([]glyph, error) {
	pg, err := inst.FPDF_LoadPage(&requests.FPDF_LoadPage{Document: doc, Index: index})
	if err != nil {
		return nil, err
	}
	defer inst.FPDF_ClosePage(&requests.FPDF_ClosePage{Page: pg.Page})
	tp, err := inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: requests.Page{ByReference: &pg.Page}})
	if err != nil {
		return nil, err
	}
	defer inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: tp.TextPage})
	n, err := inst.FPDFText_CountChars(&requests.FPDFText_CountChars{TextPage: tp.TextPage})
	if err != nil {
		return nil, err
	}
	var gs []glyph
	for i := 0; i < n.Count; i++ {
		u, err := inst.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{TextPage: tp.TextPage, Index: i})
		if err != nil {
			return nil, err
		}
		r := rune(u.Unicode)
		if r == 0 || unicode.IsSpace(r) || unicode.IsControl(r) {
			if len(gs) > 0 {
				gs[len(gs)-1].spaceAfter = true
			}
			continue
		}
		b, err := inst.FPDFText_GetCharBox(&requests.FPDFText_GetCharBox{TextPage: tp.TextPage, Index: i})
		if err != nil {
			return nil, err
		}
		lb, err := inst.FPDFText_GetLooseCharBox(&requests.FPDFText_GetLooseCharBox{TextPage: tp.TextPage, Index: i})
		if err != nil {
			return nil, err
		}
		o, err := inst.FPDFText_GetCharOrigin(&requests.FPDFText_GetCharOrigin{TextPage: tp.TextPage, Index: i})
		if err != nil {
			return nil, err
		}
		// Hộp theo phông (bề rộng ký tự): chữ trong một từ chạm nhau, giữa hai từ có khe.
		g := glyph{text: string(r), idx: i, next: -1, left: float64(lb.Rect.Left), right: float64(lb.Rect.Right), base: o.Y,
			h: math.Abs(float64(lb.Rect.Top - lb.Rect.Bottom))}
		if g.right <= g.left {
			g.left, g.right = b.Left, b.Right
		}
		if g.h < 1 {
			g.h = math.Max(1, b.Top-b.Bottom)
		}
		if len(gs) > 0 {
			gs[len(gs)-1].next = i
		}
		gs = append(gs, g)
	}
	return gs, nil
}

// glyphRows groups characters by baseline into lines, top to bottom. Baselines, not glyph
// boxes, keep descenders and marks (g, p, ạ) on their own line.
func glyphRows(gs []glyph) []string {
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].base > gs[j].base })
	var out []string
	var row []glyph
	flush := func() {
		if len(row) > 0 {
			if s := textutil.Clean(joinRow(row)); s != "" {
				out = append(out, s)
			}
		}
		row = nil
	}
	for _, g := range gs {
		if len(row) > 0 {
			f := row[0]
			if f.base-g.base > math.Max(1, math.Min(f.h, g.h)*0.3) {
				flush()
			}
		}
		row = append(row, g)
	}
	flush()
	return out
}

// joinRow writes one line left to right; a glyph drawn twice at the same place (fake bold) counts once.
func joinRow(row []glyph) string {
	sort.SliceStable(row, func(i, j int) bool { return row[i].left < row[j].left })
	var b strings.Builder
	var prev *glyph
	for i := range row {
		g := &row[i]
		if prev != nil {
			if g.text == prev.text && math.Abs(g.left-prev.left) < (prev.right-prev.left)*0.3 {
				continue
			}
			if spacedGlyphs(*prev, *g) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(g.text)
		prev = g
	}
	return b.String()
}

// spacedGlyphs: a space PDFium put between stream neighbours, or a gap between the font boxes
// (some PDFs place each word without a space character).
func spacedGlyphs(a, b glyph) bool {
	switch {
	case b.left-a.right > math.Max(a.h, b.h)*0.06:
		return true
	case b.idx == a.next:
		return a.spaceAfter
	default:
		return a.idx == b.next && b.spaceAfter
	}
}
