# P20 — UI chỉnh config và quan sát

- Trạng thái: **Done**
- Phụ thuộc: [P19](01-admin-api.md)
- Nguồn: [specific.md](../../specific.md), mục 2, 8.1, 13; [phạm vi phase 07](README.md).
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: frontend, tích hợp phục vụ UI và tài liệu binary/Docker; đường dẫn xác định khi thiết kế.

## DEFINE

Tester đăng nhập, chọn proxy có sẵn và chỉnh rules/tỷ lệ/tham số fault mà không sửa file trên server. Phạm vi chốt ngày 2026-09-15:

- UI phục vụ một instance có nhiều proxy, dùng đăng nhập độc lập của P19; thể hiện quyền xem hoặc chỉnh/apply/bật-tắt toàn instance.
- Form chỉnh rule dựa trên schema/capability hiện có, gồm HTTP/HTTP2 và gRPC unary cùng faults đã hỗ trợ. Listener/upstream/TLS chỉ xem thông tin phù hợp; thay đổi do người vận hành thực hiện.
- Hiển thị draft, base/active revision, diff trước apply, lỗi field, conflict và apply result. Draft không mất khi validate/apply lỗi hoặc conflict.
- Hiển thị injection state, active faults, counters selected/applied/outcome và dropped events. Không kết luận ứng dụng ready hoặc nghiệp vụ PASS/FAIL từ trạng thái proxy.
- Chưa có quản lý nhiều server, chỉnh hạ tầng proxy, quản trị tài khoản trong UI hoặc lưu/tra cứu traffic dài hạn.

## PLAN → BUILD

1. Chọn UI stack và cách đóng gói/serve cùng binary/Docker; ghi quyết định trước BUILD. Dùng API P19 cho session, quyền, config và capability; không yêu cầu người dùng chốt framework.
2. Xây đăng nhập/logout, trạng thái session và giao diện theo quyền. Xây form chọn proxy, chỉnh rules và tham số, trình bày đơn vị và probability rõ ràng; validation phía server là nguồn quyết định.
3. Xây luồng draft → validate → diff → apply → đọc active revision. Khi conflict, giữ draft và cho so sánh với active mới trước khi gửi lại; không tự ghi đè thay đổi người khác.
4. Hiển thị status/counters và enable/disable. Phân biệt fault được chọn, đã thực thi và kết quả quan sát được; giải thích counters theo revision và cảnh báo dữ liệu chưa cập nhật khi mất kết nối.
5. Bổ sung E2E và hướng dẫn tester cho đăng nhập, sửa/apply, conflict, trạng thái injection và giới hạn quan sát; kiểm chứng UI trong bản binary/Docker.

## VERIFY

- 2026-09-16, sau nghiệm thu: thêm favicon SVG và logo trước FAULTLINE; asset/session test PASS. Bổ sung giao diện này chờ người dùng xác nhận hiển thị sau rebuild, không thay đổi kết quả nghiệm thu các luồng chức năng.

- 2026-09-16: người dùng xác nhận U1–U9 PASS, hoạt động ổn định. Mục vận hành/Docker và checklist bổ sung path pattern/diff chưa được xác nhận trong báo cáo này.

- Diff coi `null` và field vắng mặt là tương đương cho các tham số Fault tùy chọn; vẫn hiển thị thay đổi giá trị thật, kể cả `0` và body rỗng. Có Node regression test; tester xác nhận UI sau build/restart.

Các tiêu chí dưới đây được đối chiếu với kết quả thực tế trong [acceptance.md](acceptance.md).

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P20-AC1 | E2E đăng nhập → chọn proxy/rule → sửa probability → validate → xem diff → apply → thấy active revision mới và thay đổi hành vi traffic, với HTTP và gRPC đại diện. |
| P20-AC2 | Config lỗi hiển thị lỗi tương ứng, giữ draft và active cũ; hai phiên sửa đồng thời tạo conflict, giữ draft và cho người dùng xử lý trước khi apply lại. |
| P20-AC3 | Quyền xem không có thao tác ghi khả dụng; quyền chỉnh thực hiện được apply/toggle. Logout/session hết hạn xử lý rõ ràng; gọi trực tiếp API vẫn bị kiểm tra quyền bởi P19. |
| P20-AC4 | Disable khi còn hold thể hiện injection tắt cho request mới và fault cũ còn chạy; selected/applied/outcome, dropped events và counters theo revision không bị diễn giải thành kết quả nghiệp vụ hoặc app ready. Mất kết nối không hiển thị dữ liệu cũ như trạng thái mới xác nhận. |
| P20-AC5 | UI chạy qua HTTPS trong binary và Docker; rule forms phản ánh capability hiện có, listener/upstream/TLS không chỉnh được. Không mất draft do validate/apply lỗi và không lộ dữ liệu xác thực trong giao diện/log. |

Chạy frontend checks theo stack được chọn, API/UI E2E và smoke binary/Docker; chạy Go regression khi thay đổi tích hợp phía Go. Ghi lệnh và kết quả thật khi triển khai.

## REVIEW

- Không sao chép validation semantics thành engine độc lập ở frontend.
- Rà soát diff/revision/conflict, quyền thao tác, ngôn ngữ mô tả trạng thái và cách hiển thị dữ liệu cũ.
- Kiểm tra phạm vi chỉ chỉnh rule trên proxy có sẵn, đóng gói sử dụng được và không thêm dashboard lịch sử traffic ngoài yêu cầu.

## Tiến độ

- 2026-09-15: BUILD UI nhúng hoàn tất; JavaScript syntax, asset delivery qua HTTPS và setup fixture đã kiểm tra. REVIEW code đã sửa draft thay đổi trong lúc validate/apply và giữ nội dung header JSON lỗi khi đổi proxy. VERIFY tương tác/hiển thị browser giao người dùng theo yêu cầu; P20 giữ In progress cho tới khi checklist UI được xác nhận.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện).

## Thiết kế và kết quả hiện thực

Xem [thiết kế chung](README.md#thiết-kế-hiện-thực--2026-09-15), [bằng chứng kiểm chứng](acceptance.md) và [hướng dẫn test/vận hành](../../examples/tester/README.md).

## Phản hồi nghiệm thu U1–U4

- Người dùng báo **U1, U2, U3, U4 PASS**, kèm yêu cầu tối ưu UI; không suy ra các case còn lại đã pass.
- Đã bổ sung badge injection sát nút Enable/Disable trong Runtime, pending/confirmed state cho toggle; chặn phản hồi status cũ ghi đè trong lúc toggle. Theo phản hồi tiếp theo, bỏ header cố định; thông báo nổi xác nhận tự ẩn sau 5 giây, lỗi giữ tới khi đóng. Phase giới hạn theo action/direction; giải thích method optional và observation retention trên UI.
- JavaScript syntax và kiểm tra lựa chọn phase cho 7 action × 2 direction PASS; asset/session test PASS. Chưa kiểm thử browser các thay đổi mới; xem [checklist kiểm tra lại](../../examples/tester/README.md#kiểm-tra-lại-ui-sau-phản-hồi-u1u4). P20 giữ In progress.

- Theo yêu cầu tiếp theo: bỏ dòng trạng thái injection lớn ở đầu Runtime; chỉ giữ badge cạnh Enable/Disable. JS syntax và tham chiếu DOM đã kiểm tra; hiển thị browser chờ người dùng xác nhận.

- 2026-09-16: thêm highlight diff field/rule theo proxy ID + rule ID, dấu thêm/xóa/đổi và position. Rename hiện remove/add; duplicate ID cảnh báo không ghép mơ hồ. Logic diff/render có Node tests; browser acceptance chờ checklist mới trong hướng dẫn tester.

## Nghiệm thu hoàn tất — 2026-09-16

Người dùng xác nhận mọi case trong [hướng dẫn tester](../../examples/tester/README.md) PASS: U1–U9, vận hành config, Docker, path pattern và highlight diff. Kết hợp bằng chứng tự động đã ghi, P19/P20/P20a và Phase 7 được đánh dấu **Done**. Các ghi chú pending bên trên là lịch sử trước nghiệm thu. Lần cập nhật này chỉ sửa tài liệu, không chạy lại runtime tests.
