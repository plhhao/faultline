# P11 — Reload qua CLI và revision nhất quán

- Trạng thái: **Planned**
- Phụ thuộc: [P10](../04-runtime-control/01-local-admin.md)
- Nguồn: [specific.md](../../specific.md), mục 7, 8, 8.1.
- Nghiệm thu MVP liên quan: AC10, AC11, AC12, AC19
- Vùng thay đổi dự kiến: `cmd/faultline/, internal/control/, internal/config/, tests/integration/`

## DEFINE

CLI gửi config đến process để validate lại và apply atomically; chỉ request mới nhận thay đổi.

## PLAN → BUILD

1. CLI gửi đường dẫn tuyệt đối file config gốc; process tự đọc toàn bộ root/include bằng loader P01a theo mục 7.1. Resolve include theo root và cert theo file chứa proxy; không upload config/cert qua admin channel.
2. Chuẩn bị/validate toàn bộ trước swap; trả revision và thời điểm thực có hiệu lực, lỗi phải giữ nguyên active snapshot.
3. So sánh config hiệu lực, từ chối thay listener/protocol/upstream/TLS/runtime limits; rules và seed theo schema có thể tạo revision mới.
4. Giữ state injection khi reload; no-op giữ counters, revision mới reset mọi rule counter và flow cũ tiếp tục ghi revision cũ.

## VERIFY

- Giữ một request đang chạy rồi reload 0→1: cũ không đổi, mới dùng revision mới, kể cả trên keep-alive.
- Reload sai/field cần restart từ chối toàn bộ; reload tương đương giữ sequence, config đổi reset; race reload/toggle/request.
- MC2/MC4/MC5: sửa rule trong fragment, duplicate ID, glob không match, file lỗi và cây config mount trong container. Chia lại file tương đương là no-op; thêm/xóa proxy cần restart. Không cam kết filesystem transaction cho các file đang được chỉnh đồng thời.

## REVIEW

CLI đọc được file không đủ kết luận apply thành công; không áp dụng một phần hoặc giữ snapshot cũ vô hạn.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
