package einvoice

import (
	"os"
	"strings"
	"testing"
)

func TestParseXML(t *testing.T) {
	data, err := os.ReadFile("../../testdata/hoadon-mau.xml")
	if err != nil {
		t.Fatal(err)
	}
	inv, warns, err := ParseXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings: %v", warns)
	}
	if inv.SerialFull() != "1C25MAU" || inv.Number != "3344" || inv.Date != "2025-09-23" {
		t.Errorf("header: %+v", inv)
	}
	if inv.Seller.TaxCode != "0100000003" || inv.Buyer.TaxCode != "0200000007" {
		t.Errorf("tax codes: %q %q", inv.Seller.TaxCode, inv.Buyer.TaxCode)
	}
	if len(inv.Lines) != 7 {
		t.Fatalf("lines = %d", len(inv.Lines))
	}
	g := inv.Lines[6]
	if g.Name != "Gas lạnh R22" || g.Qty != 272 || g.Amount != 23142593 || g.VATRate != "8%" || g.VATAmount != 1851407 {
		t.Errorf("line 7: %+v", g)
	}
	if inv.TotalBeforeVAT != 72242593 || inv.TotalVAT != 5797407 || inv.Total != 78040000 {
		t.Errorf("totals: %v %v %v", inv.TotalBeforeVAT, inv.TotalVAT, inv.Total)
	}
}

func TestParseXMLRejectsOther(t *testing.T) {
	if _, _, err := ParseXML([]byte(`<?xml version="1.0"?><root/>`)); err != ErrNotInvoiceXML {
		t.Fatalf("err = %v", err)
	}
}

// Text as a PDF renderer emits it: wrapped names land on their own rows.
var pdfLines = []string{
	"HÓA ĐƠN GIÁ TRỊ GIA TĂNG Ký hiệu (Serial): 1C25MAU",
	"(VAT INVOICE) Số (No.): 00003344",
	"Ngày (Date) 23 tháng (month) 09 năm (year) 2025",
	"Mã CQT (Code): 00A1B2C3D4E5F60718293A4B5C6D7E8F90",
	"CÔNG TY TNHH ĐIỆN LẠNH MẪU",
	"Mã số thuế (Tax code): 0100000003",
	"Địa chỉ (Address): Số 1 phố Mẫu, Hà Nội",
	"Họ tên người mua hàng (Buyer):",
	"Tên đơn vị (Company's name): CÔNG TY TNHH NGƯỜI MUA MẪU",
	"Mã số thuế (Tax code): 0200000007",
	"Địa chỉ (Address): Số 2 đường Mẫu, Hải Phòng",
	"STT Tên hàng hóa, dịch vụ Đơn vị tính Số lượng Đơn giá Thành tiền Thuế suất Tiền thuế",
	"(No) (Name of goods and services) (Unit) (Quantity) (Unit price) (Amount) (VAT rate) (VAT Amount)",
	"1 Thiết bị bay hơi DD 26.2/140 Cái 1,00 22.500.000,00 22.500.000 8% 1.800.000",
	"Quạt công nghiệp lạnh YWF4D-",
	"2 Cái 15,00 780.000,00 11.700.000 8% 936.000",
	"400S-E5L",
	"3 Lọc ẩm DML 163 Cái 40,00 170.000,00 6.800.000 8% 544.000",
	"4 Lọc ẩm DML 164 Cái 40,00 180.000,00 7.200.000 8% 576.000",
	"Ống nối bằng đồng chữ S 1.5/8-",
	"5 Cái 10,00 50.000,00 500.000 10% 50.000",
	"1.3/8",
	"6 Khớp nối bằng đồng hình chữ V 1 Cái 10,00 40.000,00 400.000 10% 40.000",
	"3/8",
	"7 Gas lạnh R22 Kg 272,00 85.083,06 23.142.593 8% 1.851.407",
	"Tổng hợp Thành tiền trước thuế GTGT Tiền thuế GTGT Cộng tiền thanh toán",
	"Tổng tiền chịu thuế suất 8%: 71.342.593 5.707.407 77.050.000",
	"Tổng cộng (Total): 72.242.593 5.797.407 78.040.000",
}

func TestParseText(t *testing.T) {
	inv, warns, err := ParseText(pdfLines)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warns {
		t.Log("warn:", w)
	}
	if inv.SerialFull() != "1C25MAU" || inv.Number != "3344" || inv.Date != "2025-09-23" {
		t.Errorf("header: form=%q serial=%q no=%q date=%q", inv.Form, inv.Serial, inv.Number, inv.Date)
	}
	if inv.Seller.Name != "CÔNG TY TNHH ĐIỆN LẠNH MẪU" || inv.Seller.TaxCode != "0100000003" {
		t.Errorf("seller: %+v", inv.Seller)
	}
	if inv.Buyer.Name != "CÔNG TY TNHH NGƯỜI MUA MẪU" || inv.Buyer.TaxCode != "0200000007" {
		t.Errorf("buyer: %+v", inv.Buyer)
	}
	want := []string{
		"Thiết bị bay hơi DD 26.2/140",
		"Quạt công nghiệp lạnh YWF4D-400S-E5L",
		"Lọc ẩm DML 163",
		"Lọc ẩm DML 164",
		"Ống nối bằng đồng chữ S 1.5/8-1.3/8",
		"Khớp nối bằng đồng hình chữ V 1 3/8",
		"Gas lạnh R22",
	}
	if len(inv.Lines) != len(want) {
		t.Fatalf("lines = %d: %+v", len(inv.Lines), inv.Lines)
	}
	for i, w := range want {
		if inv.Lines[i].Name != w {
			t.Errorf("line %d name %q, want %q", i+1, inv.Lines[i].Name, w)
		}
	}
	if l := inv.Lines[6]; l.Qty != 272 || l.Price != 85083.06 || l.Unit != "Kg" {
		t.Errorf("gas line: %+v", l)
	}
	if inv.Total != 78040000 || inv.TotalVAT != 5797407 {
		t.Errorf("totals: %v %v", inv.Total, inv.TotalVAT)
	}
	for _, w := range warns {
		if strings.HasPrefix(w, "Tổng thanh toán") {
			t.Errorf("unexpected total warning: %s", w)
		}
	}
}

// Lines as extracted from a Chrome-printed PDF: header cell wraps, names wrap around the number row.
var chromeLines = []string{
	"HÓA ĐƠN GIÁ TRỊ GIA TĂNG Ký hiệu (Serial): 1C25MAU",
	"(VAT INVOICE) Số (No.): 00003345",
	"Ngày (Date) 24 tháng (month) 09 năm (year) 2025",
	"CÔNG TY TNHH ĐIỆN LẠNH MẪU",
	"Mã số thuế (Tax code): 0100000003",
	"Địa chỉ (Address): Số 1 phố Mẫu, Hà Nội",
	"Tên đơn vị (Company's name): CÔNG TY TNHH NGƯỜI MUA MẪU",
	"Mã số thuế (Tax code): 0200000007",
	"Địa chỉ (Address): Số 2 đường Mẫu, Hải Phòng",
	"Tên hàng hóa, dịch",
	"STT Đơn vị tính Số lượng Đơn giá Thành tiền Thuế suất Tiền thuế",
	"vụ",
	"Thiết bị bay hơi DD",
	"1 Cái 1,00 22.500.000,00 22.500.000 8% 1.800.000",
	"26.2/140",
	"Quạt công nghiệp lạnh",
	"2 Cái 15,00 780.000,00 11.700.000 8% 936.000",
	"YWF4D-400S-E5L",
	"3 Lọc ẩm DML 163 Cái 40,00 170.000,00 6.800.000 8% 544.000",
	"4 Lọc ẩm DML 164 Cái 40,00 180.000,00 7.200.000 8% 576.000",
	"Ống nối bằng đồng",
	"5 Cái 10,00 50.000,00 500.000 10% 50.000",
	"chữ S 1.5/8-1.3/8",
	"Khớp nối bằng đồng",
	"6 Cái 10,00 40.000,00 400.000 10% 40.000",
	"hình chữ V 1 3/8",
	"7 Gas lạnh R22 Kg 272,00 85.083,06 23.142.593 8% 1.851.407",
	"Phí vận chuyển và lắp",
	"8 Lần 1,00 1.000.000,00 1.000.000 8% 80.000",
	"đặt",
	"Tổng cộng (Total): 73.242.593 5.877.407 79.120.000",
}

func TestParseChromePDFText(t *testing.T) {
	inv, _, err := ParseText(chromeLines)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Lines) != 8 || inv.Lines[0].Name != "Thiết bị bay hơi DD 26.2/140" || inv.Lines[7].Name != "Phí vận chuyển và lắp đặt" {
		t.Fatalf("lines: %+v", inv.Lines)
	}
	if inv.Lines[1].Amount != 11700000 || inv.Total != 79120000 || inv.Number != "3345" {
		t.Errorf("amounts: %v %v %q", inv.Lines[1].Amount, inv.Total, inv.Number)
	}
}
