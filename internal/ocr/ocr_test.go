package ocr

import "testing"

func TestFixToken(t *testing.T) {
	cases := map[string]string{
		"22.500-ooo,oo": "22.500.000,00",
		"11.700.OOO":    "11.700.000",
		"80/0":          "8%",
		"100/0":         "10%",
		"00/0":          "0%",
		".780.000,00":   "780.000,00",
		"Cåi":           "Cåi",
		"DML":           "DML",
		"l":             "l",
		"1.851.407":     "1.851.407",
	}
	for in, want := range cases {
		if got := FixToken(in); got != want {
			t.Errorf("FixToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLines(t *testing.T) {
	w := func(p int, s string, x, y int) Word { return Word{P: p, T: s, X: x, Y: y, W: 40, H: 20} }
	words := []Word{
		w(0, "1", 100, 102), w(0, "Gas", 20, 100), w(0, "lanh", 60, 101),
		w(0, ".000.000", 140, 103), w(0, "8", 300, 100), w(0, "%", 320, 100),
		w(0, "Tong", 20, 160),
		w(1, "Trang", 20, 10),
	}
	got := Lines(words)
	want := []string{"Gas lanh 1.000.000 8%", "Tong", "Trang"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
