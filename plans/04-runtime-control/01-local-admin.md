# P10 — Kênh quản trị local và CLI runtime

- Trạng thái: **Planned**
- Phụ thuộc: [P02](../01-core/02-runtime-snapshots.md), [P09](../03-fault-actions/03-hold-request-response.md)
- Nguồn: [specific.md](../../specific.md), mục 7, 8.1, 11.
- Nghiệm thu MVP liên quan: AC17, AC18, AC19, AC20
- Vùng thay đổi dự kiến: `cmd/faultline/, internal/control/, tests/integration/`

## DEFINE

Điều khiển process đang chạy qua enable/disable/status, tách khỏi traffic bị inject.

## PLAN → BUILD

1. Chốt transport local, cách định danh instance và flags kết nối; kiểm chứng chạy trên host và trong container.
2. Expose control service qua admin channel giới hạn local; CLI có timeout, exit code và lỗi khi không kết nối được.
3. Status gồm run/revision, state, thời điểm/control sequence, listener readiness và active fault flows; không đợi recorder hoàn chỉnh mới theo dõi flow đang giữ.
4. Kiểm tra --start-enabled và startup disabled từ CLI đến runtime; lệnh lặp no-op.

## VERIFY

- Process test startup→enable→disable; traffic disabled không tiêu thụ nth:1, counter tiếp tục sau enable lại.
- Disable khi có hold: flow cũ tiếp tục, request mới pass-through và status vẫn báo active fault; hai instance không điều khiển nhầm nhau.

## REVIEW

Chưa mở remote admin API; proxy ready không đại diện app ready. Không thêm yêu cầu app gửi header/SDK.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
