// Package model holds the invoice and inventory types shared by all packages.
package model

// Kind is how a purchase line is booked.
type Kind string

const (
	KindGoods    Kind = "goods"    // hàng hóa: vào kho
	KindService  Kind = "service"  // dịch vụ: chi phí, không vào kho
	KindDiscount Kind = "discount" // chiết khấu thương mại (TChat=3)
	KindNote     Kind = "note"     // dòng ghi chú/diễn giải (TChat=4)
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	switch k {
	case KindGoods, KindService, KindDiscount, KindNote:
		return true
	}
	return false
}

// Party is a seller or buyer.
type Party struct {
	Name    string `json:"name"`
	TaxCode string `json:"taxCode"`
	Address string `json:"address"`
}

// Issue is something that could not be verified. Block issues must be fixed, others confirmed.
type Issue struct {
	Text  string `json:"text"`
	Block bool   `json:"block,omitempty"`
}

// Source formats, from most to least reliable.
const (
	SourceXML = "xml"
	SourcePDF = "pdf"
	// SourcePDFRaw is a PDF whose text order had to be rebuilt from glyphs (broken fonts).
	SourcePDFRaw = "pdf-raw"
	SourceHTML   = "html"
	SourceOCR    = "ocr"
	// SourceText is invoice text pasted by the user (email, chat, PDF copy).
	SourceText = "text"
)

// Stated holds totals printed on a non-XML invoice, used to cross-check the lines.
type Stated struct {
	BeforeVAT float64 `json:"beforeVat"`
	VAT       float64 `json:"vat"`
	Total     float64 `json:"total"`
	Words     string  `json:"words"`
	Rate      string  `json:"rate,omitempty"` // one VAT rate printed for the whole invoice
}

// Line is one row of the goods/services table of an invoice.
type Line struct {
	STT       int      `json:"stt"`
	Nature    int      `json:"nature"` // TChat: 1 HHDV, 2 khuyến mại, 3 chiết khấu, 4 ghi chú
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Unit      string   `json:"unit"`
	Qty       float64  `json:"qty"`
	Price     float64  `json:"price"`
	Amount    float64  `json:"amount"`
	VATRate   string   `json:"vatRate"`
	VATAmount float64  `json:"vatAmount"`
	Kind      Kind     `json:"kind"`
	Reason    string   `json:"reason"`
	UserSet   bool     `json:"userSet,omitempty"` // kind chosen by the user in the review screen
	Notes     []string `json:"notes,omitempty"`   // điều bộ đọc đã phải suy ra (PDF/OCR)
	Wrapped   bool     `json:"wrapped,omitempty"` // tên ghép từ nhiều dòng in
	Certain   bool     `json:"certain"`           // phân loại hàng hóa/dịch vụ chắc chắn
	Issues    []Issue  `json:"issues"`
	Confirmed bool     `json:"confirmed,omitempty"` // người dùng đã đối chiếu các vấn đề của dòng
	ItemID    int64    `json:"itemId,omitempty"`
	ItemCode  string   `json:"itemCode,omitempty"`
	Cost      float64  `json:"cost,omitempty"` // giá trị nhập kho (VND, sau chiết khấu)
}

// Invoice is a purchase (input) invoice.
type Invoice struct {
	ID             int64   `json:"id"`
	Form           string  `json:"form"`   // ký hiệu mẫu số (KHMSHDon), vd "1"
	Serial         string  `json:"serial"` // ký hiệu (KHHDon), vd "C25TTA"
	Number         string  `json:"number"`
	Date           string  `json:"date"` // YYYY-MM-DD
	Seller         Party   `json:"seller"`
	Buyer          Party   `json:"buyer"`
	Currency       string  `json:"currency"`
	ExRate         float64 `json:"exRate"`
	PayMethod      string  `json:"payMethod"`
	TaxAuthCode    string  `json:"taxAuthCode"` // mã của cơ quan thuế
	TotalBeforeVAT float64 `json:"totalBeforeVat"`
	TotalVAT       float64 `json:"totalVat"`
	Total          float64 `json:"total"`
	ReceiptNo      string  `json:"receiptNo,omitempty"` // số phiếu nhập kho
	SourceFile     string  `json:"sourceFile,omitempty"`
	CreatedAt      string  `json:"createdAt,omitempty"`
	Source         string  `json:"source"`
	Stated         Stated  `json:"stated"`
	Issues         []Issue `json:"issues"`
	Confirmed      bool    `json:"confirmed,omitempty"`
	Lines          []Line  `json:"lines"`
}

// SerialFull is the printed "Ký hiệu", e.g. "1C25TTA".
func (inv *Invoice) SerialFull() string { return inv.Form + inv.Serial }
