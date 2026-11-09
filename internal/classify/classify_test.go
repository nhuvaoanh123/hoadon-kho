package classify

import (
	"testing"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name, unit string
		qty        float64
		nature     int
		want       model.Kind
	}{
		{"Thiết bị bay hơi DD 26.2/140", "Cái", 1, 1, model.KindGoods},
		{"Quạt công nghiệp lạnh YWF4D-400S-E5L", "Cái", 15, 1, model.KindGoods},
		{"Gas lạnh R22", "Kg", 272, 1, model.KindGoods},
		{"Ống đồng phi 9.52", "m", 50, 1, model.KindGoods},
		{"Khớp nối bằng đồng hình chữ V 1 3/8", "Cái", 10, 1, model.KindGoods},
		{"Phí vận chuyển", "Chuyến", 1, 1, model.KindService},
		{"Cước vận chuyển hàng hóa", "", 0, 1, model.KindService},
		{"Dịch vụ lắp đặt điều hòa", "Bộ", 2, 1, model.KindService},
		{"Sửa chữa máy nén", "Lần", 1, 1, model.KindService},
		{"Tien cong lap dat", "cong", 3, 1, model.KindService},
		{"Giấy vệ sinh cuộn", "Cuộn", 20, 1, model.KindGoods},
		{"Thuê xe nâng", "Ngày", 2, 1, model.KindService},
		{"Chiết khấu thương mại", "", 0, 3, model.KindDiscount},
		{"Hàng tặng kèm: Lọc ẩm DML 163", "Cái", 1, 2, model.KindGoods},
	}
	var c Classifier
	for _, tc := range cases {
		got, why := c.Classify(model.Line{Name: tc.name, Unit: tc.unit, Qty: tc.qty, Nature: tc.nature})
		if got != tc.want {
			t.Errorf("%q (%s): got %s, want %s (%s)", tc.name, tc.unit, got, tc.want, why)
		}
	}
}

func TestRuleOverrides(t *testing.T) {
	c := Classifier{Rule: func(k string) (model.Kind, bool) {
		return model.KindService, k == "gas lạnh r22|kg"
	}}
	if k, _ := c.Classify(model.Line{Name: "Gas lạnh  R22", Unit: "Kg", Nature: 1}); k != model.KindService {
		t.Fatalf("rule not applied: %s", k)
	}
}
