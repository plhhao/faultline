# P02 — Snapshot và trạng thái injection

- Trạng thái: **Planned**
- Phụ thuộc: [P01](../01-core/01-config-schema.md)
- Nguồn: [specific.md](../../specific.md), mục 6.3, 8, 8.1.
- Nghiệm thu MVP liên quan: AC10, AC12, AC17, AC18, AC19, AC20
- Vùng thay đổi dự kiến: `internal/control/`

## DEFINE

Tạo control service trong process, snapshot config và injection state nhất quán; chưa có admin transport.

## PLAN → BUILD

1. Tạo run ID, revision, control sequence và snapshot bất biến mà mỗi flow giữ đến khi kết thúc.
2. Serialize apply/enable/disable; mặc định disabled, --start-enabled sẽ được wiring ở P04. Enable/disable lặp là no-op và không reset counters.
3. Apply config hiệu lực giống nhau là no-op; thay đổi tạo revision/counter scope mới, giữ injection state. Từ chối toàn bộ thay đổi cần restart.
4. Xác định ownership/lifetime của revision cũ và counters; không giữ lịch sử revision vô hạn.

## VERIFY

- Kiểm tra snapshot cũ không đổi sau apply; lỗi validation hoặc field cần restart không đổi active revision.
- Kiểm tra enable/disable, reload khi disabled, no-op và restart; chạy race tests với acquire/apply/toggle đồng thời.

## REVIEW

Không tạo snapshot gồm config cũ và state mới do đọc rời rạc; flow cũ phải ghi đúng revision.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
