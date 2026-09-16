# P19 — Remote API, revision và quyền thao tác

- Trạng thái: **Done**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 8.1, 11, 13; [phạm vi phase 07](README.md).
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `internal/control/`, `cmd/faultline/`, API/storage/auth và tài liệu triển khai; đường dẫn mới xác định khi thiết kế.

## DEFINE

Mở control service cho một instance trên server test chung, với config lưu bền, revision chống ghi đè và danh tính người thao tác. Quyết định sản phẩm chốt ngày 2026-09-15:

- Có hai chế độ: file/CLI hiện tại và API/UI quản lý config. Trong chế độ API/UI, file chỉ bootstrap khi chưa có dữ liệu; các lần chạy sau lấy config đã apply từ storage.
- Mọi đường ghi trong chế độ API/UI phải qua cùng quy trình revision; CLI reload file không được vượt qua kiểm tra này. Việc chỉnh listener/upstream/TLS thuộc người vận hành, không thuộc API sửa rule của tester.
- Chỉ config đã apply thành công mới trở thành active và được phục hồi sau restart. Draft/validate không đổi runtime. Restart mặc định disabled; không lưu enabled thành trạng thái tự phục hồi. Giữ lựa chọn startup chủ động hiện có.
- Đăng nhập độc lập bằng tài khoản riêng, hai quyền toàn instance: xem; chỉnh/apply/bật-tắt. Tài khoản và cấp quyền do người vận hành quản lý; không yêu cầu màn hình quản trị tài khoản.
- API/UI phục vụ qua HTTPS. Audit có actor, revision, thao tác và outcome; không ghi mật khẩu, session hoặc nội dung nhạy cảm.

## PLAN → BUILD

1. Thiết kế storage và contract hai chế độ: bootstrap config nhiều file, quyền sở hữu dữ liệu, đường dẫn TLS, lưu config/revision/apply result và phục hồi sau restart. Định nghĩa cách người vận hành đổi cấu hình cần restart mà không tạo nguồn ghi cạnh tranh. Chọn cơ chế commit bảo đảm lỗi ghi đĩa/crash không khiến apply báo thành công nhưng restart phục hồi bản khác.
2. Thiết kế API đọc active config/status/capability, validate/draft/apply/enable/disable trên control service hiện có. Draft mang base revision; apply kiểm tra revision atomically với commit, trả conflict có đủ thông tin để xử lý. Giới hạn API tester chỉ sửa rules trên proxy có sẵn.
3. Thực hiện đăng nhập/logout, session hết hạn, lưu mật khẩu dạng hash phù hợp, kiểm tra quyền phía server, bảo vệ thao tác thay đổi trước CSRF và giới hạn thử đăng nhập. Cung cấp quy trình local cho người vận hành tạo/thu hồi tài khoản, đặt lại mật khẩu và gán một trong hai quyền; không có mật khẩu mặc định dùng chung.
4. Tích hợp HTTPS và audit, kể cả thao tác bị từ chối và thất bại. Quy định hành vi khi storage/audit lỗi, giới hạn lưu trữ, và bảo đảm đường CLI/local admin không vô tình vượt contract của chế độ API/UI.
5. Bổ sung unit/integration tests, hướng dẫn binary/Docker, HTTPS, volume lưu bền và quản lý tài khoản. Cập nhật đặc tả/CLI theo contract thực tế trước nghiệm thu.

## VERIFY

Các tiêu chí dưới đây được đối chiếu với kết quả thực tế trong [acceptance.md](acceptance.md).

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P19-AC1 | Chế độ file/CLI giữ hành vi cũ; API/UI bootstrap một lần, lần restart sau dùng config lưu bền. Reload file không vượt revision hoặc âm thầm ghi đè config API. |
| P19-AC2 | Validate/draft lỗi không đổi active revision, config hoặc injection state; API tester từ chối sửa listener/upstream/TLS và thêm/xóa proxy. |
| P19-AC3 | Hai người apply từ cùng base revision: chỉ một thay đổi thành công, bên còn lại nhận conflict; có thể đọc bản active và gửi lại draft sau khi xử lý khác biệt. |
| P19-AC4 | Apply thành công lưu config/revision/result nhất quán; kiểm thử lỗi persistence và crash/restart tại ranh giới commit. Restart phục hồi bản đã commit, mặc định disabled, không tự bật lại do trạng thái trước đó. |
| P19-AC5 | Đăng nhập đúng/sai, logout, session hết hạn, thu hồi tài khoản và hai quyền được kiểm tra ở API; người chưa đăng nhập hoặc chỉ có quyền xem không apply/toggle được. Kiểm chứng CSRF và giới hạn thử đăng nhập. |
| P19-AC6 | Audit thao tác có actor/revision/outcome, bao gồm conflict và từ chối quyền; dữ liệu xác thực không xuất hiện trong response/log; xử lý lỗi audit theo policy đã ghi. |
| P19-AC7 | Apply chỉ ảnh hưởng request mới; no-op không tạo revision/reset counters; thay đổi config reset counters theo contract cũ. Enable/disable không đổi config; disable vẫn báo fault đang chạy. |
| P19-AC8 | Smoke binary và Docker qua HTTPS: đăng nhập, apply, toggle, restart với dữ liệu lưu bền. Tài liệu đủ để bootstrap tài khoản, quản lý quyền và cấu hình HTTPS/volume. |

Chạy tests liên quan, toàn bộ Go tests/race/vet và build CLI theo [workflow dự án](../README.md#theo-dõi-thực-hiện); ghi riêng kết quả binary/Docker và các giới hạn.

## REVIEW

- Không tạo rule engine hoặc validation riêng cho API; mọi đường apply dùng cùng control service.
- Rà soát tính nhất quán giữa persistence và runtime, quyền trên mọi đường ghi, dữ liệu nhạy cảm và startup semantics.
- Đối chiếu mode file/API, thao tác người vận hành và HTTP/gRPC capability hiện có; không mở rộng scope sang nhiều instance hoặc quyền từng proxy.

## Tiến độ

- 2026-09-15: BUILD/VERIFY/REVIEW P19 hoàn tất. API/auth/storage, lệnh user/configure, revision commit và quyền Unix admin đã hiện thực; full Go race, vet, binary và Docker smoke pass. Giới hạn storage/session/socket được ghi trong acceptance và hướng dẫn vận hành. UI E2E thuộc P20 vẫn pending.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện).

## Thiết kế và kết quả hiện thực

Xem [thiết kế chung](README.md#thiết-kế-hiện-thực--2026-09-15), [bằng chứng kiểm chứng](acceptance.md) và [hướng dẫn test/vận hành](../../examples/tester/README.md).
