package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhuvaoanh123/hoadon-kho/internal/einvoice"
	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/store"
)

func do(t *testing.T, h http.Handler, method, url string, body []byte, ctype string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:8765"+url, bytes.NewReader(body))
	req.Header.Set("X-App", AppID)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestUploadSaveExport(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "kho.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := New(st, dir, "test")

	xml, _ := os.ReadFile("../../testdata/hoadon-mau.xml")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("files", "hoadon.xml")
	fw.Write(xml)
	mw.Close()
	w := do(t, h, "POST", "/api/parse", buf.Bytes(), mw.FormDataContentType())
	if w.Code != 200 {
		t.Fatalf("parse: %d %s", w.Code, w.Body)
	}
	var drafts []Draft
	json.Unmarshal(w.Body.Bytes(), &drafts)
	if len(drafts) != 1 || drafts[0].Error != "" || len(drafts[0].Invoice.Lines) != 7 {
		t.Fatalf("drafts: %s", w.Body)
	}
	body, _ := json.Marshal(map[string]any{"token": drafts[0].Token, "invoice": drafts[0].Invoice})
	w = do(t, h, "POST", "/api/invoices", body, "application/json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PN00001") {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", "0100000003_1C25MAU_3344.xml")); err != nil {
		t.Errorf("original not kept: %v", err)
	}
	w = do(t, h, "POST", "/api/invoices", body, "application/json")
	if w.Code != 400 {
		t.Errorf("duplicate save allowed: %d", w.Code)
	}
	for _, k := range []string{"purchases", "receipts", "stock", "cards", "services", "import"} {
		w = do(t, h, "GET", "/api/export/"+k+"?from=2025-09-01&to=2025-09-30", nil, "")
		if w.Code != 200 || w.Body.Len() < 4000 || !strings.HasPrefix(w.Body.String(), "PK") {
			t.Errorf("export %s: %d len=%d", k, w.Code, w.Body.Len())
		}
	}
}

func TestRejectsCrossSite(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "kho.db"))
	defer st.Close()
	h := New(st, t.TempDir(), "test")
	req := httptest.NewRequest("POST", "http://127.0.0.1:8765/api/stockout", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("write without X-App: %d", w.Code)
	}
	req = httptest.NewRequest("GET", "http://evil.example/api/stock", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("foreign host: %d", w.Code)
	}
}

func upload(t *testing.T, h http.Handler, url string, files map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fw, _ := mw.CreateFormFile("files", name)
		fw.Write(data)
	}
	mw.Close()
	return do(t, h, "POST", url, buf.Bytes(), mw.FormDataContentType())
}

func TestBatchSavesOnlyCleanInvoices(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "kho.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	h := New(st, dir, "test")
	w := upload(t, h, "/api/batch", map[string]string{
		"a.xml":         "../../testdata/hoadon-mau.xml",
		"b-same.xml":    "../../testdata/hoadon-mau.xml",
		"wrapped.pdf":   "../../testdata/corpus/13_pdf_standard_layout_wrapped_names.pdf",
		"not-invoice.x": "../../testdata/corpus/11_xml_non_invoice_rejected.xml",
	})
	if w.Code != 200 {
		t.Fatalf("batch: %d %s", w.Code, w.Body)
	}
	var res struct {
		Saved  []Saved `json:"saved"`
		Drafts []Draft `json:"drafts"`
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Saved) != 1 || res.Saved[0].ReceiptNo == "" {
		t.Fatalf("want 1 saved with receipt: %s", w.Body)
	}
	var dup, flagged, failed int
	for _, d := range res.Drafts {
		switch {
		case d.Error != "":
			failed++
		case d.DuplicateOf > 0:
			dup++
		case !clean(d.Invoice):
			flagged++
		default:
			t.Errorf("clean invoice %s left for review", d.FileName)
		}
	}
	if dup != 1 || flagged != 1 || failed != 1 {
		t.Errorf("dup=%d flagged=%d failed=%d: %s", dup, flagged, failed, w.Body)
	}
}

func TestParsePastedText(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "kho.db"))
	defer st.Close()
	h := New(st, t.TempDir(), "test")
	pdf, _ := os.ReadFile("../../testdata/corpus/14_pdf_extra_columns_reordered.pdf")
	lines, err := einvoice.PDFLines(pdf)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"text": strings.Join(lines, "\r\n")})
	w := do(t, h, "POST", "/api/parse-text", body, "application/json")
	var drafts []Draft
	json.Unmarshal(w.Body.Bytes(), &drafts)
	if w.Code != 200 || len(drafts) != 1 || drafts[0].Invoice == nil || len(drafts[0].Invoice.Lines) == 0 {
		t.Fatalf("parse-text: %d %s", w.Code, w.Body)
	}
	inv := drafts[0].Invoice
	pasted := false
	for _, is := range inv.Issues {
		pasted = pasted || (!is.Block && strings.HasPrefix(is.Text, "Chữ dán vào"))
	}
	if inv.Source != model.SourceText || !pasted {
		t.Errorf("pasted text must carry a soft confirm issue: %+v", inv.Issues)
	}
	body, _ = json.Marshal(map[string]string{"text": "  \n "})
	w = do(t, h, "POST", "/api/parse-text", body, "application/json")
	if !strings.Contains(w.Body.String(), "error") {
		t.Errorf("empty paste: %s", w.Body)
	}
}
