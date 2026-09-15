# P16 — Adapter gRPC unary

- Trạng thái: **Done**
- Phụ thuộc: [P17](02-http2-mtls.md), trên nền MVP [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 9, 9.1, 13; [phạm vi phase 06](README.md).
- Nghiệm thu MVP liên quan: Regression rule/control/recorder; thêm tiêu chí riêng bên dưới.
- Vùng thay đổi dự kiến: `internal/proxy/grpc/`, `internal/config/`, contract dùng chung khi cần, `tests/integration/`, ví dụ gRPC.

## DEFINE

Adapter thứ hai là **gRPC unary**, dùng HTTP/2 hai phía và TLS/mTLS tùy chọn từ P17. Use case nghiệm thu: app gọi unary RPC với payload nhỏ/lớn, quan sát deadline/cancellation và kết quả RPC; P18 bổ sung upload/download dở dang và truyền chậm bằng fixture payload lớn.

- Một RPC là một flow/attempt; retry do client là attempt mới. Snapshot và selector quyết định một lần khi nhận metadata, không theo connection hoặc từng message.
- Match theo tên service/method từ RPC path và metadata; không cần `.proto`, giải mã nghiệp vụ hoặc thêm header cho chức năng cơ bản.
- Chuyển tiếp đúng message bytes, metadata, headers/trailers và gRPC status; HTTP status không thay thế gRPC status. Proxy không tự retry.
- Dùng lại engine, control và recorder; I/O và diễn giải protocol nằm trong adapter. Mỗi flow tối đa một action, giữ startup disabled và hiệu lực reload/enable/disable hiện tại.
- gRPC client/server/bidirectional streaming, gRPC-Web và TCP tiếp tục Deferred. Không quảng bá streaming support từ kết quả test unary; tài liệu phải nêu rõ phạm vi được kiểm chứng.

## PLAN → BUILD

1. Dựa trên P17, ghi capability matrix gRPC gồm matcher, phase, action và cách báo lỗi trước BUILD. Xác định ánh xạ delay/hold và stream termination phù hợp; không áp dụng máy móc HTTP `respond` hoặc `close_connection` cho RPC. Capability chưa hỗ trợ bị config từ chối.
2. Hiện thực adapter Go trên transport HTTP/2, bảo toàn message framing, metadata/trailers/status và deadline/cancellation; không dựng framework plugin hay yêu cầu generated service code của ứng dụng.
3. Tái sử dụng selector/snapshot/control/recorder, chỉ mở contract có bằng chứng cần thiết; lỗi transport, lỗi RPC tự nhiên và fault applied được phân biệt.
4. Thêm unary fixture và ví dụ cấu hình chạy local/binary/Docker, gồm TLS/mTLS. Fixture generated code nếu cần chỉ phục vụ kiểm thử, không là đầu vào bắt buộc của proxy.
5. Công bố contract body streaming theo byte cho P18; unary payload lớn cũng phải có backpressure và bộ nhớ giới hạn.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| GR-1 | Unary pass-through với payload nhỏ/lớn giữ dữ liệu, metadata và status/trailers thành công/lỗi; chạy HTTP/2 không TLS, TLS và mTLS một/hai phía |
| GR-2 | Service/method/metadata matcher, probability 0/1, nth/every và first-match hoạt động theo RPC; startup disabled không tiêu thụ sequence; không đếm theo message/connection |
| GR-3 | Các RPC đồng thời trên cùng connection giữ snapshot đúng khi reload/enable/disable; counters reset/no-op đúng; lỗi một RPC không kết thúc RPC khác ngoài scope; proxy không retry upstream |
| GR-4 | Deadline, client cancel, upstream lỗi và shutdown giải phóng flow/body; log/counters phân biệt selected/applied/not_reached và lỗi tự nhiên; config từ chối capability không hỗ trợ |

Regression HTTP/HTTPS MVP và chạy tests/race/vet/build theo [workflow](../README.md#theo-dõi-thực-hiện). Kiểm tra dependency imports và lượng thay đổi engine, ghi kết quả thực tế cùng giới hạn unary.

## REVIEW

Không đưa protocol I/O vào engine, không đồng nhất HTTP status với gRPC status hoặc RPC với TCP connection. Review metadata/trailers, retry, cleanup và việc tái sử dụng TLS từ P17; không mở rộng sang streaming hoặc semantic business assertions.


## BUILD / VERIFY — 2026-09-14

Adapter gRPC unary dùng shared HTTP/2 I/O và helper metadata/status/deadline tại `internal/proxy/grpc`. Matcher service/method/metadata không cần protobuf ứng dụng. Có fixture Go và ví dụ binary/Docker/mTLS tại `examples/grpc`. GR-1–GR-4 đã có test pass, xem [lệnh, matrix và review](acceptance.md). Streaming vẫn Deferred.

2026-09-14: **Done** sau full tests/race, vet, build CLI, binary/Docker và review; kết quả/lệnh cụ thể ở [bằng chứng nghiệm thu](acceptance.md).
