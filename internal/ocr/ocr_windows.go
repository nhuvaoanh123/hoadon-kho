package ocr

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed ocr.ps1
var script []byte

const createNoWindow = 0x08000000

// Run recognises the text of an image or PDF (ext like ".png", ".pdf").
// With nil data it only reports the installed OCR languages.
func Run(data []byte, ext string) (*Result, error) {
	dir, err := os.MkdirTemp("", "hoadonkho-ocr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := ""
	ps := filepath.Join(dir, "ocr.ps1")
	if data != nil {
		in = filepath.Join(dir, "input"+strings.ToLower(ext))
		if err := os.WriteFile(in, data, 0o600); err != nil {
			return nil, err
		}
	}
	// BOM để PowerShell 5.1 đọc script UTF-8 có tiếng Việt.
	if err := os.WriteFile(ps, append([]byte("\xef\xbb\xbf"), script...), 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", ps)
	cmd.Env = append(os.Environ(), "HOADONKHO_OCR_PATH="+in)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("OCR của Windows lỗi: %v %s", err, msg)
	}
	var res Result
	if err := json.Unmarshal(bytes.TrimPrefix(stdout.Bytes(), []byte("\xef\xbb\xbf")), &res); err != nil {
		return nil, fmt.Errorf("OCR trả về dữ liệu lạ: %w", err)
	}
	return &res, nil
}

// Languages returns the OCR languages Windows has installed.
func Languages() ([]string, error) {
	res, err := Run(nil, "")
	if err != nil {
		return nil, err
	}
	return res.Langs, nil
}

// InstallVietnamese starts an elevated PowerShell (Windows asks the user to allow it)
// that adds the Vietnamese OCR capability.
func InstallVietnamese() error {
	inner := "Add-WindowsCapability -Online -Name 'Language.OCR~~~vi-VN~0.0.1.0'; Write-Host 'Xong. Dong cua so nay.'; Read-Host"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command",
		"Start-Process powershell.exe -Verb RunAs -ArgumentList '-NoProfile','-Command',\""+inner+"\"")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Run()
}
