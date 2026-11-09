// Command HoaDonKho reads Vietnamese purchase invoices, separates goods from services
// and keeps the goods in a stock ledger. It serves a local web UI on 127.0.0.1.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/nhuvaoanh123/hoadon-kho/internal/classify"
	"github.com/nhuvaoanh123/hoadon-kho/internal/einvoice"
	"github.com/nhuvaoanh123/hoadon-kho/internal/server"
	"github.com/nhuvaoanh123/hoadon-kho/internal/store"
)

var version = "dev"

func main() {
	setupConsole()
	port := flag.Int("port", 8765, "cổng web nội bộ")
	dataFlag := flag.String("data", "", "thư mục dữ liệu (mặc định: HoaDonKho-data cạnh file chạy)")
	noBrowser := flag.Bool("no-browser", false, "không tự mở trình duyệt")
	dumpPDF := flag.String("dump-pdf", "", "in ra chữ đọc được từ file PDF hoặc ảnh (OCR) rồi thoát (để báo lỗi)")
	parseOnly := flag.Bool("parse", false, "đọc các file hóa đơn trong tham số, in kết quả JSON rồi thoát")
	flag.Parse()
	if *dumpPDF != "" {
		os.Exit(dump(*dumpPDF))
	}
	if *parseOnly {
		os.Exit(parseFiles(flag.Args()))
	}
	if err := run(*port, *dataFlag, !*noBrowser); err != nil {
		fmt.Println("LỖI:", err)
		fmt.Println("Nhấn Enter để đóng...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
}

func run(port int, dataDir string, browser bool) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if alreadyRunning(addr) {
			fmt.Println("Ứng dụng đang chạy, mở lại trình duyệt...")
			openBrowser("http://" + addr)
			return nil
		}
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	if dataDir == "" {
		dataDir = defaultDataDir()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dataDir, "hoadonkho.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	url := "http://" + ln.Addr().String()
	fmt.Println("==============================================")
	fmt.Println(" HÓA ĐƠN → KHO  phiên bản", version)
	fmt.Println(" Đang chạy tại:", url)
	fmt.Println(" Dữ liệu:      ", dataDir)
	fmt.Println(" Đóng cửa sổ này để thoát ứng dụng.")
	fmt.Println("==============================================")
	if browser {
		go func() { time.Sleep(300 * time.Millisecond); openBrowser(url) }()
	}
	srv := &http.Server{Handler: server.New(st, dataDir, version), ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(ln)
}

func dump(path string) int {
	data, err := os.ReadFile(path)
	if err == nil {
		var lines []string
		if lines, err = einvoice.DumpLines(path, data); err == nil {
			for _, l := range lines {
				fmt.Println(l)
			}
			return 0
		}
	}
	fmt.Println("LỖI:", err)
	return 1
}

// parseFiles prints one JSON object per invoice found, classified with the default rules.
func parseFiles(paths []string) int {
	var c classify.Classifier
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	code := 0
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			_ = enc.Encode(map[string]string{"file": path, "error": err.Error()})
			code = 1
			continue
		}
		for _, p := range einvoice.ParseAny(filepath.Base(path), data) {
			out := map[string]any{"file": path, "name": p.Name, "format": p.Ext, "warnings": p.Warnings}
			if p.Err != nil {
				out["error"] = p.Err.Error()
				code = 1
			} else {
				for i := range p.Invoice.Lines {
					l := &p.Invoice.Lines[i]
					l.Kind, l.Reason, l.Certain = c.Classify(*l)
				}
				einvoice.Verify(p.Invoice)
				out["invoice"] = p.Invoice
				out["problems"] = einvoice.Problems(p.Invoice)
			}
			_ = enc.Encode(out)
		}
	}
	return code
}

func alreadyRunning(addr string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get("http://" + addr + "/api/ping")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 512))
	return strings.Contains(string(b), server.AppID)
}

// defaultDataDir prefers a folder next to the program (portable), else the user profile.
func defaultDataDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "HoaDonKho-data")
		if writable(dir) {
			return dir
		}
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		return filepath.Join(cfg, "HoaDonKho")
	}
	return "HoaDonKho-data"
}

func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	return !errors.Is(os.Remove(name), os.ErrPermission)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("Hãy mở trình duyệt và vào:", url)
	}
}
