# P10 — Kênh quản trị local và CLI runtime

- Trạng thái: **Done** (2026-09-13)
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

Quyết định: HTTP/JSON qua Unix socket, mặc định `/tmp/faultline-<uid>/admin.sock`;
`--admin-socket` chọn instance khác. Thư mục socket phải riêng tư (0700), socket
0600; không xóa socket có sẵn. CLI `--timeout` mặc định 5s, không retry mutation.
Admin chạy ngoài HTTP adapter gây lỗi; readiness chỉ xác nhận listener đã bind.

## VERIFY

- Process test startup→enable→disable; traffic disabled không tiêu thụ nth:1, counter tiếp tục sau enable lại.
- Disable khi có hold: flow cũ tiếp tục, request mới pass-through và status vẫn báo active fault; hai instance không điều khiển nhầm nhau.

## REVIEW

Chưa mở remote admin API; proxy ready không đại diện app ready. Không thêm yêu cầu app gửi header/SDK.

## Kết quả VERIFY/REVIEW

- Host process tests pass: startup disabled không tiêu thụ nth:1; enable/disable/no-op giữ sequence và counter; restart tạo run mới và disabled.
- Status trả readiness, control timestamps/sequence, current-revision rule counters và run counters; hold cũ còn active sau disable. Hai socket điều khiển đúng hai instance.
- Socket 0600 trong private directory, không ghi đè socket đang dùng; CLI timeout và lỗi kết nối được kiểm thử. Admin không đi qua adapter gây lỗi.
- Test `TestClosedStdoutKeepsAdminAvailable` pass: closed event pipe không giết server, admin vẫn truy cập được và write_errors/dropped tăng.
- REVIEW: local-only, readiness chỉ là listener bind. Snapshot/counters được lấy trong khi traffic có thể tiếp tục; không cam kết snapshot metrics toàn hệ thống tại một instant.
- Container smoke test pass 3 lần trên Docker/OrbStack: startup disabled/ready, CLI enable/status/disable, reload và stop exit code 0; xem [bằng chứng container](README.md#kiểm-chứng-container).
