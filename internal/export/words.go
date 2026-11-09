package export

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
