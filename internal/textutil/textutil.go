// Package textutil normalises Vietnamese text and numbers.
package textutil

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var spaces = regexp.MustCompile(`\s+`)

// Clean converts to NFC, collapses whitespace and trims.
func Clean(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(norm.NFC.String(s), " "))
}

// Lower is Clean plus lower case.
func Lower(s string) string { return strings.ToLower(Clean(s)) }

// Fold lower-cases and strips Vietnamese diacritics ("Đồng" -> "dong").
func Fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, Lower(s))
	if err != nil {
		out = Lower(s)
	}
	return strings.NewReplacer("đ", "d", "Đ", "d").Replace(out)
}

// Words lower-cases s and replaces punctuation by spaces, padded with spaces
// so that " word " matches whole words only.
func Words(s string) string {
	b := []rune(Lower(s))
	for i, r := range b {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			b[i] = ' '
		}
	}
	return " " + spaces.ReplaceAllString(string(b), " ") + " "
}

// ItemKey identifies an inventory item by name and unit.
func ItemKey(name, unit string) string { return Lower(name) + "|" + Lower(unit) }

// ParseNumber parses a number written either in Vietnamese style
// ("22.500.000,00") or plain/English style ("22500000.00", "1,800.5").
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	if s == "" {
		return 0, false
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for _, r := range s {
		if !(r >= '0' && r <= '9') && r != '.' && r != ',' {
			return 0, false
		}
	}
	dots, commas := strings.Count(s, "."), strings.Count(s, ",")
	lastDot, lastComma := strings.LastIndex(s, "."), strings.LastIndex(s, ",")
	switch {
	case dots > 0 && commas > 0:
		if lastComma > lastDot { // 22.500.000,00
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else { // 22,500,000.00
			s = strings.ReplaceAll(s, ",", "")
		}
	case dots > 1:
		s = strings.ReplaceAll(s, ".", "")
	case dots == 1:
		if len(s)-lastDot-1 == 3 && lastDot > 0 { // 1.800 = một nghìn tám trăm
			s = strings.ReplaceAll(s, ".", "")
		}
	case commas > 1:
		s = strings.ReplaceAll(s, ",", "")
	case commas == 1: // dấu phẩy thập phân kiểu Việt Nam
		s = strings.Replace(s, ",", ".", 1)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || s == "." {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// ParseRate converts "8%", "10", "KCT", "KHAC:5.26%" into a fraction.
func ParseRate(s string) float64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	v, ok := ParseNumber(s)
	if !ok {
		return 0
	}
	if v > 1 {
		v /= 100
	}
	return v
}

// NormRate canonicalises a VAT rate label, e.g. "8" -> "8%", "kct" -> "KCT".
func NormRate(s string) string {
	u := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	switch {
	case u == "":
		return ""
	case u == "KCT" || u == "KKKNT" || strings.HasPrefix(u, "KHAC"):
		return u
	}
	v, ok := ParseNumber(strings.TrimSuffix(u, "%"))
	if !ok {
		return u
	}
	if v > 0 && v < 1 {
		v *= 100
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + "%"
}

// Round rounds to n decimals.
func Round(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
