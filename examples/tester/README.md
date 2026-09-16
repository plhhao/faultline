# Phase 7 — Hướng dẫn kiểm thử UI thủ công

API và các kiểm tra tự động được ghi tại [bằng chứng Phase 7](../../plans/07-tester-experience/acceptance.md). Người dùng xác nhận **mọi case trong hướng dẫn PASS ngày 2026-09-16**, gồm U1–U9, vận hành config, Docker, path pattern và highlight diff. Không cần gửi mật khẩu, cookie hoặc file `users.json`.

## 1. Chuẩn bị một lần

Từ thư mục repository, cần Go, Python 3 và OpenSSL. Script tạo binary, config HTTP + gRPC, certificate localhost có hạn 14 ngày và hai tài khoản riêng. Thư mục mặc định phải chưa tồn tại; muốn chạy lại, chọn thư mục mới bằng `--directory`.

```sh
rtk proxy python3 examples/tester/setup.py
```

Nhập mật khẩu cho `tester` (editor) và `viewer` (chỉ xem), mỗi mật khẩu tối thiểu 12 byte. Password được nhập ẩn và truyền qua stdin, không nằm trong command line.

Các lệnh dưới đây dùng `/tmp/faultline-phase7`. Nếu chọn đường dẫn khác, thay tương ứng.

## 2. Khởi động

Terminal 1 — HTTP dependency:

```sh
rtk proxy go run ./examples/http/paymentdemo/cmd serve --listen 127.0.0.1:9000
```

Terminal 2 — gRPC dependency:

```sh
rtk proxy go run ./examples/grpc/unary/cmd -mode server -address 127.0.0.1:9001
```

Terminal 3 — Faultline với HTTPS UI/API:

```sh
rtk proxy /tmp/faultline-phase7/faultline serve   --config /tmp/faultline-phase7/config.yaml   --data-dir /tmp/faultline-phase7/data   --admin-socket /tmp/faultline-phase7/admin.sock   --api-listen 127.0.0.1:8443   --api-cert /tmp/faultline-phase7/cert.pem   --api-key /tmp/faultline-phase7/key.pem
```

Mở **https://localhost:8443**. Certificate do script tự ký: cho phép certificate localhost trong trình duyệt test hoặc tin cậy `cert.pem` trong môi trường test. Khi triển khai server dùng chung, thay bằng certificate được các máy tester tin cậy và hostname đúng. Không cần cấu hình SSO.

Đăng nhập `tester`. Có hai proxy: `payment` và `echo`; injection ban đầu **disabled**.

## 3. Checklist UI chính

### U1 — Đăng nhập và quyền xem

- [x] Sai mật khẩu: báo lỗi, không vào workspace.
- [x] Đúng tài khoản `tester`: thấy quyền editor, hai proxy, revision hiện hành.
- [x] Mở cửa sổ riêng tư, đăng nhập `viewer`: xem status/config/counters được; không thể sửa rules, validate, apply hoặc bật/tắt injection.
- [x] Logout rồi thao tác: phải đăng nhập lại.

### U2 — HTTP: sửa → validate → xem diff → apply

1. Chọn `payment`, rule `payment-fault`; thay Probability từ `0` thành `1` (= 100%).
2. Chọn **Validate draft & review diff**. So sánh Active rules và Draft rules.
3. Chọn **Apply reviewed draft**.
4. Bấm **Enable injection** rồi gọi từ terminal khác:

```sh
rtk proxy curl -i http://127.0.0.1:8080/healthz
```

- [x] Trước validate không thể apply; diff thể hiện probability đã đổi.
- [x] Apply thành công tăng revision, hiện kết quả apply; không tự bật injection.
- [x] Khi enabled, request nhận HTTP **503** và body `Injected by Faultline`.
- [x] Counters selected/applied tăng. Mở **Observed outcomes** thấy kết quả quan sát; đây không phải kết luận nghiệp vụ.
- [x] Disable rồi gọi lại: HTTP **200** từ upstream.
- [x] Validate/apply không sửa gì: báo no-op, revision giữ nguyên.

### U3 — Config lỗi và draft

- [x] Nhập Probability `2`; validate báo đường dẫn field `select.probability`, active revision giữ nguyên, giá trị draft vẫn là `2`.
- [x] Nhập JSON header sai; không thể validate/apply; đổi proxy rồi quay lại vẫn giữ phần đang nhập.
- [x] Sửa lỗi rồi validate lại được.
- [x] Add rule, đổi action, thay tham số, reorder và remove hoạt động; trường lỗi không làm crash giao diện.
- [x] Không có ô chỉnh listener/upstream/TLS hoặc nút thêm/xóa proxy.

Draft giữ trong **bộ nhớ tab**. Reload/đóng tab mất draft; trình duyệt có thể hỏi xác nhận khi có thay đổi chưa apply. Không cam kết khôi phục draft sau crash trình duyệt.

### U4 — Hai người sửa đồng thời

1. Hai cửa sổ/tab editor cùng mở revision R (có thể dùng cùng tài khoản để test conflict).
2. A đổi probability và apply; B giữ draft khác ở base R.
3. B validate/apply; hoặc đợi polling status cập nhật rồi validate.

- [x] B nhận conflict, draft vẫn còn; active revision đã là R+1.
- [x] **Compare with latest active** chỉ cập nhật phía active, không mất draft.
- [x] Sau khi so sánh, chủ động chọn **Use latest revision as draft base**: có xác nhận rằng toàn bộ draft rules có thể thay các chỉnh sửa của người khác.
- [x] Validate lại rồi apply thành công; không có tự ghi đè ngầm.

### U5 — Disable khi còn hold

1. Với `payment`, đổi action thành `hold_response`, Phase là `after_upstream_headers`, Maximum hold `15000` ms, probability `1`; validate/apply rồi enable.
2. Chạy curl dưới đây và trong lúc nó đang chờ, bấm **Disable injection**:

```sh
rtk proxy curl --max-time 20 -i http://127.0.0.1:8080/healthz
```

- [x] Trước disable thấy `active fault flows` tăng (status cập nhật mỗi 5 giây).
- [x] Sau disable, injection tắt nhưng flow cũ vẫn đang hold tới giới hạn/hủy; counters phản ánh điều đó.
- [x] Request mới từ một terminal khác nhận HTTP 200 ngay.
- [x] UI không nói ứng dụng ready chỉ vì proxy listeners ready.

### U6 — gRPC

Chọn `echo`, giữ action delay 2.000 ms, thay probability `1`, validate/apply và enable. Gọi với deadline 500 ms:

```sh
rtk proxy go run ./examples/grpc/unary/cmd -mode client   -address 127.0.0.1:8081 -timeout 500ms -size 1024
```

- [x] Request lỗi deadline; disable rồi gọi lại thành công.
- [x] gRPC không có lựa chọn `respond` hoặc `close_connection`.
- [x] Có thể chọn truncate/throttle với direction và bytes/rate; server báo lỗi field nếu phase không tương ứng direction.

### U7 — Restart, hết session và mất kết nối

1. Ghi active revision và một rule đã apply; bật injection.
2. Dừng Faultline bằng Ctrl+C trong terminal 3; chờ hơn 5 giây.
3. Chạy lại đúng lệnh terminal 3, cùng data directory.

- [x] Khi server dừng: UI ghi rõ status unavailable/stale, không coi dữ liệu cũ là trạng thái mới xác nhận.
- [x] Sau restart phải đăng nhập lại; config/revision đã apply được khôi phục, injection **disabled**.
- [x] Draft đang mở trong tab không mất khi session hết hạn hoặc server mất kết nối.
- [x] Sửa file bootstrap rồi restart không ghi đè config managed đã lưu (không dùng file này để đổi runtime).

### U8 — Audit và thu hồi quyền

- [x] Mở **Recent administration audit**, Refresh audit: thấy actor, operation, revision, outcome; không có mật khẩu hoặc session token.
- [x] Trong khi `viewer` đang đăng nhập, chạy lệnh dưới rồi refresh status: session bị từ chối.

```sh
rtk proxy /tmp/faultline-phase7/faultline user   --data-dir /tmp/faultline-phase7/data --name viewer --delete
```

Đặt lại mật khẩu hoặc đổi quyền dùng cùng lệnh `user --role viewer|editor`, đọc password từ stdin. Ví dụ trên zsh/bash, nhập ẩn trong shell con:

```sh
rtk proxy bash -c 'read -r -s -p "New password: " tester_password; printf "\n" >&2; printf "%s" "$tester_password" | /tmp/faultline-phase7/faultline user --data-dir /tmp/faultline-phase7/data --name viewer --role viewer'
```

Session có hạn tuyệt đối 8 giờ; đổi password/quyền hoặc xóa user có hiệu lực ở request kế tiếp. Giới hạn 20 login/phút **toàn instance** và 256 session; nếu test nhiều lần sai liên tiếp, đợi một phút.

### U9 — Hiển thị

- [x] Desktop và cửa sổ hẹp: nội dung không đè nhau, form/diff đọc được.
- [x] Dùng Tab/Enter để đăng nhập, chọn proxy, validate và apply được.
- [x] Các nút báo trạng thái hợp lý khi server lỗi; Apply không làm mất draft do request thất bại.

## 4. Vận hành config và dữ liệu

- File mode: bỏ `--data-dir`, CLI reload/enable/disable hoạt động như trước.
- Managed mode: CLI Unix chỉ đọc status; mọi thao tác tester qua HTTPS có xác thực. API payload chỉ chứa `base_revision` và danh sách `{id, rules}` cho toàn bộ proxy hiện có.
- `data/state.json`: config hợp nhất, revision, last apply result và **1.000 audit gần nhất**; không phải traffic history. `data/users.json`: hash mật khẩu và quyền. Giữ thư mục private 0700; sao lưu như dữ liệu nhạy cảm vì config có thể chứa header/body test.
- Muốn đổi listener/upstream/TLS: dừng instance, chuẩn bị **toàn bộ config thay thế**, chạy dưới đây rồi start lại. Lệnh bị từ chối nếu instance còn chạy. Không dùng một file bootstrap cũ nếu muốn giữ rules mới nhất; trích trường `config` trong `state.json` làm điểm xuất phát khi instance đã dừng.

```sh
rtk proxy /tmp/faultline-phase7/faultline configure   --data-dir /tmp/faultline-phase7/data --config /path/to/replacement.yaml
```

- API/session cần hoạt động qua HTTPS trực tiếp; reverse proxy nếu dùng phải kết nối HTTPS đến Faultline và giữ Host/Origin phù hợp. Không có chế độ tin header danh tính từ proxy.
- Sau khi binary bị SIGKILL/crash, Unix socket cũ có thể còn trên đĩa. Chỉ khi đã xác nhận process cũ dừng, xóa riêng `admin.sock` rồi khởi động lại; không xóa state/users. Docker dùng tmpfs `/tmp` nên socket runtime không nằm trên volume dữ liệu.
- TLS proxy data-plane giữ quy tắc phase 6: thay certificate cần restart. Lưu đường dẫn tuyệt đối đã chuẩn hóa; khi chuyển container/máy cần dùng đường dẫn khả dụng trong runtime mới.
- Lỗi storage/audit trước commit: không apply/toggle. Lỗi fsync directory sau rename là commit chưa xác định chắc về durability: instance dừng, cần restart và đọc revision active trước khi thử lại. Nếu client mất response ngay sau apply, cũng đọc lại active trước khi retry.

## 5. Docker (tùy chọn cho bạn; có smoke test tự động)

Build image:

```sh
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:phase7 .
```

Chuẩn bị thư mục riêng như bước 1 bằng `--directory /tmp/faultline-phase7-docker`. Trong bản config này, đổi listen thành `0.0.0.0:8080` / `0.0.0.0:8081` và upstream thành `http://host.docker.internal:9000` / `http://host.docker.internal:9001`. Dependency trên host cần listen địa chỉ container truy cập được (ví dụ `0.0.0.0`). Chưa bootstrap state trước khi sửa file. Docker dùng cùng UID/GID với chủ thư mục bind mount; image vẫn hỗ trợ UID 65532 mặc định nếu volume được cấp ownership tương ứng.

```sh
rtk proxy docker run --rm --name faultline-phase7   --user "$(id -u):$(id -g)"   --tmpfs /tmp:mode=1777   --add-host host.docker.internal:host-gateway   -p 127.0.0.1:8443:8443 -p 127.0.0.1:8080:8080 -p 127.0.0.1:8081:8081   -v /tmp/faultline-phase7-docker:/fixture   faultline:phase7 serve --config /fixture/config.yaml   --data-dir /fixture/data --api-listen 0.0.0.0:8443   --api-cert /fixture/cert.pem --api-key /fixture/key.pem
```

`--tmpfs /tmp:mode=1777` cho UID của host tạo admin socket trong container và tránh giữ socket cũ sau khi container dừng.

Dừng bản binary trước nếu dùng cùng port. Truy cập cùng URL UI, test U2 và U7; thư mục bind mount giữ config qua restart. Không dùng chung data directory đồng thời cho binary và Docker.

## 6. Gửi kết quả

Có thể trả lời ngắn theo mẫu:

```text
Browser / OS:
Binary hoặc Docker:
U1: pass/fail
U2: pass/fail
U3: pass/fail
U4: pass/fail
U5: pass/fail
U6: pass/fail
U7: pass/fail
U8: pass/fail
U9: pass/fail
Lỗi: bước tái hiện, kết quả mong đợi/thực tế, thông báo lỗi.
```

Nếu cần ưu tiên thời gian: chạy **U1–U5 và U7** trước. Gửi lỗi gặp đầu tiên; không cần hoàn tất mọi mục rồi mới phản hồi.

## P20a — Kiểm thử path pattern

Bản binary cần build lại sau thay đổi. Managed mode đã có dữ liệu sẽ giữ config đã apply; rule mẫu mới trong file bootstrap không tự nhập vào instance cũ. Có thể thêm rule trực tiếp trên UI.

1. Đăng nhập editor, chọn proxy `payment`, thêm rule `payment-detail`: Enabled `true`, Path match `Pattern`, Path pattern `/payment/:id`, HTTP method `GET`, Probability `1`, action `respond`, phase `before_upstream_request`, status `503`, body `Path pattern matched`.
2. Validate → xem diff có `Match.PathPattern` → Apply → bật injection. Chạy:

   ```sh
   curl -i 'http://127.0.0.1:8080/payment/123?x=1&x=2'
   curl -i 'http://127.0.0.1:8080/payment/history'
   curl -i 'http://127.0.0.1:8080/payment/123/items'
   curl -i 'http://127.0.0.1:8080/payment/123/'
   ```

   Hai request đầu trả `503` với body đã đặt; hai request sau chuyển upstream (fixture có thể trả `404`). Không dùng `respond` để kết luận query đã được forward.
3. Thêm rule exact `/payment/history`, respond `200`, body `history rule`, probability `1`, đặt **trước** pattern. Apply: history trả `200`, `/payment/123` vẫn `503`. Đổi probability rule exact thành `0`: history chuyển upstream, không rơi xuống pattern. Đổi Enabled exact thành `false`: history lại `503` từ pattern.
4. Nhập pattern lỗi `/payment/*`, `payment/:id`, `/:id/:id`: Validate báo lỗi `match.path_pattern`, draft được giữ, active revision không đổi. Kiểm tra để trống ô pattern rồi chuyển proxy và Validate vẫn báo thiếu path.
5. Chuyển Pattern → Exact: nhập `/payment/:id`, validate/apply; diff phải có `PathPattern` rỗng và `Path` mới. `/payment/123` không khớp; `/payment/:id` khớp literal. Chuyển Any thì cả hai trường rỗng. Kiểm tra đổi proxy không mất loại matcher đang chọn khi ô đang trống.
6. Đăng nhập viewer: loại path và giá trị chỉ xem. Dùng hai phiên editor để xác nhận conflict giữ draft pattern như checklist conflict hiện có.
7. Restart managed server: pattern đã apply được khôi phục, injection mặc định tắt. Bật lại và kiểm tra traffic. Không cần restart khi chỉ sửa rule qua Apply.

Percent-encoding: `/payment/a%2Fb` có path decode `/payment/a/b` nên không khớp `/payment/:id`; `/payment/%252F` khớp vì chỉ decode một lần. Query không dùng để match; test tự động HTTP/1 và HTTP/2 kiểm tra raw query tới upstream với key lặp, giá trị rỗng và percent-encoding.

Gửi kết quả từng bước (PASS/FAIL), revision và lỗi nếu có. Người dùng đã xác nhận toàn bộ checklist P20a PASS ngày 2026-09-16.

## Kiểm tra lại UI sau phản hồi U1–U4

Người dùng đã báo U1, U2, U3 và U4 PASS. Sau khi build lại và tải lại trang (apply hoặc lưu lại nội dung draft trước vì refresh làm mất draft), kiểm tra các thay đổi sau:

- Badge **Injection ON/OFF** nằm sát nút Enable/Disable trong Runtime; header cuộn cùng trang. Enable/Disable hiển thị **Updating…**, khóa hai nút lúc chờ, sau đó phản ánh trạng thái server và thông báo nổi góc dưới tự ẩn sau 5 giây. Nếu thao tác lỗi, hiện trạng thái chưa xác nhận; khi mất kết nối status, badge báo unavailable.
- Nhập duration/probability sai rồi Validate: lỗi server hiện thông báo nổi góc dưới, giữ lại tới khi bấm ×; draft vẫn giữ. Lỗi định dạng input do browser kiểm tra vẫn hiện tại field. Sửa lại, Validate và Apply bình thường.
- `truncate`/`throttle`: Direction request khóa Phase ở `before_upstream_request`; response khóa ở `after_upstream_headers`. `respond`/`hold_request` khóa before, `hold_response` khóa after. `delay`/`close_connection` vẫn chọn được hai phase nếu protocol hỗ trợ action.
- **HTTP method (blank = any)**: để trống khớp mọi method; GET/POST gốc vẫn forward tới upstream. Đặt method cụ thể để giới hạn rule, không dùng trường này để đổi method.
- **Observation limits**: audit chỉ giữ 1.000 bản ghi gần nhất, tự loại bản cũ; outcomes là counters tổng hợp, không phải lịch sử request. Rule counters reset khi config revision đổi; run counters reset khi process restart. Event queue đầy sẽ bỏ event mới và tăng dropped counter; file/log stdout bên ngoài do người vận hành quản lý retention.

U1–U9 đã được người dùng xác nhận PASS ngày 2026-09-16. Checklist bổ sung path pattern và highlight diff cũng đã được xác nhận PASS.

## Kiểm thử highlight diff rule

Sau build/restart và refresh, hai cột Active/Draft highlight theo field: xanh `+` thêm, đỏ `−` xóa, vàng `~` đổi giá trị. Các field không đổi giữ màu bình thường; position biểu thị thứ tự thực tế trong mỗi phiên bản.

1. Sửa probability: chỉ field thay đổi được tô vàng ở hai cột, giá trị cũ/mới nhìn thấy rõ.
2. Thêm/xóa rule: thấy rule xanh/đỏ và `Rule absent` ở cột đối diện.
3. Đổi thứ tự: thấy Position cũ → mới; field không bị tô vàng chỉ vì đổi vị trí. Thêm/xóa rule cũng có thể làm vị trí các rule sau thay đổi.
4. Đổi ID: hiển thị xóa rule cũ, thêm rule mới. ID trùng trong draft có cảnh báo và hiển thị riêng, Validate vẫn từ chối.
5. Hai proxy có cùng rule ID phải được so sánh riêng. Body/header có ký tự HTML hiển thị như text.
6. Validate → review → apply; sau apply diff trở về không có field thay đổi. Conflict vẫn giữ draft và so sánh với active mới.

Kiểm tra tự động logic diff: `rtk proxy node --test internal/control/remote/diff_test.cjs`. Người dùng xác nhận kiểm thử tương tác/hiển thị mục này PASS ngày 2026-09-16.
