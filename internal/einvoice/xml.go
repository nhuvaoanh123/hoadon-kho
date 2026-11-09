// Package einvoice reads Vietnamese e-invoices (XML per Thông tư 78/2021, PDF best-effort).
package einvoice

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nhuvaoanh123/hoadon-kho/internal/model"
	"github.com/nhuvaoanh123/hoadon-kho/internal/textutil"
)

type xParty struct {
	Ten  string
	MST  string
	DChi string
}

type xLine struct {
	TChat  string
	STT    string
	MHHDVu string
	THHDVu string
	DVTinh string
	SLuong string
	DGia   string
	ThTien string
	TSuat  string
	TTKhac []struct {
		TTruong string
		DLieu   string
	} `xml:"TTKhac>TTin"`
}

type xHDon struct {
	DLHDon struct {
		TTChung struct {
			KHMSHDon string
			KHHDon   string
			SHDon    string
			NLap     string
			DVTTe    string
			TGia     string
			HTTToan  string
		} `xml:"TTChung"`
		NDHDon struct {
			NBan  xParty  `xml:"NBan"`
			NMua  xParty  `xml:"NMua"`
			Lines []xLine `xml:"DSHHDVu>HHDVu"`
			TToan struct {
				TgTCThue  string
				TgTThue   string
				TgTTTBSo  string
				TgTTTBChu string
				ByRate    []struct{ TSuat, ThTien, TThue string } `xml:"THTTLTSuat>LTSuat"`
			} `xml:"TToan"`
		} `xml:"NDHDon"`
	} `xml:"DLHDon"`
	MCCQT struct {
		Value string `xml:",chardata"`
	} `xml:"MCCQT"`
}

// ErrNotInvoiceXML means the XML has no <HDon> element.
var ErrNotInvoiceXML = errors.New("không tìm thấy thẻ <HDon>: file XML không phải hóa đơn điện tử theo TT78")

// ParseXML reads an e-invoice XML (bare <HDon> or wrapped in <TDiep>).
func ParseXML(data []byte) (*model.Invoice, []string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var h xHDon
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return nil, nil, ErrNotInvoiceXML
		}
		if err != nil {
			return nil, nil, fmt.Errorf("XML lỗi: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "HDon" {
			if err := d.DecodeElement(&h, &se); err != nil {
				return nil, nil, fmt.Errorf("XML lỗi: %w", err)
			}
			break
		}
	}
	return fromXML(&h)
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(s, ",", "")), 64)
	return v
}

func fromXML(h *xHDon) (*model.Invoice, []string, error) {
	tc := h.DLHDon.TTChung
	nd := h.DLHDon.NDHDon
	inv := &model.Invoice{
		Form:        textutil.Clean(tc.KHMSHDon),
		Serial:      textutil.Clean(tc.KHHDon),
		Number:      strings.TrimLeft(textutil.Clean(tc.SHDon), "0"),
		Date:        textutil.Clean(tc.NLap),
		Currency:    strings.ToUpper(textutil.Clean(tc.DVTTe)),
		ExRate:      num(tc.TGia),
		PayMethod:   textutil.Clean(tc.HTTToan),
		TaxAuthCode: textutil.Clean(h.MCCQT.Value),
		Seller:      party(nd.NBan),
		Buyer:       party(nd.NMua),
		Source:      model.SourceXML,
		Stated: model.Stated{BeforeVAT: num(nd.TToan.TgTCThue), VAT: num(nd.TToan.TgTThue),
			Total: num(nd.TToan.TgTTTBSo), Words: textutil.Clean(nd.TToan.TgTTTBChu)},
	}
	if len(inv.Date) > 10 {
		inv.Date = inv.Date[:10]
	}
	if inv.Currency == "" {
		inv.Currency = "VND"
	}
	if inv.ExRate <= 0 {
		inv.ExRate = 1
	}
	var warns []string
	for i, xl := range nd.Lines {
		l := model.Line{
			STT:     int(num(xl.STT)),
			Nature:  int(num(xl.TChat)),
			Code:    textutil.Clean(xl.MHHDVu),
			Name:    textutil.Clean(xl.THHDVu),
			Unit:    textutil.Clean(xl.DVTinh),
			Qty:     num(xl.SLuong),
			Price:   num(xl.DGia),
			Amount:  num(xl.ThTien),
			VATRate: textutil.NormRate(xl.TSuat),
		}
		if l.STT == 0 {
			l.STT = i + 1
		}
		if l.Nature == 0 {
			l.Nature = 1
		}
		l.VATAmount = lineVAT(xl, l)
		inv.Lines = append(inv.Lines, l)
	}
	if len(inv.Lines) == 0 {
		warns = append(warns, "Hóa đơn không có dòng hàng hóa/dịch vụ nào")
	}
	finishTotals(inv, num(nd.TToan.TgTCThue), num(nd.TToan.TgTThue), num(nd.TToan.TgTTTBSo), &warns)
	return inv, warns, nil
}

func party(p xParty) model.Party {
	return model.Party{Name: textutil.Clean(p.Ten), TaxCode: strings.ReplaceAll(textutil.Clean(p.MST), " ", ""), Address: textutil.Clean(p.DChi)}
}

// lineVAT takes the per-line VAT from TTKhac when present, else computes it.
func lineVAT(xl xLine, l model.Line) float64 {
	for _, t := range xl.TTKhac {
		f := strings.ToLower(t.TTruong)
		if strings.Contains(f, "vatamount") || strings.Contains(f, "tienthue") || strings.Contains(f, "tthue") || f == "tiền thuế" {
			if v := num(t.DLieu); v != 0 {
				return v
			}
		}
	}
	return textutil.Round(l.Amount*textutil.ParseRate(l.VATRate), 0)
}

// finishTotals fills invoice totals from the lines and warns on mismatch with stated totals.
func finishTotals(inv *model.Invoice, before, vat, total float64, warns *[]string) {
	var sb, sv float64
	for _, l := range inv.Lines {
		if l.Nature == 4 {
			continue
		}
		sign := 1.0
		if l.Nature == 3 {
			sign = -1
		}
		sb += sign * l.Amount
		sv += sign * l.VATAmount
	}
	inv.TotalBeforeVAT, inv.TotalVAT, inv.Total = textutil.Round(sb, 2), textutil.Round(sv, 2), textutil.Round(sb+sv, 2)
	if before != 0 && abs(before-sb) > 1 {
		*warns = append(*warns, fmt.Sprintf("Tổng tiền hàng trên hóa đơn (%s) khác tổng các dòng (%s)", money(before), money(sb)))
	}
	if before != 0 {
		inv.TotalBeforeVAT = before
	}
	if vat != 0 || before != 0 {
		if abs(vat-sv) > float64(len(inv.Lines)) {
			*warns = append(*warns, fmt.Sprintf("Tiền thuế trên hóa đơn (%s) khác tổng thuế các dòng (%s)", money(vat), money(sv)))
		}
		inv.TotalVAT = vat
	}
	if total != 0 {
		inv.Total = total
	} else {
		inv.Total = inv.TotalBeforeVAT + inv.TotalVAT
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func money(v float64) string {
	s := strconv.FormatFloat(textutil.Round(v, 0), 'f', 0, 64)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
