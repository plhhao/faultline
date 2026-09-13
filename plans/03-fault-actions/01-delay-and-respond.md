# P07 — Delay và response giả

- Trạng thái: **Done** (2026-09-13)
- Phụ thuộc: [P06](../02-http-proxy/03-flow-lifecycle.md)
- Nguồn: [specific.md](../../specific.md), mục 5.1, 6.
- Nghiệm thu MVP liên quan: AC7, AC8, AC22
- Vùng thay đổi dự kiến: `internal/fault/, internal/proxy/http/, tests/integration/`

## DEFINE

Hiện thực delay ở hai phase và respond trước upstream theo rule đã chọn.

## PLAN → BUILD

1. Delay dùng timer hủy được, tôn trọng flow deadline và tạo backpressure.
2. Respond trả status/body cấu hình với HTTP framing hợp lệ; không gọi upstream.
3. Gắn action outcome vào flow; chỉ tăng applied khi action thực sự bắt đầu tác động, không báo duration hoàn tất khi bị hủy.

Quyết định: `fault.Builtin` dùng capabilities hiện có; CLI truyền executor vào adapter.
Delay dùng timer; `applied=true` từ khi bắt đầu chờ, cancellation vẫn có outcome lỗi.
HTTP adapter bỏ body cho HEAD/204/205/304; response giả có Content-Length khi được phép
để kết thúc hợp lệ cả khi client còn upload. HEAD giữ độ dài representation; 205 có độ dài 0.

## VERIFY

- Đo delay 500 ms với dung sai rõ ràng ở từng phase; cancellation sớm không giữ timer/flow.
- Respond 503 không gọi upstream; cover bodyless response semantics. Chạy action trên HTTP và HTTPS.

## REVIEW

Delay 100% không đồng nghĩa client chắc chắn lỗi; không buffer body chỉ để chờ.

## Kết quả VERIFY/REVIEW

- Full race suite, vet, build và CLI smoke pass; lệnh/bằng chứng chung tại [phase README](README.md#kết-quả).
- Unit tests dùng `testing/synctest` kiểm tra timer hoàn tất/cancel và context đã hủy không áp action.
- Integration kiểm tra delay 500 ms ở hai phase, cả bốn tổ hợp HTTP/HTTPS, dung sai -25 ms/+2 s; upstream nhận request sau delay ở phase trước.
- Respond 503 trả đúng body, không gọi upstream; HEAD/204/205/304 giữ framing hợp lệ qua hai request trên cùng connection. Regression response giả khi upload chưa xong pass.
- Cancellation trước/sau headers và deadline với upload chưa đọc pass. Applied ghi nhận lúc bắt đầu đợi; cancellation không được báo hoàn tất duration.
- Review sửa Content-Length cho early response; delay không đọc toàn bộ body vào RAM. Phát hiện disconnect trong upload chưa đọc có thể trì hoãn tới I/O tiếp theo; deadline/shutdown vẫn giới hạn flow.
