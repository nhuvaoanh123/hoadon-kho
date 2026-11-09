package einvoice

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
)

// Parsed is the result for one invoice document (a zip yields several).
type Parsed struct {
	Name     string
	Ext      string // ".xml" or ".pdf": the format Data is in
	Data     []byte
	Invoice  *model.Invoice
	Warnings []string
	Err      error
}

// ErrUnsupported is returned for file types the app cannot read.
var ErrUnsupported = errors.New("định dạng không hỗ trợ: dùng XML, PDF, ZIP, HTML hoặc ảnh (PNG, JPG...)")

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".jfif": true, ".bmp": true,
	".gif": true, ".tif": true, ".tiff": true, ".webp": true}

// ParseAny reads a document by its content type (the extension is only a fallback).
func ParseAny(name string, data []byte) []Parsed {
	ext := sniff(name, data)
	if ext == ".zip" {
		return parseZip(name, data)
	}
	p := Parsed{Name: name, Ext: ext, Data: data}
	switch {
	case ext == ".xml":
		p.Invoice, p.Warnings, p.Err = ParseXML(data)
	case ext == ".pdf":
		p.Invoice, p.Warnings, p.Err = ParsePDF(data)
	case ext == ".html":
		p.Invoice, p.Warnings, p.Err = ParseHTML(data)
	case imageExts[ext]:
		p.Invoice, p.Warnings, p.Err = ParseImage(data, ext)
	default:
		p.Err = ErrUnsupported
	}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	return []Parsed{p}
}

func sniff(name string, data []byte) string {
	head := bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), " \t\r\n")
	low := bytes.ToLower(head[:min(len(head), 512)])
	switch {
	case bytes.HasPrefix(head, []byte("%PDF")):
		return ".pdf"
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return ".zip"
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		return ".png"
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return ".jpg"
	case bytes.HasPrefix(data, []byte("GIF8")):
		return ".gif"
	case bytes.HasPrefix(data, []byte("II*\x00")) || bytes.HasPrefix(data, []byte("MM\x00*")):
		return ".tif"
	case len(data) > 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WEBP":
		return ".webp"
	case bytes.Contains(low, []byte("<html")) || bytes.HasPrefix(low, []byte("<!doctype html")):
		return ".html"
	case bytes.HasPrefix(head, []byte("<")):
		return ".xml"
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".htm" {
		ext = ".html"
	}
	return ext
}

// parseZip reads every XML in a zip; PDFs are used only when it holds no invoice XML.
func parseZip(name string, data []byte) []Parsed {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return []Parsed{{Name: name, Err: errors.New("file ZIP lỗi: " + err.Error()), Warnings: []string{}}}
	}
	var xmls, pdfs []Parsed
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			continue
		}
		for _, p := range ParseAny(name+" › "+f.Name, b) {
			switch {
			case p.Ext == ".xml" && !errors.Is(p.Err, ErrNotInvoiceXML):
				xmls = append(xmls, p)
			case p.Ext != ".xml" && !errors.Is(p.Err, ErrUnsupported):
				pdfs = append(pdfs, p)
			}
		}
	}
	if len(xmls) > 0 {
		return xmls
	}
	if len(pdfs) > 0 {
		return pdfs
	}
	return []Parsed{{Name: name, Err: errors.New("trong file ZIP không có hóa đơn XML/PDF"), Warnings: []string{}}}
}
