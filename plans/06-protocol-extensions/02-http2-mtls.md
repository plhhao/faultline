# P17 — HTTP/2 hai phía và mTLS tùy chọn

- Trạng thái: **Planned**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Cung cấp nền cho: [P16](01-second-adapter.md), [P18](03-additional-faults.md)
- Nguồn: [specific.md](../../specific.md), mục 3.1, 9.1, 13; [phạm vi phase 06](README.md).
- Nghiệm thu MVP liên quan: Regression AC10–AC12, AC14–AC16, AC22–AC24; thêm tiêu chí riêng bên dưới.
- Vùng thay đổi dự kiến: `internal/proxy/http/`, `internal/config/`, `internal/control/`, `tests/integration/`, ví dụ và tài liệu TLS.

## DEFINE

HTTP/2 ở cả hai phía, hỗ trợ TLS với negotiation rõ ràng và HTTP/2 không TLS được bật tường minh cho local/Docker. Giữ cấu hình HTTP/1.1 cũ; gRPC cần HTTP/2 hai phía, không âm thầm fallback sang HTTP/1.1.

mTLS là cấu hình tùy chọn độc lập với HTTP/2 và độc lập giữa hai phía:

| Phía | Không cấu hình mTLS | Có cấu hình mTLS |
| --- | --- | --- |
| App → Faultline | TLS thông thường, không yêu cầu client certificate | Listener yêu cầu và xác minh client certificate theo CA cấu hình; thiếu/sai certificate bị từ chối tại handshake |
| Faultline → upstream | Không cung cấp client certificate | Dùng client certificate/key cấu hình khi upstream yêu cầu; vẫn xác minh server certificate và hostname |

CA xác minh client ở listener tách biệt với CA xác minh server upstream. Upstream xác thực certificate của Faultline, không phải certificate gốc của app. Đợt này chỉ xác thực theo CA, chưa thêm allowlist danh tính SAN/SPIFFE.

TLS, CA/cert/key (cả đường dẫn lẫn nội dung), protocol và chế độ mTLS thay đổi cần restart. Reload rule vẫn dùng contract hiện tại. Không thêm hot reload certificate hoặc tự cấp certificate.

HTTP/2 chọn fault theo flow/stream. Contract phải phân biệt kết thúc stream với đóng connection; không đổi nghĩa `close_connection` thành reset stream. Capability chưa hỗ trợ phải bị validation từ chối. Chốt bảng action/phase cho HTTP/2 trước BUILD, gồm cách delay/hold kết thúc stream và ánh xạ lỗi; không thêm fault toàn connection trong đợt này.

## PLAN → BUILD

1. Bổ sung schema cho protocol hai phía, listener client CA và upstream client cert/key; giữ config cũ hợp lệ. Tên field cụ thể là quyết định thiết kế tại BUILD, ghi vào plan và cập nhật ví dụ/spec cùng thay đổi code.
2. Validation cặp cert/key đầy đủ, đọc/parse CA/cert/key, tương thích TLS/protocol; mTLS trên kết nối không TLS phải bị từ chối. Cho phép cấu hình upstream client cert/key trong khi vẫn dùng CA hệ thống để xác minh server nếu không có CA server bổ sung.
3. Mở rộng đường dẫn tương đối theo file khai báo, clone/canonicalization, TLS content digests và restart fingerprint cho các field mới; reload lỗi không đổi snapshot đang chạy.
4. Hiện thực negotiation HTTP/2 và mTLS hai phía, tái sử dụng TLS hiện có bằng helper có trách nhiệm rõ. Kiểm tra pooling/retry của transport để Faultline không tự phát lại request khi có lỗi.
5. Cung cấp lifecycle stream, cancellation, headers/trailers và phân loại lỗi cho P16/P18; giữ giới hạn tài nguyên, không buffer body không giới hạn. Lỗi handshake không được ghi thành fault applied; handshake listener chưa có flow thì ghi diagnostic phù hợp.
6. Thêm integration fixtures sinh certificate tạm thời, ví dụ TLS/mTLS một phía và hai phía, hướng dẫn mount file khi chạy Docker.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| H2-1 | Pass-through xác nhận protocol thực tế ở cả hai phía qua TLS và không TLS, kể cả tổ hợp TLS độc lập; config HTTP/1.1 cũ vẫn hoạt động; protocol không tương thích báo lỗi rõ |
| H2-2 | Nhiều stream đồng thời chung connection: fault/cancel stream được chọn không reset stream khác hoặc đóng connection ngoài scope; stream khác hoàn tất và connection còn dùng được |
| H2-3 | Flow cũ giữ snapshot khi reload/enable/disable; stream mới trên cùng connection dùng snapshot mới; deadline/shutdown dọn tài nguyên; upstream không bị gọi lặp do retry của proxy |
| MT-1 | Listener mTLS nhận client hợp lệ, từ chối thiếu cert, sai CA, hết hạn; không bật mTLS thì client không có cert vẫn dùng TLS được |
| MT-2 | Upstream yêu cầu mTLS nhận certificate Faultline hợp lệ, từ chối thiếu/sai/hết hạn; Faultline vẫn từ chối server sai CA/hostname; kiểm tra một phía và cả hai phía với HTTP/1.1 và HTTP/2 |
| MT-3 | Cấu hình thiếu cert/key, cặp không khớp, CA lỗi hoặc mTLS không TLS bị từ chối; đường dẫn theo file include đúng; thay field hoặc nội dung TLS yêu cầu restart, apply thất bại giữ revision/state/counters |
| MT-4 | Lỗi xác thực phân biệt với fault inject; không tăng applied nếu chưa tới action; binary/Docker đọc được file mount và chạy được cấu hình mTLS, không ghi private key vào log |

Chạy regression HTTP/HTTPS MVP, unit/integration tests, race, vet và build CLI theo [workflow](../README.md#theo-dõi-thực-hiện). Ghi bằng chứng binary/Docker và mọi check chưa chạy.

## REVIEW

Review bảng capability và config compatibility; không diễn giải reset stream như close connection, không bỏ verify TLS, không mặc nhiên giữ danh tính TLS gốc. Kiểm tra retry, body cleanup, clone/digest và tránh duplicate TLS logic giữa HTTP/gRPC.

2026-09-14: cập nhật kế hoạch, chưa hiện thực. Các kiểm tra trên là tiêu chí dự kiến, chưa phải kết quả đã chạy; chỉ Done theo [workflow](../README.md#theo-dõi-thực-hiện).
