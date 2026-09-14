# Phase 06 — Mở rộng protocol và fault

Phạm vi: **Sau MVP**. Trạng thái: **Planned**.

## DEFINE — Phạm vi đã chốt ngày 2026-09-14

- Adapter thứ hai là **gRPC**, bắt đầu với unary RPC. HTTP/2 hoạt động ở cả app → Faultline và Faultline → upstream, qua TLS hoặc không TLS khi cấu hình tường minh.
- **mTLS tùy chọn ở từng phía độc lập**, dùng được với HTTP/1.1, HTTP/2 và gRPC qua TLS. Không cấu hình thì giữ TLS thông thường; bật xác thực client ở listener thì bắt buộc certificate hợp lệ từ CA cấu hình.
- Thêm **truncate và throttle**, mỗi action hỗ trợ chiều request hoặc response, cho HTTP hiện có và gRPC mới. Một flow chỉ có một action và một chiều được chọn; chưa phối hợp hai fault hoặc hai chiều trên cùng flow.
- Mỗi RPC là một flow; dùng lại rule/control/recorder. Fault trên HTTP/2 tác động vào stream được chọn, không mặc nhiên đóng toàn connection.
- Giữ startup disabled, enable/disable và reload chỉ ảnh hưởng flow mới, reset counters khi cấu hình hiệu lực thay đổi. TLS/protocol/listener/upstream thay đổi cần restart.

## Các plan và thứ tự triển khai

| Thứ tự | Plan | Kết quả | Phụ thuộc |
| --- | --- | --- | --- |
| 1 | [P17](02-http2-mtls.md) | HTTP/2 hai phía và mTLS tùy chọn; contract kết thúc stream | P15 |
| 2 | [P16](01-second-adapter.md) | gRPC unary, metadata/status/trailers và lifecycle | P17 |
| 3 | [P18](03-additional-faults.md) | Truncate rồi throttle, request/response trên HTTP và gRPC | P16, P17 |

Số plan giữ nguyên để bảo toàn tham chiếu; triển khai theo phụ thuộc, không theo số P16/P17. mTLS thuộc tiêu chí hoàn tất P17 dù người dùng có thể không bật trong cấu hình.

## Điều kiện hoàn tất phase

- Đạt H2-1–H2-3 và MT-1–MT-4 ở P17, GR-1–GR-4 ở P16, FT-1–FT-4 ở P18; ghi lệnh và kết quả thật trong từng plan.
- Có ví dụ cấu hình và integration fixtures cho unary RPC, upload/download dở dang và truyền body chậm; không yêu cầu sửa code ứng dụng để dùng chức năng cơ bản.
- Regression HTTP/HTTPS MVP, rule selection, reload, admin, recorder, cancellation và giới hạn tài nguyên đều pass. Kiểm thử binary/Docker với cấu hình/certificate mới và ghi rõ môi trường, checks bị bỏ qua hoặc blocker.
- Full Go tests, race tests, vet và build CLI pass; review capability matrix, retry ngoài ý muốn, stream isolation và cleanup không còn blocker. Các AC mới bổ sung, không thay thế AC1–AC24.

## Phần tiếp tục Deferred

- gRPC client/server/bidirectional streaming: đợt riêng với tiêu chí lifecycle, flow control và tài nguyên cho stream dài; chưa cam kết chỉ vì unary đã pass.
- TCP adapter, gRPC-Web, fault đóng toàn HTTP/2 connection, TCP reset/half-close, weighted selection và action chaining.
- Hot reload certificate, truyền danh tính certificate gốc của app tới upstream, policy danh tính SAN/SPIFFE và quản lý cấp/thu hồi certificate tự động.

## Tiến độ

2026-09-14: hoàn tất cập nhật phạm vi và kế hoạch theo trao đổi. Chưa BUILD hoặc chạy kiểm thử tính năng; phase và các plan vẫn **Planned**.

Xem [lộ trình và quy tắc thực hiện](../README.md).
