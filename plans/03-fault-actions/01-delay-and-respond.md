# P07 — Delay và response giả

- Trạng thái: **Planned**
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

## VERIFY

- Đo delay 500 ms với dung sai rõ ràng ở từng phase; cancellation sớm không giữ timer/flow.
- Respond 503 không gọi upstream; cover bodyless response semantics. Chạy action trên HTTP và HTTPS.

## REVIEW

Delay 100% không đồng nghĩa client chắc chắn lỗi; không buffer body chỉ để chờ.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
