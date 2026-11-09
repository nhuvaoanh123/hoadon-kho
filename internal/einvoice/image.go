package einvoice

import (
	"html"
	"regexp"
	"strings"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/ocr"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

// ParseImage reads a photo/screenshot (or a scanned PDF) through Windows OCR.
func ParseImage(data []byte, ext string) (*model.Invoice, []string, error) {
	res, err := ocr.Run(data, ext)
	if err != nil {
		return nil, nil, err
	}
	inv, warns, err := ParseText(ocr.Lines(res.Words))
	if err != nil {
		return nil, nil, err
	}
	inv.Source = model.SourceOCR
	head := []string{"Đọc từ ảnh bằng OCR (" + res.Lang + "): BẮT BUỘC đối chiếu từng số với hóa đơn gốc. Dùng file XML sẽ chính xác tuyệt đối."}
	if !strings.HasPrefix(res.Lang, "vi") {
		head = append(head, "Windows chưa có OCR tiếng Việt nên tên hàng có thể mất dấu: thêm ngôn ngữ Tiếng Việt trong Cài đặt → Thời gian & ngôn ngữ → Ngôn ngữ.")
	}
	return inv, append(head, warns...), nil
}

var (
	reBlockEnd = regexp.MustCompile(`(?i)</(tr|p|div|h[1-6]|li|table)>|<br\s*/?>`)
	reCellEnd  = regexp.MustCompile(`(?i)</t[dh]>`)
	reDrop     = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]*>`)
)

// ParseHTML reads an invoice view saved as a web page.
func ParseHTML(data []byte) (*model.Invoice, []string, error) {
	s := reDrop.ReplaceAllString(string(data), " ")
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
	s = reBlockEnd.ReplaceAllString(s, "\n")
	s = reCellEnd.ReplaceAllString(s, " ")
	s = html.UnescapeString(reTag.ReplaceAllString(s, " "))
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = textutil.Clean(l); l != "" {
			lines = append(lines, l)
		}
	}
	inv, warns, err := ParseText(lines)
	if err != nil {
		return nil, nil, err
	}
	inv.Source = model.SourceHTML
	return inv, append([]string{"Đọc từ trang HTML: hãy kiểm tra trước khi lưu"}, warns...), nil
}

// DumpLines returns the text lines the parser sees for a PDF (text layer) or image (OCR).
func DumpLines(name string, data []byte) ([]string, error) {
	ext := sniff(name, data)
	if ext == ".pdf" {
		if lines, err := PDFLines(data); err == nil && len(strings.Join(lines, "")) >= 20 {
			return lines, nil
		}
	} else if !imageExts[ext] {
		return nil, ErrUnsupported
	}
	res, err := ocr.Run(data, ext)
	if err != nil {
		return nil, err
	}
	return ocr.Lines(res.Words), nil
}
