# P08 — Đóng connection có chủ đích

- Trạng thái: **Planned**
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

## VERIFY

- Probability 0/1 trước upstream: đếm chính xác request đích và client outcome; listener khác không bị ảnh hưởng.
- Sau headers không có response cuối hoàn chỉnh; upstream lỗi trước headers cho not_reached. Chạy HTTP/HTTPS và kiểm tra cleanup.

## REVIEW

Không cam kết TCP RST hay cùng error string trên mọi client; đóng connection ảnh hưởng khả năng reuse.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
