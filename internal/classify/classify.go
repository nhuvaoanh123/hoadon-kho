// Package classify decides whether an invoice line is goods (into stock)
// or a service (expense), and explains why.
package classify

import (
	"strings"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

// Từ khóa dịch vụ, khớp nguyên từ có dấu ("phí" không khớp "ống đồng phi 9.52").
var serviceKeywords = []string{
	"dịch vụ", "phí", "lệ phí", "cước", "vận chuyển", "vận tải", "giao hàng", "giao nhận",
	"bốc xếp", "bốc dỡ", "lắp đặt", "lắp ráp", "thi công", "sửa chữa", "bảo dưỡng", "bảo trì",
	"thuê", "cho thuê", "tư vấn", "đào tạo", "kiểm định", "hiệu chuẩn", "nhân công", "tiền công",
	"gia công", "thiết kế", "quảng cáo", "tiền điện", "tiền nước", "điện năng", "internet",
	"viễn thông", "ăn uống", "suất ăn", "tiếp khách", "lưu trú", "khách sạn", "phòng nghỉ",
	"vé máy bay", "lưu kho", "kho bãi", "hoa hồng", "môi giới", "bảo hiểm", "khảo sát",
	"giám sát", "nghiệm thu", "hosting", "tên miền", "gia hạn", "nạp gas", "vệ sinh",
	"chuyển phát", "kiểm toán", "dịch thuật", "in ấn", "lap dat", "sua chua", "van chuyen",
	"dich vu", "tien cong", "nhan cong", "bao duong",
}

// Cụm từ chứa từ khóa dịch vụ nhưng là hàng hóa.
var goodsPhrases = []string{
	"giấy vệ sinh", "thiết bị vệ sinh", "bồn vệ sinh", "nước vệ sinh", "dung dịch vệ sinh",
	"khăn vệ sinh",
}

var goodsNameHints = []string{"thiết bị", "máy", "vật tư", "linh kiện", "phụ tùng", "nguyên liệu", "vật liệu"}

var serviceUnits = set("lần", "gói dịch vụ", "tháng", "năm", "chuyến", "công", "ngày công", "giờ",
	"lượt", "đêm", "km", "kỳ", "khóa", "suất", "trọn gói", "hạng mục", "ca", "buổi", "dịch vụ",
	"dv", "hợp đồng", "hđ", "người", "ngày", "tuần", "quý", "phút", "tiết", "vé", "phòng")

var goodsUnits = set("cái", "chiếc", "bộ", "kg", "kilogam", "g", "gam", "gram", "tấn", "tạ", "yến",
	"m", "mét", "md", "m2", "m3", "cm", "mm", "lít", "l", "ml", "hộp", "thùng", "cuộn", "tấm", "cây",
	"bao", "túi", "chai", "lon", "đôi", "ram", "quyển", "cuốn", "tập", "viên", "ống", "can", "bình",
	"sợi", "thanh", "khối", "pcs", "pc", "set", "con", "vỉ", "lọ", "hũ", "két", "lốc", "kiện",
	"tuýp", "bánh", "cặp", "máy", "cục", "miếng", "bịch", "quả", "bó", "gói", "hòm", "xô", "chậu")

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Classifier classifies lines. Rule and KnownItem are optional lookups by item key.
type Classifier struct {
	Rule      func(key string) (model.Kind, bool)
	KnownItem func(key string) bool
}

// Classify returns the kind of l and a human-readable reason (Vietnamese).
func (c Classifier) Classify(l model.Line) (model.Kind, string) {
	switch l.Nature {
	case 3:
		return model.KindDiscount, "Dòng chiết khấu thương mại trên hóa đơn"
	case 4:
		return model.KindNote, "Dòng ghi chú/diễn giải, không có giá trị"
	}
	key := textutil.ItemKey(l.Name, l.Unit)
	if c.Rule != nil {
		if k, ok := c.Rule(key); ok {
			return k, "Theo lựa chọn bạn đã sửa trước đây"
		}
	}
	if c.KnownItem != nil && c.KnownItem(key) {
		return model.KindGoods, "Mặt hàng đã có trong kho"
	}
	if strings.TrimSpace(l.Name) == "" && l.Amount == 0 {
		return model.KindNote, "Dòng trống"
	}

	score := 0
	var why []string
	words := textutil.Words(l.Name)
	for _, p := range goodsPhrases {
		words = strings.ReplaceAll(words, " "+p+" ", " ")
	}
	plain := textutil.Fold(l.Name) == textutil.Lower(l.Name) // tên gõ không dấu
	for _, kw := range serviceKeywords {
		if kw == textutil.Fold(kw) && !plain {
			continue // từ khóa không dấu chỉ dùng cho tên không dấu
		}
		if strings.Contains(words, " "+kw+" ") {
			score += 3
			why = append(why, "tên có \""+kw+"\"")
			break
		}
	}
	trimmed := strings.TrimSpace(words)
	for _, h := range goodsNameHints {
		if trimmed == h || strings.HasPrefix(trimmed, h+" ") {
			score--
			why = append(why, "tên bắt đầu bằng \""+h+"\"")
			break
		}
	}
	unit := textutil.Lower(l.Unit)
	if textutil.Fold(unit) == unit { // ĐVT gõ không dấu: so sánh không dấu
		unit = foldMatch(unit)
	}
	switch {
	case unit == "":
		if l.Qty == 0 {
			score++
			why = append(why, "không có ĐVT và số lượng")
		}
	case goodsUnits[unit]:
		score -= 2
		why = append(why, "ĐVT \""+l.Unit+"\" là đơn vị hàng hóa")
	case serviceUnits[unit]:
		score += 2
		why = append(why, "ĐVT \""+l.Unit+"\" thường dùng cho dịch vụ")
	}
	if l.Nature == 2 {
		why = append(why, "hàng khuyến mại")
	}
	if score > 0 {
		return model.KindService, "Dịch vụ: " + strings.Join(why, ", ")
	}
	if len(why) == 0 {
		why = append(why, "mặc định khi không có dấu hiệu dịch vụ")
	}
	return model.KindGoods, "Hàng hóa: " + strings.Join(why, ", ")
}

// foldMatch maps an unaccented unit to its accented form, goods units first.
func foldMatch(u string) string {
	for _, m := range []map[string]bool{goodsUnits, serviceUnits} {
		for k := range m {
			if textutil.Fold(k) == u {
				return k
			}
		}
	}
	return u
}
