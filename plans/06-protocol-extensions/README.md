# Phase 06 — Mở rộng protocol và fault

Phạm vi: **Sau MVP**. Trạng thái: **Done**.

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

2026-09-14: **Done** P17 → P16 → P18. Đã hiện thực và kiểm chứng 15 AC bằng unit/integration tests, full race, vet, CLI build, binary và Docker (kể cả regression MVP). Review không còn blocker. Xem [bằng chứng nghiệm thu](acceptance.md) và [ví dụ/capability matrix](../../examples/grpc/README.md).

Xem [lộ trình và quy tắc thực hiện](../README.md).

## BUILD — Contract triển khai

- `protocol: http1|http2|grpc` chọn listener/adapter. `upstream_protocol: http1|http2` mặc định theo adapter; gRPC bắt buộc http2. `http2` trên URL/listener không TLS là prior knowledge, không HTTP/1 Upgrade. TLS chỉ quảng bá protocol cấu hình.
- `tls.client_ca_file` bật require-and-verify client; `upstream_tls.cert_file/key_file` cung cấp danh tính proxy, `ca_file` tùy chọn. Mọi field/file TLS thuộc restart fingerprint.
- HTTP/1 giữ năm action MVP. HTTP/2 hỗ trợ delay, respond, hold_request/hold_response; gRPC hỗ trợ delay và hold. Hai protocol từ chối close_connection; gRPC từ chối respond. Hold HTTP/2 kết thúc stream sau thời gian chờ. Delay dùng hai phase hiện có.
- Truncate/throttle dùng `direction: request|response`, phase tương ứng `before_upstream_request|after_upstream_headers`. Truncate có `bytes: N` (N >= 0); throttle có `bytes_per_second: R` (R > 0). Selected tại decision, reached khi gắn wrapper, applied khi thực sự cắt hoặc bắt đầu chờ cho byte body đầu tiên. Body rỗng hoặc N >= độ dài không applied.
- Truncate dò thêm tối đa một byte ở biên N, lỗi nguồn trước biên là lỗi tự nhiên. Throttle đọc tối đa min(16 KiB, max(1, R/10)) mỗi lần, trả byte sau khi chờ n/R; không tích lũy tín dụng khi nguồn chậm. Burst tối đa một chunk. Thời gian kiểm chứng tối thiểu B/R trừ 20 ms, dung sai trên 1 giây cho scheduling ở fixture nhỏ.
- Transport dùng Go 1.26 ClientConn.RoundTrip trực tiếp cho HTTP/2 để tránh vòng retry của Transport. Mỗi flow sở hữu upstream connection riêng, đóng khi body/flow kết thúc; client có thể multiplex stream trên cùng listener connection.
