# Phase 07 — API và UI cho tester

Phạm vi: **Sau MVP**. Trạng thái: **Done**.

Tester quản lý cấu hình trên một server test chung qua cùng control service. Phạm vi được người dùng chốt ngày **2026-09-15**; API, UI nhúng và path pattern đã hoàn tất; người dùng xác nhận mọi case trong hướng dẫn PASS ngày 2026-09-16.

## Quyết định đã chốt

- Một instance có nhiều proxy; giữ chế độ file/CLI hiện tại cho dev và CI.
- Chế độ API/UI dùng file để khởi tạo lần đầu, sau đó config đã apply lưu bền trên server là nguồn chính. CLI reload file không được ghi đè ngoài quy trình revision.
- Restart khôi phục config đã apply gần nhất; injection mặc định tắt, không khôi phục trạng thái enabled của lần chạy trước. Giữ khả năng chủ động bật lúc startup theo contract hiện có.
- Đăng nhập độc lập, mỗi người có danh tính riêng. Hai quyền áp dụng toàn instance: **xem** và **chỉnh/apply/bật-tắt**. API/UI truy cập qua HTTPS; không phụ thuộc SSO.
- UI cho phép chỉnh rules, tỷ lệ và tham số fault trên proxy có sẵn; xem diff, validate, apply, revision, trạng thái và counters. Listener/upstream/TLS do người vận hành cấu hình.
- Chưa gồm quản lý nhiều server, phân quyền từng proxy, lịch sử traffic dài hạn, assertions hoặc scenario scheduler.

## Các plan và thứ tự

| Plan | Tính năng | Phụ thuộc | Trạng thái |
| --- | --- | --- | --- |
| [P19](01-admin-api.md) | Remote API, persistence, revision, đăng nhập và quyền thao tác | P15 | Done |
| [P20](02-tester-ui.md) | UI chỉnh config và quan sát | P19 | Done |
| [P20a](03-path-pattern.md) | Path pattern cho rule và form UI | P19, form rule P20 đã BUILD | Done |

Triển khai P19 → P20 → P20a; P20a có thể bắt đầu trên form hiện có trong khi chờ nghiệm thu tương tác P20. Người hiện thực tự chọn UI stack, API/storage và cách đóng gói trong phạm vi đã chốt; ghi thiết kế cụ thể vào plan trước BUILD, không cần chốt lại các quyết định sản phẩm trên.

## Điều kiện hoàn tất phase

- Đạt các tiêu chí P19-AC1–AC8 và P20-AC1–AC5 và P20a-AC1–AC6 trong từng plan, có bằng chứng kiểm thử thực tế.
- Luồng tester đăng nhập → sửa tỷ lệ → validate → xem diff → apply → thấy revision mới chạy được trên binary và Docker.
- Giữ semantics request mới, no-op reload, reset counters theo revision và disable không hủy fault đang chạy; không thay đổi engine riêng cho UI/API.
- Có hướng dẫn khởi tạo tài khoản, HTTPS, lưu dữ liệu qua restart, chế độ file/API và giới hạn vận hành; hoàn tất VERIFY/REVIEW trước khi đánh dấu Done.

## Tiến độ

- 2026-09-15: P20a đã BUILD schema/engine/API/UI và PASS Go race, vet, CLI build/validate cùng JS syntax; chờ checklist tương tác path pattern từ người dùng.

- 2026-09-15: thêm P20a cho path pattern; contract đề xuất dùng `match.path_pattern` với `:param` một đoạn, giữ exact path và query forwarding hiện tại. Chưa hiện thực.

- 2026-09-15: P19 hoàn tất backend và kiểm chứng API/binary/Docker. P20 đã BUILD UI nhúng và kiểm tra cú pháp/assets; chưa đánh dấu Done do chờ người dùng chạy [checklist UI](../../examples/tester/README.md).
- Kết quả/lệnh/giới hạn: [acceptance.md](acceptance.md). Không coi API smoke là browser E2E.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Thiết kế hiện thực — 2026-09-15

- UI HTML/CSS/JavaScript nhúng bằng `go:embed`, cùng HTTPS listener của API; không cần Node ở runtime.
- `--data-dir` bật managed mode: file bootstrap được hợp nhất và chuẩn hóa đường dẫn; `state.json` lưu config, revision, apply result và 1.000 audit gần nhất bằng temp-file → fsync → rename → fsync directory. Khóa instance ngăn hai process ghi cùng storage. Lỗi durability sau rename khiến instance dừng để phục hồi từ storage trước khi nhận traffic tiếp.
- Unix admin chỉ đọc status trong managed mode. Người vận hành dừng instance và dùng `configure --data-dir --config` để thay đổi hạ tầng. Chế độ file giữ nguyên.
- Tài khoản local qua lệnh `user`, mật khẩu đọc từ stdin, PBKDF2-HMAC-SHA256 (600.000 vòng, salt riêng); session cookie Secure/HttpOnly/SameSite, CSRF token, hết hạn tuyệt đối sau 8 giờ. Tối đa 256 session, 20 lần đăng nhập/phút toàn instance. Đổi mật khẩu/quyền hoặc xóa tài khoản thu hồi session khi request kế tiếp kiểm tra file tài khoản.
- Draft giữ trong bộ nhớ tab, kèm base revision; không lưu qua refresh/đóng tab. API nhận toàn bộ rules của các proxy hiện có, không nhận trường hạ tầng. Validate phía server; conflict yêu cầu so sánh và chủ động chọn base mới trước apply lại.
- Kiểm thử tương tác/hiển thị UI giao người dùng theo yêu cầu; ghi kết quả pending cho tới khi người dùng xác nhận. Agent chạy kiểm thử API, persistence, quyền và binary/Docker, cung cấp hướng dẫn test riêng.

## Nghiệm thu hoàn tất — 2026-09-16

Người dùng xác nhận mọi case trong [hướng dẫn tester](../../examples/tester/README.md) PASS: U1–U9, vận hành config, Docker, path pattern và highlight diff. Kết hợp bằng chứng tự động đã ghi, P19/P20/P20a và Phase 7 được đánh dấu **Done**. Các ghi chú pending bên trên là lịch sử trước nghiệm thu. Lần cập nhật này chỉ sửa tài liệu, không chạy lại runtime tests.
