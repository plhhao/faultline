# P05 — TLS hai phía và HTTP/1.1

- Trạng thái: **Done**
- Phụ thuộc: [P04](../02-http-proxy/01-cli-and-forwarding.md)
- Nguồn: [specific.md](../../specific.md), mục 3.1, 7.
- Nghiệm thu MVP liên quan: AC22, AC23
- Vùng thay đổi dự kiến: `internal/proxy/http/, internal/config/, tests/integration/`

## DEFINE

Hỗ trợ bốn tổ hợp HTTP/HTTPS với TLS termination và trust hợp lệ, giữ HTTP/1.1 ở hai phía.

## PLAN → BUILD

1. Nạp listener cert/key và upstream trust gồm system CA cộng CA test; SNI/hostname lấy từ upstream URL.
2. Cấu hình rõ HTTP/1.1 và negotiation cho cả inbound/outbound; không vô tình bật HTTP/2.
3. Phân biệt TLS lỗi tự nhiên với quyết định fault; tạo chứng chỉ test tạm thời thay vì commit private key thật.

## VERIFY

- Integration matrix HTTP→HTTP, HTTP→HTTPS, HTTPS→HTTP, HTTPS→HTTPS; kiểm tra phiên bản thực dùng.
- Sai hostname, CA, cert/key phải có lỗi phù hợp; không có mặc định bỏ verify TLS. Fault matrix được mở rộng ở P07–P09.

## REVIEW

Chưa có mTLS, cert hot reload hoặc TLS tunnel semantic matching.

## Kết quả VERIFY / REVIEW — 2026-09-13

- Bốn tổ hợp HTTP/HTTPS pass integration test; kiểm tra thực tế HTTP/1.1 hai phía và ALPN dù client/upstream hỗ trợ HTTP/2.
- Test SNI `localhost`, system trust cộng CA test, sai hostname và CA không tin cậy pass. Lỗi TLS upstream trả 502 và report `upstream_error`, selected sau headers là `not_reached`, không applied.
- Cert/key test sinh tạm; key hỏng sau config validation làm startup thất bại trước serving. Validation cert/key không khớp được bao phủ bởi regression phase 1.
- Full race suite, vet và build pass. Review không có insecure skip verification, mTLS, tunnel hoặc cert hot reload. AC22 phần thực thi action vẫn cần P07–P09.
