# P18 — Truncate, throttle và fault bổ sung

- Trạng thái: **Deferred**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 5.2, 5.4, 6.4.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `internal/fault/, internal/proxy/http/, tests/integration/`

## DEFINE

Chọn một fault có use case trước mỗi đợt triển khai; ưu tiên truncate khi cần kiểm thử body không hoàn tất.

## PLAN → BUILD

1. Định nghĩa direction, điểm cắt byte, framing và transport termination cho truncate; không mô tả là bỏ byte âm thầm trong TCP.
2. Định nghĩa bytes/time, backpressure và cancellation nếu chọn throttle; reset/half-close cần capability riêng.
3. Mở schema/capability đúng fault được chọn. Weighted selection/action chaining cần thiết kế riêng nếu phát sinh, không tự gộp vào plan này.

## VERIFY

- Truncate request/response với Content-Length và chunked: phía nhận phát hiện incomplete body; ghi hạn chế close-delimited.
- Kiểm tra HTTP/HTTPS, cut boundary và cleanup; throttle đo tốc độ với dung sai đã định.

## REVIEW

Không cam kết packet loss/reordering ở cấp IP hoặc thêm toàn bộ fault chỉ vì có trong roadmap.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
