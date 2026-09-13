# P11 — Reload qua CLI và revision nhất quán

- Trạng thái: **Done** (2026-09-13)
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

Quyết định: CLI chỉ gửi absolute root path; server gọi `control.ReloadFile`.
Các lệnh mutation được serialize trong admin; status trả snapshot control và
counters đang quan sát. Timeout CLI có thể xảy ra sau khi mutation đã áp dụng;
CLI không tự retry, dùng status để kiểm tra lại.

## VERIFY

- Giữ một request đang chạy rồi reload 0→1: cũ không đổi, mới dùng revision mới, kể cả trên keep-alive.
- Reload sai/field cần restart từ chối toàn bộ; reload tương đương giữ sequence, config đổi reset; race reload/toggle/request.
- MC2/MC4/MC5: sửa rule trong fragment, duplicate ID, glob không match, file lỗi và cây config mount trong container. Chia lại file tương đương là no-op; thêm/xóa proxy cần restart. Không cam kết filesystem transaction cho các file đang được chỉnh đồng thời.

## REVIEW

CLI đọc được file không đủ kết luận apply thành công; không áp dụng một phần hoặc giữ snapshot cũ vô hạn.

## Kết quả VERIFY/REVIEW

- CLI process test pass: old request giữ revision 1; sau reload, request mới trên connection keep-alive dùng revision 2. State injection được giữ.
- Admin tests pass: thay rule fragment; chia lại file tương đương no-op; duplicate proxy ID, missing glob, config lỗi, upstream/runtime đổi bị từ chối và giữ snapshot/counters.
- Revision mới reset rule counter, toggle không reset; concurrent reload/toggle/request pass dưới race detector. Validation và restart compatibility dùng lại control/config hiện có.
- REVIEW: chỉ gửi absolute root path, không upload config/cert. CLI timeout không đủ kết luận mutation thất bại; không retry tự động. Không hứa filesystem transaction khi files đang đổi.
- Host MC2/MC4 và container MC5 đã pass. [Test opt-in](README.md#kiểm-chứng-container) chạy 3 lần trên Docker/OrbStack: root/includes/TLS mount, sửa rule tạo revision mới giữ enabled, no-op giữ revision và duplicate ID giữ snapshot cũ. Test đợi validation thấy đủ fixture qua bind mount trước mutation.
