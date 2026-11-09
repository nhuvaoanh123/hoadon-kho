package textutil

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

func TestSameMoneyWords(t *testing.T) {
	ok := map[string]float64{
		"Bảy mươi tám triệu không trăm bốn mươi nghìn đồng.":         78040000,
		"Bay muoi tam trieu khong tram bon muoi ngan dong chan":      78040000,
		"Một triệu tám trăm năm mươi mốt nghìn bốn trăm lẻ bảy đồng": 1851407,
	}
	for s, v := range ok {
		if !SameMoneyWords(s, v) {
			t.Errorf("%q should spell %v", s, v)
		}
	}
	if SameMoneyWords("Bảy mươi hai triệu đồng", 78040000) {
		t.Error("mismatch accepted")
	}
}

func TestValidMST(t *testing.T) {
	for _, s := range []string{"0100000003", "0200000007", "0100000003-001"} {
		if !ValidMST(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	for _, s := range []string{"0100000001", "010000000", "01000000031", "0100000003-1", "ABCDEFGHIJ"} {
		if ValidMST(s) {
			t.Errorf("%s should be invalid", s)
		}
	}
}
