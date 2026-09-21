# P24 — Hiện thực PostgreSQL adapter

- Trạng thái: **In progress — code và automated tests hoàn tất; chờ manual UI**
- Phụ thuộc: [P23](01-semantic-contract.md) hoàn tất contract.
- Nguồn: [specific.md](../../specific.md), mục 9, 9.1, 13; [phạm vi Phase 9](README.md).
- Vùng thay đổi dự kiến: `internal/proxy/` (package PostgreSQL chốt ở P23), config, engine/control/recorder nếu cần, CLI/UI capabilities, examples và integration tests.

## DEFINE

Hiện thực adapter đúng protocol/version và action × phase đã chốt. Hỗ trợ delay, hold giới hạn và ngắt kết nối; có điểm sau xác nhận COMMIT thành công. Kết nối trực tiếp, SCRAM-SHA-256, thường/TLS. Không mở rộng sang broker hoặc tính năng PostgreSQL ngoài contract.

## PLAN → BUILD

1. Bổ sung validation/capabilities và adapter đăng ký trong binary. Tái sử dụng engine/control/recorder, giữ protocol I/O trong adapter; không xây framework plugin.
2. Hiện thực startup/authentication/TLS, framing và transaction state machine theo P23; giới hạn bộ nhớ, timeout, cleanup và xử lý protocol không hỗ trợ.
3. Đặt hooks tại các phase đã chốt. Delay tiếp tục luồng; hold có giới hạn rồi kết thúc theo contract; disconnect đóng đúng scope. Không forward xác nhận thành công rồi mới áp dụng fault mất xác nhận.
4. Tích hợp reload/enable/disable, snapshot và counters theo đơn vị flow của P23. UI chỉ hiện matcher/action/phase PostgreSQL thực sự hỗ trợ; credentials không nằm trong event/log.
5. Thêm fixture Go + PostgreSQL Docker với transaction tường minh, test kết nối thường và TLS/SCRAM. Không thêm Node.js/TypeORM hoặc case tắt transaction.
6. Demo commit đã thành công nhưng client nhận lỗi, query kiểm chứng qua kết nối riêng. Demo retry không chống trùng và có unique operation ID; dữ liệu là bằng chứng, không dùng riêng client status để kết luận.
7. Viết hướng dẫn CLI/UI/binary/Docker và manual acceptance phù hợp. Báo cáo kết quả test bằng tài liệu, chưa thêm runner/report framework Phase 8.

## VERIFY

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P24-AC1 | Baseline qua proxy: xác thực, transaction ghi/đọc/commit/rollback hoạt động theo capability matrix; kết nối thường và TLS được kiểm chứng. |
| P24-AC2 | Delay/hold/disconnect chạy tại mọi tổ hợp phase đã công bố; phase không hợp lệ bị từ chối; không chọn fault thì forwarding bình thường. |
| P24-AC3 | Sau xác nhận COMMIT thành công: delay cuối cùng trả thành công; hold/disconnect gây lỗi client trong giới hạn test nhưng kết nối kiểm chứng thấy dữ liệu đã commit. |
| P24-AC4 | COMMIT lỗi, transaction rollback và lỗi trước commit không bị ghi nhận là commit thành công; client error không được dùng làm bằng chứng rollback. |
| P24-AC5 | Reload/no-op, enable/disable, probability và snapshot trên session dài hạn đúng P23; fault đang chạy tuân theo policy khi disable. |
| P24-AC6 | Cancellation, timeout, client disconnect, malformed/unsupported protocol và shutdown không treo/rò goroutine/connection; có bound frame/buffer và kiểm tra secrets không xuất hiện trong log. |
| P24-AC7 | Fixture retry chứng minh sự khác nhau có/không chống trùng bằng dữ liệu độc lập; có hướng dẫn tái hiện, giới hạn và recovery kết nối. |
| P24-AC8 | Regression HTTP/HTTP2/gRPC và quản trị CLI/API/UI pass; build binary/Docker, Go tests/race/vet và asset checks liên quan pass. UI PostgreSQL được kiểm tra và ghi kết quả thật. |

## REVIEW

Đối chiếu matrix P23 và bằng chứng thực tế trước đánh dấu Done. Không claim tương thích mọi ORM hoặc toàn PostgreSQL; không gắn lỗi timeout với rollback mặc định. Phase 8 vẫn Deferred, không thêm nghiệp vụ tự đánh giá vào proxy.

## Tiến độ

Đã hiện thực `internal/proxy/postgresql`, config/CLI/API/UI và fixture Go. Automated evidence tại [acceptance](acceptance.md); manual UI ở [hướng dẫn PostgreSQL](../../examples/postgresql/README.md). Người dùng xác nhận PG1–PG6 PASS ngày 2026-09-17. P24 còn chờ kiểm thử chuyển qua lại HTTP/gRPC/PostgreSQL trong cùng UI.
