//go:build !windows

package ocr

// Run is only implemented on Windows.
func Run(data []byte, ext string) (*Result, error) { return nil, ErrUnavailable }

// Languages is only implemented on Windows.
func Languages() ([]string, error) { return nil, ErrUnavailable }

// InstallVietnamese is only implemented on Windows.
func InstallVietnamese() error { return ErrUnavailable }
