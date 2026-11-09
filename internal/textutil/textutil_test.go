package textutil

import "testing"

func TestClean(t *testing.T) {
	cases := map[string]string{
		"  a   b  ":        "a b",
		"a\u00A0b":         "a b",
		"a\u202Fb":         "a b",
		"a\u2003b":         "a b",
		"a\u200Bb":         "ab",
		"a\u200Cb":         "ab",
		"a\u200Db":         "ab",
		"a\uFEFFb":         "ab",
		"a\u2060b":         "ab",
		"a\u00ADb":         "ab",
		"  Máy lọc nước  ": "Máy lọc nước",
	}
	for in, want := range cases {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToneNew(t *testing.T) {
	cases := map[string]string{
		"hoà":   "hòa",
		"khoẻ":  "khỏe",
		"thuỷ":  "thủy",
		"hoá":   "hóa",
		"hòa":   "hòa",
		"khỏe":  "khỏe",
		"thủy":  "thủy",
		"hóa":   "hóa",
		"hoàn":  "hoàn",
		"xoắn":  "xoắn",
		"huyết": "huyết",
		"khuya": "khuya",
		"quý":   "quý",
		"quỳ":   "quỳ",
		"quỷ":   "quỷ",
	}
	for in, want := range cases {
		if got := ToneNew(in); got != want {
			t.Errorf("ToneNew(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestItemKeySameTone(t *testing.T) {
	pairs := [][2]string{
		{"hoà", "hòa"},
		{"khoẻ", "khỏe"},
		{"thuỷ", "thủy"},
		{"Hoá Chất", "hóa chất"},
		{"Máy lọc nước Hoà Phát", "máy lọc nước hòa phát"},
	}
	for _, p := range pairs {
		k1, k2 := ItemKey(p[0], "cái"), ItemKey(p[1], "cái")
		if k1 != k2 {
			t.Errorf("ItemKey(%q)=%q != ItemKey(%q)=%q", p[0], k1, p[1], k2)
		}
	}
}
