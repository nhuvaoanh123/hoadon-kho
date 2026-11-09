// Package server is the local web UI and JSON API (bound to 127.0.0.1 only).
package server

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/nhuvaoanh123/hoadon-kho/internal/classify"
	"github.com/nhuvaoanh123/hoadon-kho/internal/einvoice"
	"github.com/nhuvaoanh123/hoadon-kho/internal/export"
	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/ocr"
	"github.com/nhuvaoanh123/hoadon-kho/internal/store"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

//go:embed web
var webFS embed.FS

// AppID is returned by /api/ping so a second launch can detect a running instance.
const AppID = "hoadon-kho"

// Server serves the UI and API.
type Server struct {
	st      *store.Store
	dataDir string
	version string
	mux     *http.ServeMux
}

// New builds the handler. dataDir holds inbox/ (uploads pending review) and files/ (originals).
func New(st *store.Store, dataDir, version string) http.Handler {
	s := &Server{st: st, dataDir: dataDir, version: version, mux: http.NewServeMux()}
	for _, d := range []string{"inbox", "files"} {
		_ = os.MkdirAll(filepath.Join(dataDir, d), 0o755)
	}
	s.cleanInbox()
	web, _ := fs.Sub(webFS, "web")
	s.mux.Handle("GET /", http.FileServer(http.FS(web)))
	s.mux.HandleFunc("GET /api/ping", s.ping)
	s.mux.HandleFunc("POST /api/parse", s.parse)
	s.mux.HandleFunc("POST /api/parse-text", s.parseText)
	s.mux.HandleFunc("POST /api/batch", s.batch)
	s.mux.HandleFunc("POST /api/invoices", s.saveInvoice)
	s.mux.HandleFunc("POST /api/check", s.checkInvoice)
	s.mux.HandleFunc("GET /api/ocr", s.ocrStatus)
	s.mux.HandleFunc("POST /api/ocr/install-vi", s.ocrInstall)
	s.mux.HandleFunc("GET /api/invoices", s.listInvoices)
	s.mux.HandleFunc("GET /api/invoices/{id}", s.getInvoice)
	s.mux.HandleFunc("GET /api/invoices/{id}/file", s.invoiceFile)
	s.mux.HandleFunc("DELETE /api/invoices/{id}", s.deleteInvoice)
	s.mux.HandleFunc("GET /api/stock", s.stock)
	s.mux.HandleFunc("GET /api/items/{id}/ledger", s.ledger)
	s.mux.HandleFunc("POST /api/stockout", s.stockOut)
	s.mux.HandleFunc("DELETE /api/movements/{id}", s.deleteMovement)
	s.mux.HandleFunc("GET /api/settings", s.getSettings)
	s.mux.HandleFunc("PUT /api/settings", s.putSettings)
	s.mux.HandleFunc("GET /api/export/{kind}", s.export)
	return s
}

// ServeHTTP rejects foreign hosts (DNS rebinding) and cross-site writes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if host != "127.0.0.1" && host != "localhost" {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("X-App") != AppID {
		http.Error(w, "missing X-App header", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (s *Server) ping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"app": AppID, "version": s.version})
}

// Draft is a parsed upload awaiting the user's review.
type Draft struct {
	Token       string         `json:"token"`
	FileName    string         `json:"fileName"`
	Invoice     *model.Invoice `json:"invoice,omitempty"`
	Warnings    []string       `json:"warnings"`
	DuplicateOf int64          `json:"duplicateOf"`
	Error       string         `json:"error,omitempty"`
}

func (s *Server) parse(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.parseUpload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, drafts)
}

// parseUpload parses every file of a multipart "files" upload.
func (s *Server) parseUpload(r *http.Request) ([]Draft, error) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return nil, fmt.Errorf("không đọc được file tải lên: %w", err)
	}
	defer r.MultipartForm.RemoveAll()
	var drafts []Draft
	for _, fh := range r.MultipartForm.File["files"] {
		f, err := fh.Open()
		if err != nil {
			drafts = append(drafts, Draft{FileName: fh.Filename, Error: err.Error()})
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, 64<<20))
		f.Close()
		if err != nil {
			drafts = append(drafts, Draft{FileName: fh.Filename, Error: err.Error()})
			continue
		}
		drafts = append(drafts, s.parseFile(fh.Filename, data)...)
	}
	return drafts, nil
}

// Saved is an invoice the batch mode stored without review.
type Saved struct {
	FileName  string  `json:"fileName"`
	ID        int64   `json:"id"`
	ReceiptNo string  `json:"receiptNo"`
	Number    string  `json:"number"`
	Date      string  `json:"date"`
	Seller    string  `json:"seller"`
	Total     float64 `json:"total"`
}

// batch saves every invoice that has no issue at all and returns the rest for review.
func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	drafts, err := s.parseUpload(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// XML trước: cùng hóa đơn có cả XML và PDF thì lưu bản XML, bản PDF thành trùng.
	sort.SliceStable(drafts, func(i, j int) bool {
		return drafts[i].Invoice != nil && drafts[i].Invoice.Source == model.SourceXML &&
			(drafts[j].Invoice == nil || drafts[j].Invoice.Source != model.SourceXML)
	})
	out := struct {
		Saved  []Saved `json:"saved"`
		Drafts []Draft `json:"drafts"`
	}{Saved: []Saved{}, Drafts: []Draft{}}
	for _, d := range drafts {
		if d.Invoice != nil {
			d.DuplicateOf = s.st.FindDuplicate(d.Invoice)
		}
		if d.Error != "" || d.Invoice == nil || d.DuplicateOf > 0 || !clean(d.Invoice) {
			out.Drafts = append(out.Drafts, d)
			continue
		}
		inv := *d.Invoice
		id, err := s.store(d.Token, &inv)
		if err != nil {
			d.Warnings = append(d.Warnings, "Chưa tự lưu được: "+err.Error())
			out.Drafts = append(out.Drafts, d)
			continue
		}
		out.Saved = append(out.Saved, Saved{FileName: d.FileName, ID: id, ReceiptNo: inv.ReceiptNo, Number: inv.Number,
			Date: inv.Date, Seller: inv.Seller.Name, Total: inv.Total * inv.ExRate})
	}
	writeJSON(w, out)
}

// clean reports an invoice with no issue at all, so nothing needs the user's confirmation.
func clean(inv *model.Invoice) bool {
	if len(inv.Issues) > 0 {
		return false
	}
	for _, l := range inv.Lines {
		if len(l.Issues) > 0 {
			return false
		}
	}
	return true
}

// parseText reads invoice text pasted into the page.
func (s *Server) parseText(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	d := Draft{FileName: "Chữ dán vào", Warnings: []string{}}
	inv, warns, err := einvoice.ParsePasted(req.Text)
	if err != nil {
		d.Error = err.Error()
		writeJSON(w, []Draft{d})
		return
	}
	s.classify(inv)
	s.verify(inv)
	d.Invoice, d.Warnings = inv, append(d.Warnings, warns...)
	d.DuplicateOf = s.st.FindDuplicate(inv)
	writeJSON(w, []Draft{d})
}

func (s *Server) classify(inv *model.Invoice) {
	c := classify.Classifier{Rule: s.st.RuleLookup, KnownItem: s.st.KnownItem}
	for i := range inv.Lines {
		l := &inv.Lines[i]
		l.Kind, l.Reason, l.Certain = c.Classify(*l)
	}
}

func (s *Server) parseFile(name string, data []byte) []Draft {
	var out []Draft
	for _, p := range einvoice.ParseAny(name, data) {
		d := Draft{FileName: p.Name, Warnings: p.Warnings}
		if p.Err != nil {
			d.Error = p.Err.Error()
			out = append(out, d)
			continue
		}
		d.Token = newToken() + p.Ext
		if err := os.WriteFile(filepath.Join(s.dataDir, "inbox", d.Token), p.Data, 0o644); err != nil {
			d.Error = err.Error()
			out = append(out, d)
			continue
		}
		s.classify(p.Invoice)
		s.verify(p.Invoice)
		d.Invoice = p.Invoice
		d.DuplicateOf = s.st.FindDuplicate(p.Invoice)
		out = append(out, d)
	}
	return out
}

func newToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var tokenRe = regexp.MustCompile(`^[0-9a-f]{24}\.(xml|pdf)$`)

func (s *Server) cleanInbox() {
	dir := filepath.Join(s.dataDir, "inbox")
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

var unsafeChars = regexp.MustCompile(`[^0-9A-Za-z_.-]+`)

func (s *Server) saveInvoice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token   string        `json:"token"`
		Invoice model.Invoice `json:"invoice"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := s.store(req.Token, &req.Invoice)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]any{"id": id, "receiptNo": req.Invoice.ReceiptNo})
}

// store re-verifies an invoice, saves it when nothing is unresolved and files the original upload.
func (s *Server) store(token string, inv *model.Invoice) (int64, error) {
	inv.ID, inv.ReceiptNo = 0, ""
	inv.Seller.TaxCode = strings.ReplaceAll(strings.TrimSpace(inv.Seller.TaxCode), " ", "")
	for i := range inv.Lines {
		l := &inv.Lines[i]
		l.VATRate = textutil.NormRate(l.VATRate)
		l.Name, l.Unit = textutil.Clean(l.Name), textutil.Clean(l.Unit)
	}
	recomputeTotals(inv)
	s.reverify(inv)
	if probs := einvoice.Problems(inv); len(probs) > 0 {
		return 0, errors.New("Chưa lưu được: " + strings.Join(probs, "; "))
	}
	var src string
	if tokenRe.MatchString(token) {
		src = filepath.Join(s.dataDir, "inbox", token)
		inv.SourceFile = unsafeChars.ReplaceAllString(fmt.Sprintf("%s_%s%s_%s", inv.Seller.TaxCode, inv.Form, inv.Serial, inv.Number), "-") + filepath.Ext(token)
	}
	id, err := s.st.SaveInvoice(inv)
	if err != nil {
		return 0, err
	}
	if src != "" {
		if err := os.Rename(src, filepath.Join(s.dataDir, "files", inv.SourceFile)); err != nil {
			inv.SourceFile = ""
		}
	}
	return id, nil
}

func (s *Server) ocrStatus(w http.ResponseWriter, _ *http.Request) {
	langs, err := ocr.Languages()
	vi := false
	for _, l := range langs {
		vi = vi || strings.HasPrefix(l, "vi")
	}
	out := map[string]any{"langs": langs, "vietnamese": vi}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, out)
}

func (s *Server) ocrInstall(w http.ResponseWriter, _ *http.Request) {
	if err := ocr.InstallVietnamese(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// verify runs all checks on a freshly parsed invoice.
func (s *Server) verify(inv *model.Invoice) {
	einvoice.VerifyKnown(inv, s.st.KnownItem)
	if set, err := s.st.Settings(); err == nil {
		einvoice.CheckBuyer(inv, set["company_tax"])
	}
}

// reverify re-checks an invoice edited in the browser: classification certainty is
// recomputed here, never trusted from the client.
func (s *Server) reverify(inv *model.Invoice) {
	c := classify.Classifier{Rule: s.st.RuleLookup, KnownItem: s.st.KnownItem}
	for i := range inv.Lines {
		l := &inv.Lines[i]
		k, _, certain := c.Classify(*l)
		l.Certain = l.UserSet || (certain && k == l.Kind)
	}
	if inv.Source == "" {
		inv.Source = model.SourceOCR // nguồn không rõ: kiểm tra chặt nhất
	}
	s.verify(inv)
}

// checkInvoice re-verifies the review screen's current state and returns issues and problems.
func (s *Server) checkInvoice(w http.ResponseWriter, r *http.Request) {
	var inv model.Invoice
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&inv); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.reverify(&inv)
	lines := make([]map[string]any, len(inv.Lines))
	for i, l := range inv.Lines {
		lines[i] = map[string]any{"issues": l.Issues, "certain": l.Certain}
	}
	writeJSON(w, map[string]any{"issues": inv.Issues, "lines": lines, "problems": einvoice.Problems(&inv)})
}

// recomputeTotals keeps stated totals unless the edited lines no longer match them.
func recomputeTotals(inv *model.Invoice) {
	var b, v float64
	for _, l := range inv.Lines {
		switch l.Kind {
		case model.KindNote:
		case model.KindDiscount:
			b -= abs(l.Amount)
			v -= abs(l.VATAmount)
		default:
			b += l.Amount
			v += l.VATAmount
		}
	}
	tol := float64(len(inv.Lines)) + 1
	if abs(inv.TotalBeforeVAT-b) > tol || abs(inv.TotalVAT-v) > tol {
		inv.TotalBeforeVAT, inv.TotalVAT = textutil.Round(b, 2), textutil.Round(v, 2)
		inv.Total = inv.TotalBeforeVAT + inv.TotalVAT
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func (s *Server) listInvoices(w http.ResponseWriter, r *http.Request) {
	invs, err := s.st.Invoices(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if invs == nil {
		invs = []*model.Invoice{}
	}
	writeJSON(w, invs)
}

func (s *Server) getInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := s.st.Invoice(pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("không tìm thấy hóa đơn"))
		return
	}
	writeJSON(w, inv)
}

func (s *Server) invoiceFile(w http.ResponseWriter, r *http.Request) {
	inv, err := s.st.Invoice(pathID(r))
	if err != nil || inv.SourceFile == "" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.dataDir, "files", filepath.Base(inv.SourceFile)))
}

func (s *Server) deleteInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := s.st.Invoice(pathID(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, errors.New("không tìm thấy hóa đơn"))
		return
	}
	if err := s.st.DeleteInvoice(inv.ID); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	if inv.SourceFile != "" {
		_ = os.Remove(filepath.Join(s.dataDir, "files", filepath.Base(inv.SourceFile)))
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) stock(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.st.Summary("", "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []store.StockRow{}
	}
	writeJSON(w, rows)
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request) {
	ms, err := s.st.Ledger(pathID(r), "", "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if ms == nil {
		ms = []store.Movement{}
	}
	writeJSON(w, ms)
}

func (s *Server) stockOut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemID int64   `json:"itemId"`
		Date   string  `json:"date"`
		Qty    float64 `json:"qty"`
		Note   string  `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	doc, err := s.st.StockOut(req.ItemID, req.Date, req.Qty, req.Note)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]string{"docNo": doc})
}

func (s *Server) deleteMovement(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteStockOut(pathID(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	set, err := s.st.Settings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	set["data_dir"] = s.dataDir
	set["version"] = s.version
	writeJSON(w, set)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var m map[string]string
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.st.SaveSettings(m); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	set, err := s.st.Settings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var f *excelize.File
	var name string
	span := strings.Trim(from+"_"+to, "_")
	switch r.PathValue("kind") {
	case "purchases", "receipts", "services", "import":
		var invs []*model.Invoice
		if id, _ := strconv.ParseInt(q.Get("invoice"), 10, 64); id > 0 {
			inv, err := s.st.Invoice(id)
			if err != nil {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			invs, span = []*model.Invoice{inv}, inv.ReceiptNo
		} else if invs, err = s.st.Invoices(from, to); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		switch r.PathValue("kind") {
		case "purchases":
			f, name = export.PurchaseList(set, invs, from, to), "Bang_ke_HD_mua_vao"
		case "receipts":
			f, name = export.Receipts(set, invs), "Phieu_nhap_kho"
		case "services":
			f, name = export.Services(set, invs, from, to), "Chi_phi_dich_vu"
		default:
			f, name = export.AccountingImport(set, invs), "Nhap_lieu_phan_mem_ke_toan"
		}
	case "stock":
		rows, err := s.st.Summary(from, to)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		f, name = export.StockSummary(set, rows, from, to), "Nhap_xuat_ton"
	case "cards":
		cards, err := s.cards(from, to)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		f, name = export.StockCards(set, cards, from, to), "The_kho"
	default:
		http.NotFound(w, r)
		return
	}
	if span != "" {
		name += "_" + span
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+unsafeChars.ReplaceAllString(name, "-")+`.xlsx"`)
	if err := f.Write(w); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
	}
}

func (s *Server) cards(from, to string) ([]export.Card, error) {
	rows, err := s.st.Summary(from, to)
	if err != nil {
		return nil, err
	}
	moves, err := s.st.Ledger(0, from, to)
	if err != nil {
		return nil, err
	}
	by := map[int64][]store.Movement{}
	for _, m := range moves {
		by[m.ItemID] = append(by[m.ItemID], m)
	}
	cards := make([]export.Card, 0, len(rows))
	for _, r := range rows {
		cards = append(cards, export.Card{Item: r, Moves: by[r.ItemID], OpenQty: r.OpenQty})
	}
	return cards, nil
}
