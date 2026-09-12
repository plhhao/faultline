# P11 — Reload qua CLI và revision nhất quán

- Trạng thái: **Planned**
- Phụ thuộc: [P10](../04-runtime-control/01-local-admin.md)
- Nguồn: [specific.md](../../specific.md), mục 7, 8, 8.1.
- Nghiệm thu MVP liên quan: AC10, AC11, AC12, AC19
- Vùng thay đổi dự kiến: `cmd/faultline/, internal/control/, internal/config/, tests/integration/`

## DEFINE

CLI gửi config đến process để validate lại và apply atomically; chỉ request mới nhận thay đổi.

## PLAN → BUILD

1. Truyền tài liệu và ngữ cảnh đường dẫn config rõ ràng; server resolve theo thư mục config trong filesystem của process, không upload cert/key.
2. Chuẩn bị/validate toàn bộ trước swap; trả revision và thời điểm thực có hiệu lực, lỗi phải giữ nguyên active snapshot.
3. So sánh config hiệu lực, từ chối thay listener/protocol/upstream/TLS/runtime limits; rules và seed theo schema có thể tạo revision mới.
4. Giữ state injection khi reload; no-op giữ counters, revision mới reset mọi rule counter và flow cũ tiếp tục ghi revision cũ.

## VERIFY

- Giữ một request đang chạy rồi reload 0→1: cũ không đổi, mới dùng revision mới, kể cả trên keep-alive.
- Reload sai/field cần restart từ chối toàn bộ; reload tương đương giữ sequence, config đổi reset; race reload/toggle/request.

## REVIEW

CLI đọc được file không đủ kết luận apply thành công; không áp dụng một phần hoặc giữ snapshot cũ vô hạn.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
