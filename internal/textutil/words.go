package textutil

import (
	"math"
	"strings"
)

var digits = []string{"không", "một", "hai", "ba", "bốn", "năm", "sáu", "bảy", "tám", "chín"}

// readTriple reads 0-999; full forces "không trăm" for inner groups.
func readTriple(n int, full bool) string {
	h, t, u := n/100, n/10%10, n%10
	var w []string
	if h > 0 || full {
		w = append(w, digits[h], "trăm")
	}
	switch {
	case t == 0 && u == 0:
	case t == 0:
		if h > 0 || full {
			w = append(w, "linh")
		}
		w = append(w, digits[u])
	case t == 1:
		w = append(w, "mười")
		switch u {
		case 0:
		case 5:
			w = append(w, "lăm")
		default:
			w = append(w, digits[u])
		}
	default:
		w = append(w, digits[t], "mươi")
		switch u {
		case 0:
		case 1:
			w = append(w, "mốt")
		case 5:
			w = append(w, "lăm")
		default:
			w = append(w, digits[u])
		}
	}
	return strings.Join(w, " ")
}

// MoneyWords spells a VND amount, e.g. 78040000 -> "Bảy mươi tám triệu không trăm bốn mươi nghìn đồng".
func MoneyWords(v float64) string {
	n := int64(math.Round(math.Abs(v)))
	if n == 0 {
		return "Không đồng"
	}
	scales := []string{"", "nghìn", "triệu", "tỷ", "nghìn tỷ", "triệu tỷ"}
	var groups []int
	for n > 0 {
		groups = append(groups, int(n%1000))
		n /= 1000
	}
	var parts []string
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		if g == 0 {
			continue
		}
		s := readTriple(g, i != len(groups)-1)
		if scales[i] != "" {
			s += " " + scales[i]
		}
		parts = append(parts, s)
	}
	out := strings.Join(parts, " ") + " đồng"
	if v < 0 {
		out = "âm " + out
	}
	r := []rune(out)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

var wordAlias = map[string]string{"ngan": "nghin", "le": "linh", "tu": "bon", "lam": "nam", "chan": "", "dong": "", "vnd": "", "va": ""}

// SameMoneyWords reports whether printed words (with or without diacritics) spell v.
func SameMoneyWords(printed string, v float64) bool {
	norm := func(s string) string {
		var out []string
		for _, w := range strings.Fields(Words(Fold(s))) {
			if a, ok := wordAlias[w]; ok {
				w = a
			}
			if w != "" {
				out = append(out, w)
			}
		}
		return strings.Join(out, " ")
	}
	p := norm(printed)
	return p != "" && p == norm(MoneyWords(v))
}

// ValidMST checks a Vietnamese tax code: 10 digits with a mod-11 check digit, optional "-NNN" branch.
func ValidMST(s string) bool {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	if i := strings.IndexByte(s, '-'); i >= 0 {
		branch := s[i+1:]
		if len(branch) != 3 || strings.Trim(branch, "0123456789") != "" {
			return false
		}
		s = s[:i]
	}
	if len(s) == 12 && strings.Trim(s, "0123456789") == "" && s[:3] >= "001" && s[:3] <= "096" {
		return true // số định danh cá nhân dùng làm MST (hộ, cá nhân kinh doanh): không có chữ số kiểm tra
	}
	if len(s) != 10 || strings.Trim(s, "0123456789") != "" {
		return false
	}
	weights := []int{31, 29, 23, 19, 17, 13, 7, 5, 3}
	sum := 0
	for i, w := range weights {
		sum += int(s[i]-'0') * w
	}
	check := 10 - sum%11
	return check < 10 && int(s[9]-'0') == check
}
