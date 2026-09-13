# P08 — Đóng connection có chủ đích

- Trạng thái: **Done** (2026-09-13)
- Phụ thuộc: [P06](../02-http-proxy/03-flow-lifecycle.md)
- Nguồn: [specific.md](../../specific.md), mục 5.1, 5.3, 6.1.
- Nghiệm thu MVP liên quan: AC2, AC3, AC13, AC22
- Vùng thay đổi dự kiến: `internal/fault/, internal/proxy/http/, tests/integration/`

## DEFINE

Đóng connection phía client ở hai phase, kết thúc upstream đúng lifecycle.

## PLAN → BUILD

1. Trước upstream: đóng client mà không gửi request đích; sau headers: đóng client và cancel/close luồng upstream.
2. Quản lý connection đã tách khỏi HTTP server để deadline/shutdown vẫn giải phóng được.
3. Ghi fault applied riêng với lỗi tự nhiên; không chuyển lỗi close thành một response giả thành công.

Quyết định: executor cancel upstream rồi gọi capability CloseConnection của HTTP adapter.
Adapter đóng trực tiếp connection đã lưu trong context, tránh Hijack tranh chấp
với goroutine đang đọc bỏ upload của hold_request. Handler không flush thêm response
trên connection đã đóng; shutdown/deadline vẫn dùng cùng connection.

## VERIFY

- Probability 0/1 trước upstream: đếm chính xác request đích và client outcome; listener khác không bị ảnh hưởng.
- Sau headers không có response cuối hoàn chỉnh; upstream lỗi trước headers cho not_reached. Chạy HTTP/HTTPS và kiểm tra cleanup.

## REVIEW

Không cam kết TCP RST hay cùng error string trên mọi client; đóng connection ảnh hưởng khả năng reuse.

## Kết quả VERIFY/REVIEW

- Full race suite, vet, build và CLI smoke pass; lệnh/bằng chứng chung tại [phase README](README.md#kết-quả).
- Probability 0/1: mỗi trường hợp năm request tại listener inject, xen kẽ năm request ở listener độc lập. Upstream nhận chính xác 10/5 request tương ứng.
- Cả bốn tổ hợp HTTP/HTTPS đóng client ở hai phase; trước upstream không có attempt, sau headers upstream được cancel và client không nhận final response.
- Upstream lỗi trước headers vẫn trả 502 tự nhiên, selected/not_reached=true và applied=false.
- Review thay Hijack bằng đóng trực tiếp connection để tương thích đọc upload đồng thời; ngăn flush sau close và tránh ghi đóng chủ động thành client cancel. Không cam kết TCP RST/error string.
