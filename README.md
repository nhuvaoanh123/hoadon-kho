# Hóa đơn → Kho (HoaDonKho)

Ứng dụng Windows miễn phí cho doanh nghiệp thương mại Việt Nam: **tải hóa đơn đầu vào lên, app tự tách
hàng hóa và dịch vụ, đưa hàng hóa vào kho**, rồi xuất ra các file Excel kế toán cần
(phiếu nhập kho, bảng kê mua vào, nhập-xuất-tồn, thẻ kho, file nhập liệu MISA/Fast).

- Một file `HoaDonKho.exe` duy nhất, **không cần cài đặt**, không cần Internet, dữ liệu nằm trên máy bạn.
- Đọc **XML hóa đơn điện tử** theo Thông tư 78/2021 (chính xác tuyệt đối), **PDF** có lớp chữ, và **ZIP**
  tải từ cổng hóa đơn điện tử.

## Tải về và chạy

1. Vào trang [**Releases**](../../releases/latest), tải `HoaDonKho.exe`.
2. Chép file vào một thư mục riêng, ví dụ `D:\HoaDonKho\`, rồi nháy đúp để chạy.
   - Windows có thể báo *"Windows protected your PC"* vì file chưa ký số: bấm **More info → Run anyway**.
3. Trình duyệt tự mở trang `http://127.0.0.1:8765`. Cửa sổ đen là ứng dụng đang chạy, **đóng nó để thoát**.
4. Lần đầu: vào **Cài đặt**, điền tên, địa chỉ, MST doanh nghiệp (in lên phiếu nhập kho).

Dữ liệu lưu ở thư mục `HoaDonKho-data` cạnh file exe. **Sao lưu = chép cả thư mục này.**

## Cách dùng

1. **Nhập hóa đơn**: kéo thả các file XML/PDF/ZIP vào. Mỗi hóa đơn hiện ra một bảng để kiểm tra:
   - Dòng **xanh lá** = hàng hóa → vào kho; dòng **xanh dương** = dịch vụ → chi phí, không vào kho.
   - Cột *Lý do* cho biết vì sao app phân loại như vậy. Sai thì đổi ở cột *Phân loại*,
     **lần sau app tự nhớ** cho mặt hàng đó.
   - Bấm **Lưu & nhập kho**. Hóa đơn trùng (cùng MST người bán, ký hiệu, số) sẽ bị chặn.
2. **Hóa đơn đã nhập**: xem lại, tải phiếu nhập kho từng hóa đơn, mở file gốc, xóa nếu nhập nhầm.
3. **Tồn kho**: số lượng, giá vốn bình quân, giá trị tồn; **Xuất kho** khi bán/dùng; xem **Thẻ kho**.
4. **Báo cáo Excel**: chọn kỳ (tháng/quý/năm) rồi bấm loại báo cáo cần tải.

## Các file Excel xuất ra

| File | Nội dung |
|---|---|
| Bảng kê hóa đơn mua vào | Theo nhóm thuế suất 0/5/8/10%, KCT, KKKNT; dùng đối chiếu tờ khai thuế GTGT |
| Phiếu nhập kho (mẫu 01-VT) | Mỗi hóa đơn có hàng hóa một phiếu, số tiền bằng chữ, chỗ ký; căn cứ TT133/TT200/TT99 chọn trong Cài đặt |
| Bảng tổng hợp Nhập - Xuất - Tồn | Tồn đầu, nhập, xuất, tồn cuối theo số lượng và giá trị |
| Thẻ kho (sổ kho) | Mỗi mặt hàng một trang, theo mẫu thẻ kho |
| Bảng kê chi phí dịch vụ | Các dòng dịch vụ kèm TK chi phí gợi ý |
| File nhập liệu phần mềm kế toán | Một dòng/mặt hàng với TK Nợ/Có, thuế, số hóa đơn; dùng chức năng "Nhập từ Excel" của MISA, Fast... (ghép cột khi nhập) |

## Nguyên tắc tính toán

- **Phân loại**: chiết khấu (TChat=3) và ghi chú (TChat=4) theo XML; còn lại xét từ khóa trong tên
  (*vận chuyển, lắp đặt, sửa chữa, phí, cước, thuê, tư vấn...*) và đơn vị tính (*lần, chuyến, tháng, giờ...*
  là dịch vụ; *cái, kg, m, bộ, thùng...* là hàng hóa). Lựa chọn bạn đã sửa luôn được ưu tiên.
- **Giá nhập kho** = thành tiền chưa thuế GTGT, quy đổi VND theo tỷ giá trên hóa đơn; chiết khấu thương mại
  được phân bổ theo giá trị các dòng.
- **Giá xuất kho**: bình quân gia quyền liên hoàn (sau mỗi lần nhập). Không cho xuất âm kho.
- Mặt hàng được nhận diện theo **tên + đơn vị tính**, mã tự cấp `HH00001`, `HH00002`...

## Giới hạn

- Ảnh chụp/PDF scan không có chữ thì không đọc được: hãy tải **file XML** từ
  [hoadondientu.gdt.gov.vn](https://hoadondientu.gdt.gov.vn) hoặc email của người bán.
- PDF đọc theo bố cục phổ biến; luôn kiểm tra trước khi lưu. Gặp PDF đọc sai, chạy
  `HoaDonKho.exe -dump-pdf "duong-dan\hoa-don.pdf"` và gửi kết quả (đã xóa thông tin nhạy cảm) qua mục Issues.
- File nhập liệu không phải mẫu chính thức của MISA/Fast; tên cột rõ ràng để ghép khi nhập.

## Tùy chọn dòng lệnh

```
HoaDonKho.exe -port 8765 -data "D:\DuLieuKho" -no-browser
```

## Build từ mã nguồn

Cần Go 1.27 trở lên:

```
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-s -w" -o dist/HoaDonKho.exe .
```

Mỗi tag `v*` được GitHub Actions build và đăng lên Releases.

Giấy phép: MIT.
