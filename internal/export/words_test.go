package export

import "testing"

func TestMoneyWords(t *testing.T) {
	cases := map[float64]string{
		78040000:   "Bảy mươi tám triệu không trăm bốn mươi nghìn đồng",
		1851407:    "Một triệu tám trăm năm mươi mốt nghìn bốn trăm linh bảy đồng",
		15:         "Mười lăm đồng",
		21:         "Hai mươi mốt đồng",
		1000000000: "Một tỷ đồng",
		2005:       "Hai nghìn không trăm linh năm đồng",
		0:          "Không đồng",
	}
	for v, want := range cases {
		if got := MoneyWords(v); got != want {
			t.Errorf("%v: got %q, want %q", v, got, want)
		}
	}
}
