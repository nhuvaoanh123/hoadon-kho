// Package ocr turns images and scanned PDFs into text lines using the OCR engine
// built into Windows 10/11 (no extra install).
package ocr

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

// Word is one recognised word with its box, in pixels of the (scaled) page image.
type Word struct {
	P int    `json:"p"`
	T string `json:"t"`
	X int    `json:"x"`
	Y int    `json:"y"`
	W int    `json:"w"`
	H int    `json:"h"`
}

// Result is what the OCR script returns.
type Result struct {
	Lang  string   `json:"lang"`
	Langs []string `json:"langs"`
	Words []Word   `json:"words"`
}

// ErrUnavailable means OCR cannot run on this system.
var ErrUnavailable = errors.New("không có OCR trên hệ điều hành này: hãy dùng file XML hoặc PDF có chữ")

var (
	numLike  = regexp.MustCompile(`^[0-9oOlI.,\-—–:']+$`)
	rateLike = regexp.MustCompile(`^(\d{1,2}?)[0oO]/[0oO]$`) // "%" đọc thành "0/0": "80/0" = 8%, "100/0" = 10%
)

// FixToken repairs typical OCR confusions inside numeric cells ("22.500-ooo,oo" -> "22.500.000,00").
func FixToken(t string) string {
	if m := rateLike.FindStringSubmatch(t); m != nil {
		return m[1] + "%"
	}
	if !numLike.MatchString(t) || !strings.ContainsAny(t, "0123456789") {
		return t
	}
	t = strings.NewReplacer("o", "0", "O", "0", "l", "1", "I", "1", "—", ".", "–", ".", ":", ".", "'", "").Replace(t)
	t = strings.Trim(t, ".,-")
	if i := strings.Index(t, "-"); i > 0 {
		t = strings.ReplaceAll(t, "-", ".")
	}
	return t
}

// Lines groups words into visual lines (top to bottom, page by page) with repaired numbers.
func Lines(words []Word) []string {
	ws := append([]Word(nil), words...)
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].P != ws[j].P {
			return ws[i].P < ws[j].P
		}
		return ws[i].Y+ws[i].H/2 < ws[j].Y+ws[j].H/2
	})
	var out []string
	var row []Word
	flush := func() {
		if len(row) == 0 {
			return
		}
		sort.SliceStable(row, func(i, j int) bool { return row[i].X < row[j].X })
		var toks []string
		for _, w := range row {
			t := FixToken(w.T)
			n := len(toks)
			switch {
			case n > 0 && t == "%" && isDigits(toks[n-1]):
				toks[n-1] += "%"
			case n > 0 && (strings.HasPrefix(w.T, ".") || strings.HasPrefix(w.T, ",")) && isDigits(toks[n-1]) && len(w.T) > 1:
				toks[n-1] += FixToken("0" + w.T)[1:] // "1 .000.000" -> "1.000.000"
			default:
				toks = append(toks, t)
			}
		}
		out = append(out, strings.Join(toks, " "))
		row = nil
	}
	for _, w := range ws {
		if len(row) > 0 {
			last := row[0]
			c, lc := w.Y+w.H/2, last.Y+last.H/2
			if w.P != last.P || c-lc > max(4, min(w.H, last.H)/2) {
				flush()
			}
		}
		row = append(row, w)
	}
	flush()
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
