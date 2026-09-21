# Faultline — Đặc tả sản phẩm và hành vi hệ thống

Trạng thái: **MVP phase 01–05 đã hiện thực và nghiệm thu AC1–AC24 (2026-09-14).** Xem [bằng chứng và giới hạn](plans/05-mvp-delivery/acceptance.md).

Tài liệu này là đặc tả chính của dự án, ưu tiên nhu cầu hiện tại: một proxy nhận cấu hình, chuyển tiếp traffic và tạo lỗi với tỉ lệ có thể thay đổi khi đang chạy. Các mục ghi **đề xuất** là phương án mặc định để thảo luận; câu hỏi cần quyết định nằm ở cuối file.

Các điểm đã chốt: ưu tiên HTTP và inject lỗi; cấu hình mới chỉ áp dụng cho request mới; xác suất trên từng request eligible; reset counters khi config thay đổi; chạy binary/Docker cho dev; adapter/action nằm trong source Go. UI dùng trên server test chung ở giai đoạn sau, tester chưa phải người dùng chính. Không yêu cầu sửa code hoặc thêm header để dùng chức năng cơ bản.

Review lần 2 chốt thêm: tạm thời hỗ trợ HTTPS thông thường, để mTLS/HTTP/2 sau MVP; giữ một action/request. Không còn câu hỏi về phạm vi cần người dùng trả lời trước khi triển khai. Các chi tiết còn ghi **đề xuất**, như schema/CLI/defaults, là phương án thiết kế để hiện thực và kiểm chứng, không phải yêu cầu đã được người dùng xác nhận từng phần.

Giải thích về truncate request/response nằm ở mục 5.4. Đây là lỗi truyền body không hoàn tất có giá trị kiểm thử; đã được chọn cho phase 06 sau MVP cùng throttle, ở cả chiều request và response.

Phạm vi phase 06 đã chốt ngày 2026-09-14: gRPC unary, HTTP/2 cả hai phía qua TLS hoặc không TLS khi cấu hình tường minh, mTLS tùy chọn độc lập ở mỗi phía, truncate và throttle cho request/response trên HTTP và gRPC. Giữ một action/flow; TLS thay đổi cần restart. Streaming gRPC và hot reload certificate tiếp tục để sau. Xem [kế hoạch phase 06](plans/06-protocol-extensions/README.md) cho contract, thứ tự và tiêu chí nghiệm thu; các tính năng này đã **Done**, với [15 AC và bằng chứng kiểm chứng](plans/06-protocol-extensions/acceptance.md), không thay đổi phạm vi MVP đã nghiệm thu. Schema/capability thực tế xem [hướng dẫn phase 06](examples/grpc/README.md).

Phạm vi phase 09 đã chốt: ưu tiên PostgreSQL trước phase 08 (tiếp tục Deferred). Adapter hỗ trợ delay/hold/ngắt kết nối với phase độc lập, gồm sau xác nhận COMMIT thành công trước khi client nhận phản hồi. Fixture Go + PostgreSQL Docker, kết nối trực tiếp, SCRAM-SHA-256 và thường/TLS; chưa mTLS/PgBouncer/replication/COPY/pipeline, không thêm fixture TypeORM hoặc kiểm thử tắt transaction. Đã hiện thực PostgreSQL 17.11 wire 3.0 / pgx v5.7.6, phase `after_commit`, action `delay`/`hold_response`/`close_connection`, matcher rỗng. P23 Done; P24 **In progress**, automated tests pass, chờ manual UI. Xem [kế hoạch phase 09](plans/09-semantic-adapters/README.md). Mở rộng đã chốt: MySQL 8.4 LTS + fixture Go, transaction tường minh, `after_commit` và ba action tương tự; [P25](plans/09-semantic-adapters/03-mysql-adapter.md) Done: MySQL 8.4.8/go-sql-driver v1.10.1, caching_sha2_password, hai chặng cùng plaintext hoặc cùng TLS; mixed TLS/plaintext bị từ chối. Automated acceptance và Docker runtime đã kiểm chứng; chưa hỗ trợ MariaDB/cluster/replication.

## 1. Bài toán và mục tiêu

Trong hệ thống phân tán, một lời gọi có thể bị chậm, mất kết nối hoặc không nhận được kết quả dù phía nhận đã xử lý thành công. Những tình huống này khó kiểm thử bằng mock chỉ trả về mã lỗi, vì thời điểm lỗi xuất hiện và chiều traffic bị ảnh hưởng quyết định hành vi retry, timeout và xử lý trùng lặp.

Faultline nằm trên đường giao tiếp giữa ứng dụng và dependency để người dùng chủ động tạo, quan sát và lặp lại các tình huống đó.

```text
Chiều request:   Application → Faultline → Dependency
Chiều response:  Application ← Faultline ← Dependency
                                  ↑
                            Cấu hình lỗi
```

**Input của proxy** là traffic đi vào listener. **Output của proxy** là traffic chuyển đến upstream hoặc trả về client sau khi áp dụng rule. Cấu hình là đầu vào điều khiển riêng, không phải payload ứng dụng. Output cũng có thể là không chuyển tiếp, trả lỗi hoặc đóng kết nối.

Mục tiêu:

- Chuyển tiếp traffic bình thường khi không có rule áp dụng.
- Tạo lỗi có chủ đích theo endpoint, chiều truyền, thời điểm và tỉ lệ cấu hình.
- Thay đổi rule và tỉ lệ lỗi mà không khởi động lại proxy.
- Hỗ trợ kiểm thử xác định được lần nào gặp lỗi, bên cạnh chế độ xác suất.
- Mở rộng protocol, matcher và loại lỗi mà không viết lại engine.
- Ban đầu thao tác bằng file và CLI; sau này tester quản lý qua UI dùng cùng mô hình cấu hình.
- Về sau liên kết các lần retry và kiểm tra hành vi phục hồi, giữ định hướng framework trong tài liệu gốc.

### 1.1. Định nghĩa network partition trong phạm vi dự án

Trong Faultline, partition là tình huống giao tiếp qua một đường được chọn bị gián đoạn hoàn toàn hoặc theo một chiều trong một khoảng thời gian. Ví dụ A gọi B không được nhưng A vẫn gọi C được, hoặc B đã nhận request nhưng A không nhận được response.

Proxy chỉ tác động được traffic thực sự đi qua nó. Một proxy trước B không tự tạo ra partition cho toàn bộ cluster hoặc chặn được đường A gọi thẳng B. Kiểm thử partition giữa nhiều node cần định tuyến các đường cần kiểm thử qua proxy tương ứng.

Đề xuất bản đầu mô phỏng **hiệu ứng lỗi mà ứng dụng quan sát được**. Can thiệp packet IP, packet loss/reordering thực sự và network partition ở cấp hệ điều hành là phạm vi khác, chưa đưa vào MVP. Các hiệu ứng trên HTTP/TCP không được quảng bá như mô phỏng chính xác mọi loại lỗi mạng.

## 2. Người dùng và luồng sử dụng

| Người dùng | Công việc cần thực hiện |
| --- | --- |
| Developer | Viết config trong repository, tái hiện lỗi và debug local |
| Tester / QA | Chọn dependency, bật lỗi, đổi tỉ lệ và xem kết quả mà không cần sửa code ứng dụng |
| CI | Khởi chạy proxy bằng config cố định, chạy integration test và lưu bằng chứng |

Luồng ban đầu:

1. Khai báo listener, upstream và rules trong YAML.
2. Validate config, khởi chạy Faultline, đổi endpoint của ứng dụng sang listener.
3. Khởi động ứng dụng khi proxy đang chuyển tiếp bình thường; người dùng hoặc test script xác nhận app sẵn sàng.
4. Bật injection bằng CLI, sau đó thao tác trên app hoặc chạy test có sẵn.
5. Faultline match rule, quyết định inject, chuyển tiếp hoặc áp dụng fault và ghi sự kiện.
6. Người dùng sửa tỉ lệ/rule, reload config và nhận kết quả thành công hoặc lỗi validation; tắt injection khi kết thúc test.
7. Xem số request bị ảnh hưởng, timeline, trạng thái injection và version config đã áp dụng.

Faultline không tự sinh tải và không tự retry request trong MVP. Request retry do ứng dụng gửi đến được xem là một attempt mới.

## 3. Phạm vi phiên bản đầu

**Đã chốt HTTP và tập trung inject lỗi trước.** Đề xuất triển khai HTTP/1.1 reverse proxy, một process có nhiều listener, mỗi listener trỏ tới một upstream cố định. Mục tiêu là tạo điều kiện để developer kiểm tra app phục hồi, báo lỗi hay xuất hiện bug; MVP không tự kết luận app đúng/sai.

Theo Q6, MVP dùng HTTP/1.1 qua HTTP hoặc HTTPS thông thường. mTLS và HTTP/2 để sau MVP; không còn là quyết định đang chờ trả lời.

| Có trong MVP | Để sau MVP |
| --- | --- |
| HTTP/1.1 qua HTTP/HTTPS, upstream cố định theo listener | mTLS, HTTP/2, gRPC, TCP, database/broker |
| YAML, CLI validate/serve/reload/enable/disable/status | UI và API quản trị từ xa |
| Match method, exact path, header | Match nội dung body, SQL, topic, message |
| Xác suất, request thứ N, mỗi N request | Timeline nhiều bước, phối hợp nhiều proxy |
| Delay request/response, trả status giả, giữ request/response hoặc đóng connection | Throttle, truncate body, half-close, duplicate message |
| Reload rule atomically, ghi revision | Control plane phân tán |
| Log sự kiện JSON và counters | Retry correlation, assertions, báo cáo PASS/FAIL |
| Demo mất response và retry | Instrumentation SDK, business assertions |

HTTP upgrade, WebSocket, CONNECT, streaming vô hạn và forward proxy chưa nằm trong phạm vi bản đầu. Không cần sửa code nghiệp vụ, nhưng vẫn cần đổi endpoint và cấu hình certificate trust nếu dùng HTTPS.

### 3.1. HTTPS, mTLS và HTTP/2 là các nhu cầu riêng

Đã chốt tại Q6: tạm thời có HTTPS với TLS termination; mTLS và HTTP/2 để sau MVP. Phân biệt bên dưới được giữ để làm rõ hướng mở rộng.

```text
App ── HTTPS ──> Faultline ── HTTPS hoặc HTTP ──> Dependency
                giải mã để match/inject
```

- Hai phía cấu hình độc lập; hỗ trợ HTTP → HTTP, HTTP → HTTPS, HTTPS → HTTP và HTTPS → HTTPS.
- Phía nhận HTTPS cần certificate/key cho hostname của Faultline; client phải tin certificate hoặc CA tương ứng.
- Phía gọi upstream HTTPS xác minh certificate và hostname, dùng CA hệ thống hoặc CA test được cấu hình. Không mặc định bỏ xác minh TLS.
- mTLS bổ sung xác thực client bằng certificate, có thể cần ở một hoặc cả hai phía. Đây là quyết định riêng với HTTPS thông thường.
- HTTP/2 là phiên bản HTTP có nhiều stream cùng connection. Nếu đưa vào MVP, cần định nghĩa fault scope theo stream và kiểm tra các stream khác không bị ảnh hưởng ngoài cấu hình.
- Với phương án HTTP/1.1, phải cấu hình rõ phiên bản ở cả hai phía, tránh tự thương lượng HTTP/2 trong khi engine chỉ cam kết HTTP/1.1.

Certificate, trust và protocol negotiation tương ứng với các cấu hình trong [Go TLS Config](https://pkg.go.dev/crypto/tls#Config); Go HTTP có cơ chế hỗ trợ HTTP/2 cần được cấu hình theo phạm vi adapter, xem [net/http](https://pkg.go.dev/net/http). Đây là cơ sở kỹ thuật cho đề xuất, chưa phải lựa chọn phiên bản Go.

## 4. Các khái niệm thống nhất

| Khái niệm | Ý nghĩa |
| --- | --- |
| Listener / input | Địa chỉ Faultline nhận traffic từ client |
| Upstream / output | Dependency đích mà listener chuyển tiếp đến |
| Connection | Kết nối transport; có thể chứa nhiều request hoặc stream |
| Flow | Đơn vị adapter xử lý: HTTP request/response, TCP connection hoặc RPC stream |
| Attempt | Một lần gọi quan sát được; retry là attempt khác |
| Logical operation | Nghiệp vụ có thể gồm nhiều attempt; chỉ xác định khi có dữ liệu correlation |
| Phase | Điểm can thiệp trong lifecycle do adapter công bố |
| Matcher | Điều kiện chọn traffic: method, path, header… |
| Selector | Cách chọn lần bị lỗi: probability, nth hoặc every |
| Fault action | Hành vi thực thi tại phase đã chọn |
| Rule | Matcher + selector + phase + một fault action |
| Config revision | Snapshot cấu hình bất biến được áp dụng thành công |
| Injection state | Công tắc enabled/disabled khi chạy, tách khỏi config chứa rules |
| Run | Một lần chạy process; phạm vi counters và dữ liệu quan sát ban đầu |

Không đồng nhất HTTP status lỗi với lỗi transport. Ví dụ response 503, đóng connection và giữ response để client hết timeout là ba hành vi khác nhau.

## 5. Luồng xử lý và điểm inject

```text
Nhận request → gắn flow_id, config revision và injection state
             → disabled: chuyển tiếp bình thường, không chọn fault
             → enabled: match và chọn tối đa một rule
             → [before_upstream_request]
             → gửi request đến upstream
             → nhận response headers cuối cùng từ upstream
             → [after_upstream_headers]
             → chuyển response headers/body về client
             → ghi kết quả, giải phóng tài nguyên
```

Đề xuất MVP chỉ match metadata request, quyết định rule một lần khi nhận request headers. Quyết định được giữ lại tới phase cần inject; nếu flow lỗi hoặc bị hủy trước phase đó, ghi `not_reached`, không tính là fault đã thực thi.

### 5.1. Fault actions cho HTTP MVP

| Action | Phase | Hành vi chính xác |
| --- | --- | --- |
| `delay` | `before_upstream_request` | Đợi duration rồi mới bắt đầu gửi request đến upstream |
| `delay` | `after_upstream_headers` | Đợi duration trước khi gửi response headers cuối cùng và body về client |
| `respond` | `before_upstream_request` | Trả status/body do config định nghĩa, không gọi upstream |
| `hold_request` | `before_upstream_request` | Không gửi request đến upstream và không trả response cuối cùng; giữ tới khi client hủy hoặc hết `max_duration`, rồi đóng connection |
| `hold_response` | `after_upstream_headers` | Không gửi response cuối cùng về client; giữ tới khi client hủy hoặc hết `max_duration`, rồi đóng connection |
| `close_connection` | `before_upstream_request` hoặc `after_upstream_headers` | Đóng connection phía client tại phase đã chọn; không gọi upstream ở phase trước request, kết thúc luồng upstream ở phase sau headers |

`after_upstream_headers` nghĩa là đã nhận response headers cuối cùng, không có nghĩa đã đọc xong body hoặc biết transaction nghiệp vụ đã commit. Với MVP, response 1xx không phải điểm inject này và không được chuyển tiếp như kết quả cuối cùng.

`hold_response` phải đóng/cancel luồng upstream sau khi đã quan sát headers, không buffer body không giới hạn trong thời gian giữ client. Do đó fault này có thể ảnh hưởng việc upstream tiếp tục tạo body; demo nên dùng response nhỏ, nghiệp vụ hoàn tất trước khi trả headers.

`close_connection` không cam kết phát TCP RST hoặc một thông báo lỗi giống nhau trên mọi client. Nếu cần reset ở cấp transport, phải thêm capability và integration test riêng. Timeout là kết quả phụ thuộc timeout của client: nếu client timeout dài hơn `max_duration`, client sẽ thấy đóng connection trước.

Khi không inject, proxy chuyển tiếp request/response theo semantics của HTTP proxy; không cam kết byte-for-byte giống traffic gốc. Body bình thường được stream với bộ nhớ có giới hạn; delay tạo backpressure thay vì đọc toàn bộ body vào RAM.

### 5.2. Hướng mở rộng loại lỗi

| Nhóm | Ví dụ | Yêu cầu mở rộng |
| --- | --- | --- |
| Chậm | Jitter, slow body, bandwidth limit | Định nghĩa theo chiều truyền và đơn vị bytes/time |
| Mất liên lạc | Reject, reset, idle disconnect, half-close | Capability của transport và phạm vi connection |
| Body truyền không hoàn tất | Truncate request/response: ngắt khi mới chuyển một phần body | Điểm cắt theo byte và cách kết thúc transport; xem mục 5.4 |
| Lỗi protocol | HTTP status, gRPC status | Adapter hiểu protocol |
| Ambiguous outcome | Mất response, publisher confirm hoặc commit response | Điểm quan sát semantic đủ chính xác |
| Partition có thời hạn | Chặn mọi flow đến một dependency trong 30 giây | Scope, thời gian bắt đầu/kết thúc, xử lý flow đang chạy |
| Duplicate delivery | Phát lại message hoặc tạo điều kiện redelivery | Adapter và semantics message; không lặp tùy ý byte stream |

MVP có thể dùng `match: {}`, probability bằng 1 và `hold_request` hoặc `close_connection` tại `before_upstream_request` để mô phỏng đường gọi không hoạt động với request mới. `delay` với probability bằng 1 chỉ làm mọi request chậm, không tự tạo partition. Việc cắt ngay các flow đang chạy khi bật partition là tính năng riêng, chưa được suy ra từ reload config.

### 5.3. Nên tác động lên request hay connection?

**Đề xuất: có tác động lên cả hai khi fault cần, nhưng chọn mục tiêu theo request trong MVP.** Ví dụ delay giữ một request; lost response giữ kết quả của request đó; disconnect phải đóng connection đang phục vụ request được chọn.

Phân biệt hai thời điểm:

1. **Bật/reload lỗi:** chỉ request nhận sau mốc này mang quyết định mới; không quét và đóng các request đang chạy từ trước.
2. **Thực thi lỗi:** request đã được chọn có thể bị cắt ngay khi nó đang xử lý, tại phase cấu hình như sau khi upstream trả headers. Như vậy vẫn kiểm thử được lỗi xảy ra giữa một lần gọi.

HTTP/1.1 đóng connection sẽ khiến client không thể dùng lại connection đó; không được mô tả là chỉ thay đổi status của một request. MVP chưa cam kết cô lập ảnh hưởng khi client dùng HTTP pipelining. Hỗ trợ cắt giữa body, ngắt connection idle hoặc cắt toàn bộ connection có sẵn sẽ mở rộng sau theo use case cụ thể.

Ví dụ: R1 bắt đầu khi injection tắt; bật injection; R2 bắt đầu và được chọn lost response. R1 tiếp tục bình thường, R2 bị giữ/cắt ở phase đã cấu hình. Không cần bật fault từ lúc app khởi động để tạo lỗi giữa request.

### 5.4. Truncate request/response có thực sự tồn tại không?

**Có.** Trong Faultline, truncate nghĩa là **body đang truyền thì bị ngắt trước khi hoàn tất**. Ví dụ tình huống cần mô phỏng: upload/download đang chạy thì client, server hoặc proxy đóng kết nối, hoặc kết nối hỏng khiến lần truyền không thể tiếp tục.

TCP cung cấp byte stream có thứ tự và truyền lại dữ liệu khi phát hiện mất packet; nó không bảo đảm toàn bộ HTTP body sẽ được chuyển xong khi kết nối không thể tiếp tục. Vì vậy không nên mô tả truncate là “TCP âm thầm bỏ vài byte giữa body rồi vẫn báo thành công”. Xem [RFC 9293, mục 2.2](https://www.rfc-editor.org/rfc/rfc9293.html#section-2.2).

| Trường hợp | Ví dụ kiểm thử | Hành vi ứng dụng cần kiểm tra |
| --- | --- | --- |
| Truncate request | App upload body 10 MiB; proxy chỉ chuyển 2 MiB đến upstream rồi ngắt | Upstream xử lý upload dở dang thế nào; client báo lỗi/retry ra sao; có tài nguyên tạm hoặc side effect cần dọn không |
| Truncate response | Upstream trả headers `200 OK`, body 10 MiB; proxy chỉ chuyển 2 MiB về app rồi ngắt | App phát hiện đọc body thất bại hay lưu file thiếu và báo thành công; retry/resume có hoạt động đúng không |

Với HTTP/1.1 có body dùng `Content-Length`, nhận ít hơn độ dài khai báo là message không hoàn chỉnh. Với chunked, thiếu chunk kết thúc cũng là không hoàn chỉnh. Response phân định bằng đóng connection có thể không phân biệt được kết thúc bình thường với mất dữ liệu chỉ dựa vào HTTP framing. Các quy tắc này nằm trong [RFC 9112, mục 6.3](https://www.rfc-editor.org/rfc/rfc9112.html#section-6.3) và [mục 8](https://www.rfc-editor.org/rfc/rfc9112.html#section-8).

HTTPS cũng có thể bị ngắt giữa chừng; TLS không làm dữ liệu chưa truyền tự xuất hiện. TLS có quy tắc đóng kết nối để phát hiện/ngăn tình huống truncation, xem [RFC 8446, mục 6.1](https://www.rfc-editor.org/rfc/rfc8446.html#section-6.1). Với Faultline terminate TLS, fault được định nghĩa ở byte body HTTP trước khi mã hóa, không cần sửa tùy ý ciphertext.

**Giá trị riêng so với lost response:** lost response trong MVP chặn trước khi client nhận response headers cuối cùng; truncate response cho client nhận headers và một phần body trước khi lỗi. Tình huống sau giúp phát hiện app chỉ kiểm tra status thành công mà bỏ qua kết quả đọc body. Kết quả cụ thể cần kiểm tra với client/server thực tế, không mặc định upstream sẽ rollback mọi side effect.

**Định hướng khi triển khai sau MVP:** chuyển N byte body rồi ngắt, giữ bằng chứng body chưa hoàn tất. Không sửa `Content-Length` thành N hoặc thêm chunk kết thúc bình thường để biến body bị cắt thành message HTTP hợp lệ ngắn hơn. Cần tách lỗi do Faultline inject với body nguồn vốn đã thiếu, định nghĩa trường hợp body không đủ dài để cắt, và kiểm tra cả HTTP lẫn HTTPS. Phase 06 đã hiện thực hành vi này với `action: truncate`, `direction`, `bytes` và phase tương ứng; không thay đổi phạm vi MVP.

## 6. Tỉ lệ lỗi và tính lặp lại

### 6.1. Quy tắc chọn request

Đề xuất mỗi rule chỉ có **một selector**:

- `probability: 0.2`: mỗi attempt đủ điều kiện được chọn với xác suất 20%; không đảm bảo đúng 20 trên mỗi 100 request.
- `nth: 3`: chọn đúng request đủ điều kiện thứ ba.
- `every: 10`: chọn các request đủ điều kiện thứ 10, 20, 30…

`probability` nằm trong `[0, 1]`; `nth` và `every` là số nguyên dương. Không có selector mặc định: cấu hình phải khai báo để dễ review. Header kích hoạt test nằm trong matcher, có thể kết hợp với bất kỳ selector nào.

**Đã chốt:** probability là xác suất trên request eligible; `0` không chọn request nào, `1` chắc chắn chọn tất cả request eligible khi injection enabled, không dùng phép random có thể bỏ sót ở hai biên này. Muốn test ngay lỗi ngắt kết nối, dùng `probability: 1` với `close_connection` tại `before_upstream_request` và matcher đúng request cần test.

“100%” bảo đảm chọn action, còn lỗi mà app thấy phụ thuộc action và phase: delay ngắn có thể không làm app lỗi; fault sau response headers chỉ thực thi nếu upstream thực sự trả headers. UI/log phải phân biệt selected, applied và kết quả client để không diễn giải sai yêu cầu này.

App vẫn có thể xử lý lỗi bằng retry, fallback hoặc thông báo phù hợp. Faultline bảo đảm hành vi inject trên attempt đủ điều kiện, không bắt buộc toàn bộ nghiệp vụ phải thất bại; quan sát app có xử lý đúng hay không chính là mục tiêu kiểm thử.

Đơn vị MVP là **HTTP attempt**, không phải packet, byte, connection hoặc logical operation. Các adapter sau này phải công bố đơn vị hỗ trợ; không diễn giải ngầm cùng một tỷ lệ cho mọi protocol.

### 6.2. Khi nhiều rule cùng match

Đề xuất xét rule theo thứ tự trong file:

1. Bỏ qua rule disabled hoặc matcher không khớp.
2. Rule enabled đầu tiên có matcher khớp sở hữu request đó và tăng counter eligible của rule.
3. Selector của rule quyết định inject hay pass-through. Nếu không chọn thì chuyển tiếp bình thường, không thử rule phía sau.
4. Mỗi request chỉ có tối đa một fault action trong MVP.

Quy tắc này giữ ý nghĩa tỉ lệ dễ hiểu và tránh cộng dồn lỗi bất ngờ. Hai rule cùng matcher không tự tạo ra hỗn hợp 20% delay + 10% status; muốn hỗ trợ hỗn hợp cần thiết kế riêng. Rule rộng nên đặt cuối vì có thể che các rule phía sau.

### 6.3. Counter, seed và concurrency

- Counter thuộc `(run_id, config_revision, proxy_id, rule_id)`, bắt đầu từ 1 cho request eligible đầu tiên.
- Quyết định sequence và selector phải được cấp phát đồng bộ khi có request đồng thời.
- Chế độ probability dùng chuỗi giả ngẫu nhiên xác định theo seed, proxy/rule và eligible sequence; reload revision mới reset sequence. Không dùng revision ID ngẫu nhiên làm đầu vào random.
- Cùng config, seed và thứ tự request eligible phải cho cùng quyết định inject. Thứ tự request có thể đổi khi concurrency thay đổi, nên seed không bảo đảm cùng nghiệp vụ luôn gặp lỗi.
- Test cần tái hiện chính xác nên dùng traffic tuần tự và `nth`/`every`, hoặc matcher header của test case.
- Không bắt buộc thêm header, operation ID hoặc SDK vào app. Matcher dùng dữ liệu có sẵn; header điều khiển chỉ là lựa chọn khi test driver đã có thể gửi nó.
- Probability quyết định inject, không tái hiện toàn bộ scheduling, độ trễ thật hoặc hành vi dependency.
- Nếu chạy nhiều process, counters là cục bộ; chưa có tỉ lệ hoặc thứ tự toàn cục.

Log phải có số eligible, selected và applied riêng. Tỉ lệ applied có thể thấp hơn selected nếu request không đến được fault phase.

### 6.4. Ưu/nhược khi phối hợp nhiều lỗi — trả lời Q7

“Phối hợp” có hai nghĩa khác nhau: chia các request thành nhóm lỗi khác nhau, hoặc thực hiện nhiều action trên cùng một request.

| Phương án | Ưu điểm | Nhược điểm | Ví dụ |
| --- | --- | --- | --- |
| Một action/request | Dễ cấu hình, quy nguyên nhân, tái hiện và kiểm tra tỷ lệ | Chưa mô phỏng được chuỗi nhiều lỗi trên cùng lần gọi | 100% request được chọn bị mất response |
| Chia traffic thành các nhóm loại trừ nhau | Test nhiều kiểu phản ứng trong cùng lần chạy; tỷ lệ mỗi nhóm rõ | Cần selector có trọng số, kiểm tra tổng tỷ lệ và thống kê từng nhóm | 20% delay, 10% mất response, 70% bình thường |
| Nhiều action nối tiếp trên cùng request | Test tình huống phức tạp như chậm rồi mất kết quả, hữu ích khi app có nhiều lớp timeout/retry | Khó xác định nguyên nhân; phải định nghĩa thứ tự, phase, cancellation và action không còn đến được | Đợi 2 giây trước upstream, rồi giữ response sau headers |

**Đã chốt tại Q7: MVP giữ một action/request.** Vẫn có thể chạy nhiều rule cho các endpoint/matcher khác nhau, hoặc đổi fault giữa các lần test. Nếu cần nhiều loại lỗi trên cùng tập traffic, nên thêm nhóm có trọng số trước khi thêm chuỗi action; chưa đưa hai tính năng này vào schema.

Không nên cho các tỷ lệ độc lập chồng lên nhau mà thiếu semantics: 20% delay và 10% disconnect lấy mẫu độc lập có thể tạo nhóm gặp cả hai. Đó không phải phân chia 20%/10%/70%. MVP không áp dụng cách phối hợp này.

## 7. Cấu hình khai báo — schema đề xuất

Schema và validation đã được hiện thực trong phase 1; CLI validate/serve và HTTP/HTTPS adapter ở phase 2, các fault action ở phase 3. Phase 4 bổ sung CLI runtime và recorder; kết quả kiểm chứng nằm trong plan tương ứng. Defaults và quy tắc parse cụ thể nằm trong [hướng dẫn config](examples/http/README.md). Ví dụ dưới đây chỉ khai báo tính năng MVP để tránh nhầm với roadmap.

```yaml
api_version: faultline/v1alpha1
seed: 42

runtime:
  max_inflight_requests: 1000
  request_timeout: 30s

proxies:
  - id: payment
    protocol: http1
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:9000
    rules:
      - id: payment-lost-response
        enabled: true
        match:
          method: POST
          path: /payments
        select:
          probability: 0.2
        fault:
          phase: after_upstream_headers
          action: hold_response
          max_duration: 10s

      - id: payment-delay
        enabled: true
        match:
          method: GET
          path: /payments
        select:
          every: 10
        fault:
          phase: before_upstream_request
          action: delay
          duration: 500ms
```

Semantics của ví dụ:

- Khi injection enabled, 20% các `POST /payments` được chọn giữ response; không áp dụng cho mọi request của proxy và không yêu cầu app thêm header.
- Mỗi `GET /payments` eligible thứ 10 bị chậm thêm 500 ms trước khi gọi upstream.
- Exact path không gồm query string; header name không phân biệt hoa thường, header value match chính xác. Các điều kiện matcher kết hợp AND; `match: {}` nghĩa là tất cả request.
- `match.path_pattern` hỗ trợ tham số nguyên đoạn `:name`, ví dụ `/payment/:id`; không dùng cùng `match.path` khác rỗng. Tên tham số `[A-Za-z_][A-Za-z0-9_]*`, không trùng; không hỗ trợ wildcard, regex, query/fragment hay tham số nhúng như `item-:id`. Match toàn path, phân biệt hoa/thường và trailing slash; tham số phải không rỗng.
- Exact và pattern dùng path đã decode (`r.URL.Path`) của adapter: `%2F` thành dấu `/`, `%252F` chỉ decode một lần thành `%2F`. Không rewrite URL/query gửi upstream. Rule xét theo thứ tự khai báo, không tự ưu tiên exact: đặt `/payment/history` trước `/payment/:id` nếu muốn rule riêng cho history.

- Request không match rule nào được chuyển tiếp bình thường.
- Header dùng match vẫn được chuyển tiếp; chỉ thêm thao tác strip nếu có yêu cầu riêng.
- Upstream MVP nhận origin `http://host:port` hoặc `https://host:port`, giữ path/query của request; không hỗ trợ rewrite hoặc load balancing.
- Defaults hiện thực: 1.000 inflight và 30 giây timeout. Hold duration là tham số từng rule; các giá trị không phải chỉ tiêu hiệu năng.

Ví dụ khai báo **một phần tử thay thế trong `proxies`** để dùng HTTPS hai phía (không phải file config hoàn chỉnh):

```yaml
id: payment-tls
protocol: http1
listen: 0.0.0.0:8443
tls:
  cert_file: ./certs/faultline.crt
  key_file: ./certs/faultline.key
upstream: https://payment.test:9443
upstream_tls:
  ca_file: ./certs/upstream-ca.crt
rules: []
```

`tls` bật HTTPS phía listener; thiếu block này thì listener nhận HTTP. `upstream_tls.ca_file` là CA test bổ sung vào trust hệ thống để xác minh upstream; bỏ block thì dùng trust hệ thống. Hostname xác minh và SNI lấy từ host của upstream URL. Client gọi hostname nằm trong certificate Faultline và cấu hình trust CA tương ứng. `rules: []` chỉ chuyển tiếp; muốn inject thì thêm rule như ví dụ trước.

Đường dẫn file được resolve theo thư mục config và cần tồn tại trong filesystem của process Faultline. Khi chạy Docker phải mount config/cert vào container; CLI reload không tự upload certificate hoặc key. Đổi TLS settings/certificate cần restart trong MVP.

CLI hiện tại:

```sh
faultline validate --config ./faultline.yaml
faultline serve --config ./faultline.yaml

# Trong terminal khác, sau khi app đã sẵn sàng:
faultline status
faultline enable

# Đổi rule/tỉ lệ trong file rồi áp dụng:
faultline reload --config ./faultline.yaml

# Kết thúc giai đoạn gây lỗi:
faultline disable
```

`reload` gửi đường dẫn tuyệt đối của file config gốc qua Unix socket riêng. Process đang chạy tự đọc file gốc và toàn bộ include trong filesystem của nó, validate lại và trả revision có hiệu lực. `--admin-socket PATH` chọn instance; mặc định `/tmp/faultline-<uid>/admin.sock`, thư mục riêng 0700 và socket 0600. Runtime CLI có `--timeout` mặc định 5s, không tự retry mutation; timeout cần kiểm tra lại status vì thao tác có thể đã áp dụng. Xem [hướng dẫn runtime](README.md#runtime-administration).

Validation phải từ chối unknown field, ID trùng trong cùng scope, selector không hợp lệ, duration không dương, status không thuộc 200–599 với `respond`, upstream/listener sai định dạng và action/phase không được adapter hỗ trợ. Lỗi phải chỉ rõ đường dẫn field; không âm thầm bỏ qua config không hiểu.

TLS validation kiểm tra cert/key đọc được và khớp, CA parse được, không cho `upstream_tls` đi cùng upstream HTTP. Lỗi xác minh chứng chỉ upstream khi kết nối phải được ghi riêng với fault do engine inject.

### 7.1. Cấu hình nhiều file — đã hiện thực ở core phase 1

`config.Load(rootFilename)` đọc toàn bộ tập file; `control.ReloadFile(rootFilename)` nạp lại và apply. `Parse(data, filename)`/`Reload(data, filename)` chỉ dành cho YAML standalone và từ chối include. CLI/Docker còn chờ các phase sau; MC1–MC4 đã kiểm chứng ở tầng core.

Một file gốc là điểm vào cho validate/serve/reload; có thể giữ toàn bộ `proxies` trong file đó hoặc dùng `include` để chia theo dependency. Cấu hình một file hiện tại tiếp tục hợp lệ.

```text
config/
├── faultline.yaml
└── proxies/
    ├── payment.yaml
    └── inventory.yaml
```

File gốc `config/faultline.yaml`:

```yaml
api_version: faultline/v1alpha1
seed: 42
runtime:
  max_inflight_requests: 1000
  request_timeout: 30s
include:
  - proxies/*.yaml
```

File con `config/proxies/payment.yaml`:

```yaml
proxies:
  - id: payment
    protocol: http1
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:9000
    rules: []
```

Hợp đồng nạp cấu hình:

- `include` là danh sách đường dẫn file hoặc glob, resolve theo thư mục file gốc. Chỉ hỗ trợ include ở file gốc; file con chỉ có `proxies`, không có `seed`, `runtime`, `api_version` hoặc include lồng nhau.
- Mỗi file chứa đúng một YAML document. Ghép proxies inline trước, tiếp theo theo thứ tự mục include; kết quả từng glob được sắp xếp từ điển theo đường dẫn. Giữ nguyên thứ tự rules trong từng proxy.
- **Proxy ID trùng ở bất kỳ file nào đều báo lỗi**, không merge hoặc ghi đè. Rule ID trùng trong cùng proxy cũng báo lỗi; hai proxy khác nhau được dùng cùng rule ID vì counters có scope proxy/rule.
- File không tồn tại/không đọc được, glob sai/không match, hoặc include trỏ tới thư mục đều là lỗi. Cùng file vật lý được include nhiều lần, kể cả qua symlink, cũng báo lỗi; không âm thầm đọc hai lần. File con phải có danh sách proxies không rỗng; sau ghép cần ít nhất một proxy.
- Cert/key/CA resolve theo thư mục **file khai báo proxy đó**. Lỗi chỉ rõ source file và field path; lỗi ID trùng chỉ rõ cả hai nơi khai báo, không in giá trị nhạy cảm.
- Tất cả proxies được validate chung trước tạo một `Document`/snapshot. Source paths và cách chia file chỉ phục vụ chẩn đoán; không tham gia hash cấu hình hiệu lực. Chuyển proxy sang file khác vẫn no-op nếu cấu hình chuẩn hóa, TLS paths và nội dung TLS không đổi.
- Reload đọc lại toàn bộ tập file từ file gốc. Một lỗi làm cả lần apply thất bại; request cũ giữ snapshot cũ, request mới chỉ nhận snapshot mới sau apply thành công. Thêm/xóa proxy hoặc đổi listener/upstream/TLS/runtime vẫn cần restart theo mục 8; tách nhiều file không thay đổi giới hạn này.
- Tính atomic áp dụng cho việc công bố snapshot trong process, không phải giao dịch ghi nhiều file trên đĩa. Người dùng hoàn tất chỉnh sửa các file rồi mới reload; chưa có auto-watch.
- CLI validate/serve đọc filesystem nơi lệnh chạy; reload đọc filesystem của process phục vụ. Trên Docker, mount cả cây config và certificate cần dùng, chạy CLI quản trị trong container với đường dẫn tương ứng; không upload config/cert qua admin channel.

Tiêu chí bổ sung: **MC1** một file và nhiều file tương đương có cùng effective config; **MC2** lỗi include/ID/field có nguồn rõ và giữ revision cũ; **MC3** TLS paths resolve theo file con; **MC4** đổi rules trong file con reset counters, chia lại file tương đương là no-op; **MC5** validate/serve/reload chạy được với cây config mount trong Docker. AC1–AC24 vẫn giữ nguyên và phải regression.

## 8. Cập nhật cấu hình khi đang chạy

**Yêu cầu cốt lõi:** đổi probability, enable/disable, matcher và action của rule mà không restart process hoặc ngắt toàn bộ kết nối.

Đề xuất flow apply:

```text
Đọc toàn bộ config → parse → validate schema/capability
                  → chuẩn bị snapshot mới
                  → đổi active revision một lần, atomically
                  → trả revision và thời điểm có hiệu lực
```

- Request nhận trước thời điểm apply giữ snapshot cũ đến khi kết thúc, kể cả đang đợi delay hoặc response. Request mới dùng snapshot mới, kể cả trên HTTP keep-alive connection cũ.
- Config lỗi: giữ nguyên snapshot cũ; báo apply thất bại và nguyên nhân.
- Startup với config lỗi: process thoát lỗi, không mở listener phục vụ traffic.
- Apply lại cùng cấu hình hiệu lực là no-op, giữ revision/counters. Khi config hiệu lực thay đổi, toàn bộ rule counters của revision mới reset; các request cũ vẫn ghi vào revision cũ.
- Đổi listener, protocol, upstream, TLS settings hoặc runtime limits cần restart trong MVP; reload có thay đổi những field này bị từ chối toàn bộ, không áp dụng một phần.
- Tắt rule không hủy các fault đã gắn với request cũ. Khả năng dừng ngay mọi fault đang chạy là yêu cầu khác, cần chốt nếu cần cho tester.
- Restart tạo run mới và reset counters; không khôi phục inflight request hoặc trạng thái injection enabled của lần chạy trước.
- MVP đề xuất reload chủ động qua CLI. Auto-watch file và lịch thay đổi tỷ lệ chưa cần cho giải pháp bật lỗi sau startup.

### 8.1. Khởi động bình thường, bật lỗi khi app đang hoạt động — giải pháp cho Q3

**Đề xuất tách cấu hình rule khỏi công tắc injection.** File có thể khai báo rule `enabled: true` và probability bằng 1 từ đầu, nhưng `serve` mặc định khởi động với injection disabled. Proxy vẫn nhận/chuyển tiếp traffic để app kết nối dependency và hoàn thành startup.

```text
Nạp config → Proxy sẵn sàng, injection DISABLED
           → App khởi động và hoạt động bình thường
           → Người dùng/test script xác nhận app ready
           → enable → Request mới bắt đầu chịu lỗi
           → disable → Request mới trở lại bình thường
```

| Thao tác | Kết quả |
| --- | --- |
| `serve --config ...` | Validate/nạp config, mở listener, injection disabled |
| `enable` | Bật injection cho request mới trên toàn process; rule vẫn cần enabled và match |
| `disable` | Tắt injection cho request mới; flow cũ tiếp tục theo quyết định đã nhận |
| `reload --config ...` | Thay config atomically, giữ nguyên trạng thái công tắc injection |
| `status` | Trả run ID, config revision, injection state, thời điểm đổi state, listener readiness và số flow còn đang chịu fault |
| `serve --config ... --start-enabled` | Chủ động bật lỗi từ startup, dành cho test hành vi khởi động khi dependency lỗi |

Quy tắc đề xuất:

- Công tắc là trạng thái runtime toàn process trong MVP. Bật/tắt riêng dependency qua `rule.enabled` và reload; chưa thêm selector proxy cho lệnh enable/disable.
- Mỗi request chụp config revision và injection state đồng thời khi nhận headers. Apply/enable/disable có thứ tự hiệu lực rõ, không tạo snapshot nửa cũ nửa mới.
- Khi disabled, request vẫn ghi tổng traffic nhưng không tăng eligible counters, không chạy selector. Traffic startup không làm mất lượt `nth: 1`.
- Enable/disable không đổi config revision, không reset counter: disable tạm dừng đếm, enable tiếp tục. Config hiệu lực thay đổi mới reset theo Q8; cần run hoàn toàn mới thì restart process.
- Lệnh enable/disable lặp lại khi đã ở đúng trạng thái là no-op. Mỗi thay đổi thực sự ghi control event có thời điểm và sequence; flow lưu sequence đã nhận để truy vết.
- `status` báo proxy ready không có nghĩa app/dependency ready. Faultline không biết app sẵn sàng từ HTTP request đầu tiên; người dùng hoặc script kiểm tra bằng cơ chế hiện có rồi gọi enable.
- Không dùng thời gian chờ cố định làm mặc định: app có thể khởi động nhanh/chậm khác nhau giữa các test. CI có thể chờ health check hiện có; nếu không có thì developer bật thủ công sau khi kiểm tra app.
- Disable không phục hồi request đã timeout hoặc side effect đã xảy ra, cũng không dừng ngay hold của flow cũ. Status phải thể hiện nếu còn flow đang chịu fault dù công tắc đã tắt.

Như vậy **100% fault trong file không cản app startup**, trừ khi người dùng chủ động chọn `--start-enabled`. Giải pháp không cần sửa code ứng dụng và không cần xây scheduler/UI ngay.

UI tương lai phải sử dụng cùng schema, validation và apply service. Người dùng cần thấy bản nháp khác gì bản đang chạy, revision nào đã được áp dụng và lỗi apply nếu có. Không để UI có một engine rule riêng.

Khi có nhiều người chỉnh, API cần revision precondition để tránh ghi đè cập nhật của người khác.

**Phạm vi phase 07 đã chốt ngày 2026-09-15 — API/UI đã hiện thực, nghiệm thu tương tác UI còn pending:** một instance trên server test chung có nhiều proxy, đăng nhập độc lập với danh tính riêng và hai quyền toàn instance (xem; chỉnh/apply/bật-tắt), API/UI truy cập qua HTTPS. Giữ chế độ file/CLI cho dev/CI; chế độ API/UI dùng file để bootstrap lần đầu, sau đó config đã apply lưu bền trên server là nguồn chính. CLI reload file không được ghi đè ngoài quy trình revision. Restart phục hồi config đã apply gần nhất, injection mặc định disabled; không phục hồi enabled từ lần chạy trước, giữ lựa chọn chủ động `--start-enabled` hiện có.

Contract hiện thực: `serve --data-dir` bật managed mode; `state.json` lưu config hợp nhất, revision, apply result và 1.000 audit gần nhất. Apply commit storage trước khi công bố snapshot; lỗi durability sau rename dừng instance để phục hồi. Unix admin chỉ đọc status; `configure --data-dir --config` thay config hạ tầng khi instance đã dừng. Tài khoản qua lệnh local `user`, hai role `viewer`/`editor`; session 8 giờ, cookie Secure/HttpOnly/SameSite và CSRF token, giới hạn 20 login/phút toàn instance và 256 session. Password/role thay đổi hoặc xóa user thu hồi session ở request kế tiếp. Draft lưu trong bộ nhớ tab, không qua reload trang; API chỉ nhận rules và base revision. Xem [hướng dẫn vận hành/test](examples/tester/README.md) và [kết quả kiểm chứng](plans/07-tester-experience/acceptance.md).

Tester chỉnh rules, tỷ lệ và tham số fault trên proxy có sẵn; xem diff, validate/apply result, draft/active revision, status và counters. Listener/upstream/TLS do người vận hành quản lý. Chưa gồm quản lý nhiều server hoặc lịch sử traffic dài hạn. Xem [phase 07](plans/07-tester-experience/README.md) và các tiêu chí P19/P20 cho persistence, quyền, audit và UI.

## 9. Kiến trúc có thể mở rộng

```text
File + CLI (MVP)             UI + Admin API (sau này)
       │                              │
       └────────── Config service ────┘
            parse / validate / apply / enable
                         │
           Config + injection state snapshot
                         │
Client ↔ Protocol adapter ↔ Rule engine ↔ Fault executor ↔ Upstream
               │                 │              │
               └─────────────────┴──────────────┘
                          Events / counters
```

Sơ đồ biểu diễn trách nhiệm xử lý; adapter vẫn sở hữu I/O hai chiều, engine không tự gửi HTTP hay parse bytes của mọi protocol.

| Thành phần | Trách nhiệm |
| --- | --- |
| Config service | Schema version, validation, snapshot, apply/revision và công tắc injection |
| Protocol adapter | Nhận/chuyển tiếp traffic, định nghĩa flow, metadata, phases và khả năng can thiệp |
| Rule engine | Match, sequence, selector, tạo quyết định fault từ snapshot |
| Fault executor | Thực thi action bằng capability adapter cung cấp; tôn trọng cancellation và deadline |
| Recorder | Sự kiện có cấu trúc, counters, timeline với tài nguyên giới hạn |
| Correlation/assertion, về sau | Dùng sự kiện quan sát để nhóm attempt và đánh giá điều kiện test |

Nguyên tắc mở rộng:

- Core chỉ cần flow/event, revision, metadata và quyết định fault; kiểu request/response HTTP nằm trong adapter.
- Adapter công bố capability gồm matcher fields, phases, action, direction và scope. Validate phải kiểm tra cả tổ hợp, không chỉ tên action.
- Thêm loại lỗi không buộc mọi adapter hỗ trợ nó. Ví dụ TCP không nhận rule `http.status`, HTTP/2 reset stream không đồng nghĩa đóng cả connection.
- Phân biệt protocol với công nghệ: PostgreSQL, Kafka và AMQP cần hiểu wire protocol của chúng; chạy qua generic TCP chỉ cung cấp lỗi ở transport level.
- Đã chốt đăng ký adapter/action trong source Go và build lại khi thêm mới; hiện không cần extension hoặc runtime plugin.
- Chưa xây coordinator phân tán. Ranh giới config và engine chỉ cần đủ để thêm UI/API và adapter tiếp theo.

### 9.1. Ràng buộc cần giữ khi mở rộng protocol

- **TLS:** semantic matcher cần proxy nhìn thấy nội dung đã giải mã. Tunnel chuyển dữ liệu mã hóa không cung cấp HTTP path/status để match; termination cần cấu hình chứng chỉ và trust phù hợp. Đây là hệ quả thiết kế từ phân biệt tunnel và HTTP intermediary trong [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html#section-9.3.6).
- **HTTP/2, gRPC:** phải phân biệt stream và connection, tránh một fault ngoài ý muốn ảnh hưởng các RPC khác cùng connection. RFC mô tả HTTP/2 có multiplexing trong [tổng quan HTTP](https://www.rfc-editor.org/rfc/rfc9110.html#section-3.7).
- **Kafka:** không cam kết chỉ đổi bootstrap address là toàn bộ traffic sẽ đi qua proxy. Broker công bố địa chỉ cho client qua `advertised.listeners`; thiết kế adapter phải xử lý routing đến các địa chỉ đó. Xem [Apache Kafka broker configuration](https://kafka.apache.org/37/configuration/broker-configs/#brokerconfigs_advertised.listeners).
- **Database transaction:** event phản hồi của một statement không được tự diễn giải thành business transaction đã commit. Adapter phải định nghĩa điểm quan sát và bằng chứng cho từng loại test.

## 10. Quan sát và giới hạn kết luận

Mỗi flow cần ghi tối thiểu: `run_id`, `flow_id`, `proxy_id`, protocol, config revision, injection state/control sequence, thời điểm bắt đầu/kết thúc, rule nếu có, eligible sequence, quyết định selector, phase/action thực thi và kết quả thực tế.

Các kết quả phải phân biệt: pass-through, upstream error, fault selected nhưng chưa đến phase, fault applied, client canceled, proxy timeout và proxy internal error. Không tính lỗi upstream tự xảy ra vào số lỗi do Faultline inject.

Counters cần có tổng flow, rule eligible, selected, applied, active flow và số sự kiện bị bỏ do đầy hàng đợi. Không đưa flow ID hoặc header người dùng vào metric label có cardinality không giới hạn.

Đề xuất log JSON qua stdout trong MVP; người dùng lưu thành artifact CI. Không log body, Authorization hoặc toàn bộ headers mặc định. Recorder dùng queue có giới hạn; nếu đầy thì bỏ event và tăng counter thay vì block vô hạn. Báo cáo về sau phải đánh dấu dữ liệu thiếu.

Các giới hạn phải thể hiện đúng trong sản phẩm:

- Upstream trả 201 chỉ chứng minh proxy quan sát thấy status 201; business assertion cần nguồn kiểm tra độc lập để chứng minh chỉ có một payment.
- Có cùng payload không đủ chứng minh hai request là retry. Correlation cần key có ý nghĩa và phạm vi/window rõ ràng.
- Nếu nhóm bằng Idempotency-Key thì không thể tự phát hiện key đã đổi giữa hai attempt; muốn assert key được giữ phải có operation ID độc lập hoặc test driver cung cấp liên kết.
- Proxy nhìn thấy client disconnect/cancel không chứng minh mọi công việc bên trong upstream đã dừng. Báo cáo cancellation toàn chuỗi cần instrumentation hoặc quan sát bổ sung.
- Không có traffic trong một khoảng thời gian không tự chứng minh circuit breaker đã mở.

Giai đoạn assertions phải hỗ trợ kết quả **không đủ dữ liệu / inconclusive**, không gán PASS khi thiếu correlation hoặc quan sát bị mất. Amplification chỉ được tính khi xác định được logical operations và ghi rõ phạm vi quan sát.

## 11. Vận hành và giới hạn tài nguyên

Đã chốt chạy binary và Docker để test lỗi ở dev. Có định hướng server test dùng chung và UI sau này; hiện developer là người dùng chính. CLI quản trị chạy cục bộ trên host hoặc trong container; chưa cần mở API quản trị từ xa cho tester ở MVP.

- Listener bind loopback mặc định; deployment container có thể cấu hình địa chỉ bind rõ ràng. Kênh quản trị dùng Unix socket riêng với quyền filesystem, không mở cổng TCP quản trị.
- Admin channel tách biệt traffic được inject lỗi. Khi đưa lên server cho tester, bổ sung authentication, authorization và audit thay đổi config.
- Delay/hold phải hủy được khi client cancel, request deadline hoặc process shutdown; không để goroutine/timer tồn tại vô hạn.
- Defaults: `max_inflight_requests: 1000`, `request_timeout: 30s` cho deadline flow và giới hạn headers/read/write/idle; recorder queue 1024 qua `--event-buffer`. Override trước startup; inflight không giới hạn tổng TCP connections.
- Khi vượt inflight limit, MVP đề xuất trả 503 do proxy overload và ghi riêng; không đưa request bị từ chối này vào eligible counters của rule.
- `request_timeout` là deadline toàn flow; nếu hết trước fault duration thì ghi proxy timeout, không báo action đã hoàn tất đúng duration.
- Proxy không tự retry upstream để tránh làm sai số attempt và side effect đang kiểm thử.
- CLI shutdown ngừng nhận flow mới, chờ tối đa 5s rồi cancel phần còn lại; recorder flush tối đa 2s. Docker stop nên cho ít nhất 10s.
- Faultline dừng/crash thì đường gọi qua proxy không còn hoạt động; chưa có bypass hoặc high availability tự động.

**Trả lời Q10:** đúng, RPS, connection, payload và timeout phụ thuộc test case. Không cần chốt một tải cố định để bắt đầu MVP. Cần phân biệt tham số test case với khả năng và giới hạn của proxy: nếu proxy tự quá tải thì lỗi đó phải được phân biệt với lỗi người dùng muốn inject.

Defaults và override đã có tài liệu tại [README](README.md#runtime-defaults), kèm [benchmark và recovery](plans/05-mvp-delivery/benchmark-results.md). Một test có nhiều request bị giữ lâu cần inflight limit khác test request ngắn. Số đo cục bộ không phải throughput SLA.

Benchmark pass-through so với gọi trực tiếp trong cùng môi trường, sau đó đo khi có nhiều delay/hold, ghi tài nguyên máy và config đi kèm. Binary/Docker đều là hình thức phát hành MVP; hệ điều hành/kiến trúc binary sẽ xác định theo môi trường phát triển khi triển khai.

## 12. Tiêu chí nghiệm thu MVP

| ID | Tình huống kiểm tra | Kết quả mong đợi |
| --- | --- | --- |
| AC1 | Gửi request không match rule hoặc khi injection disabled | Upstream nhận đúng method/path/query/body; client nhận status/body như baseline |
| AC2 | Hai listener đến hai upstream, fault chỉ ở một listener | Listener còn lại không bị inject lỗi |
| AC3 | Injection enabled, probability lần lượt là 0 và 1; dùng close_connection trước upstream | Không request eligible nào được chọn ở 0; mọi request eligible ở 1 bị đóng connection, upstream không được gọi |
| AC4 | Cùng seed/config và chuỗi request tuần tự, chạy lại | Chuỗi quyết định selected giống nhau; thống kê xác suất kiểm tra với cỡ mẫu/dung sai định trước |
| AC5 | `nth: 3`, `every: 10` và traffic xen kẽ không match | Chỉ sequence eligible tương ứng bị chọn; traffic không match không làm tăng counter |
| AC6 | Hai rule cùng matcher, rule đầu không chọn inject | Pass-through, không rơi xuống rule thứ hai |
| AC7 | Delay 500 ms trên upstream phản hồi nhanh | Phase bị chậm thêm trong dung sai timing của môi trường test; cancel sớm giải phóng flow |
| AC8 | `respond` với status 503 | Client nhận response cấu hình; upstream không được gọi |
| AC9 | Upstream demo ghi payment rồi trả 201; hold response lâu hơn client timeout | Client timeout, payment vẫn tồn tại; timeline ghi đã nhận headers và đã giữ response |
| AC10 | Reload probability 0 → 1 trong khi có request đang chạy | Request cũ giữ revision cũ; request mới, kể cả cùng keep-alive connection, dùng revision mới |
| AC11 | Reload config sai hoặc đổi field cần restart | Apply thất bại toàn bộ, revision đang chạy giữ nguyên |
| AC12 | Reload cùng config, sau đó config có thay đổi | No-op giữ counters; thay đổi thật tạo revision mới và reset counters theo đặc tả |
| AC13 | Request được chọn nhưng upstream lỗi trước phase | Ghi selected và not_reached; applied không tăng |
| AC14 | Nhiều delay/hold, client cancel và shutdown | Không giữ flow vượt deadline; tài nguyên quay về gần baseline sau test |
| AC15 | Recorder đầy hoặc proxy overload | Ghi counter phân biệt dữ liệu thiếu/lỗi proxy; không báo thành fault được inject |
| AC16 | Đọc log của một request lỗi | Xác định được proxy, rule, revision, quyết định, phase và lỗi quan sát thực tế |
| AC17 | Serve mặc định với rule enabled và probability 1 | App vẫn gọi dependency qua proxy khi startup; counters eligible chưa tăng; status báo disabled |
| AC18 | App ready, enable rồi disable khi vẫn có request chạy | Chỉ request mới nhận state mới; request cũ giữ quyết định; status ghi rõ flow còn chịu fault |
| AC19 | Enable/disable nhiều lần, reload khi disabled, restart bình thường | Enable/disable giữ counters; reload giữ state, reset counters nếu config đổi; restart trở lại disabled |
| AC20 | Cấu hình nth: 1, gửi traffic startup khi disabled rồi enable | Request eligible đầu tiên sau enable được chọn; traffic startup không tiêu thụ sequence |
| AC21 | hold_request trước upstream, client timeout ngắn hơn max_duration | Client timeout, upstream không nhận request; cancellation giải phóng flow |
| AC22 | Chạy bốn tổ hợp HTTP/HTTPS hai phía với cert/trust hợp lệ | Pass-through và fault tại HTTP phases hoạt động theo cùng semantics |
| AC23 | Upstream TLS sai hostname/CA hoặc listener cert/key không hợp lệ | Phân biệt lỗi TLS tự nhiên với fault inject; cert/key cấu hình sai khiến startup thất bại |
| AC24 | Chạy bằng binary và Docker với config/cert phù hợp | Cùng hành vi validate/serve/enable/reload/disable và truy cập được kênh quản trị local |

Demo trọng tâm dùng payment service test có thể kiểm tra dữ liệu độc lập. Chạy hai biến thể có/không xử lý idempotency để cho thấy cùng một lost-response fault dẫn đến kết quả khác nhau. Ở MVP, assertion nằm trong integration test của demo; chưa cần assertion DSL trong Faultline.

## 13. Lộ trình đề xuất

| Giai đoạn | Kết quả bàn giao | Điều kiện chuyển tiếp |
| --- | --- | --- |
| A — Proxy MVP | HTTP/1.1 qua HTTP/HTTPS, binary/Docker, YAML/CLI, enable/disable, một action/request, selector, reload, JSON events, payment demo | Đạt các AC phía trên; chưa cần mTLS, HTTP/2 hoặc truncate body |
| B — Mở rộng core | gRPC unary; HTTP/2 hai phía, mTLS tùy chọn; truncate/throttle request/response theo phase 06 | Chứng minh thêm protocol không viết lại rule/config engine |
| C — Trải nghiệm tester | API/UI chỉnh cấu hình, revision, apply result, quyền và audit | Tester tự chỉnh tỉ lệ và kiểm tra bản đang chạy mà không sửa file trên server |
| D — Failure testing | Scenario timeline, correlation, assertions, report và exit code CI | Phân biệt PASS/FAIL/inconclusive và tái hiện demo bằng scenario |
| E — Semantic adapters | PostgreSQL, AMQP hoặc Kafka theo ưu tiên sử dụng | Mỗi adapter có use case, capability matrix và integration test riêng |

Ưu tiên A trước; UI hướng đến server test dùng chung nhưng tester chưa là người dùng chính nên chưa cần đưa lên MVP. B/C/D được ưu tiên tiếp theo theo nhu cầu thực tế, không buộc phải hỗ trợ hết protocol mới làm UI. Assertions chưa cần quyết định nguồn dữ liệu ở giai đoạn A.

## 14. Kết quả review và phản hồi

Giữ nguyên câu hỏi và câu trả lời bên dưới để có ngữ cảnh; một số đề xuất trong câu hỏi cũ đã được thay thế ở phần đặc tả phía trên. **Phản hồi** giải thích hoặc ghi nhận quyết định; không thay đổi nội dung bạn đã trả lời. Q6 và Q7 đã được trả lời bổ sung, không còn câu hỏi đang chờ để chốt phạm vi MVP. Giải pháp cho Q3/Q5 tiếp tục là phương án thiết kế đề xuất, có tiêu chí kiểm chứng trong tài liệu.

### Q1. Dependency và protocol đầu tiên cần kiểm thử là gì?

Một ví dụ cụ thể về application → dependency sẽ giúp chọn HTTP trước hay generic TCP trước. Đề xuất hiện tại: HTTP/1.1, demo lost response khi tạo payment. Nếu ưu tiên PostgreSQL/Redis/Kafka, xin ghi công nghệ và tình huống lỗi cần nhất.

**Trả lời:** Tập trung vào HTTP trước

**Phản hồi — đã chốt:** HTTP là protocol đầu tiên. Theo trả lời bổ sung Q6, MVP dùng HTTP/1.1 qua HTTP/HTTPS; HTTP/2 để sau. Không cần chọn database/broker lúc này.

### Q2. MVP cần tập trung proxy inject lỗi, hay cần tự đánh giá PASS/FAIL ngay?

Đề xuất: MVP cung cấp proxy, log và counters; test bên ngoài assert. Correlation, scenario DSL và assertion engine ở giai đoạn sau. Việc này thu hẹp phạm vi so với framework đầy đủ trong bản gốc.

**Trả lời:** Tập trung vào inject lỗi. Mục tiêu của MVP là với những lỗi network thì app có cơ chế xử lý, báo lỗi không hay sẽ tạo ra các bug tìm ẩn.

**Phản hồi — đã chốt:** MVP tạo tình huống lỗi để bạn quan sát app xử lý/báo lỗi hoặc tìm bug tiềm ẩn. Faultline ghi bằng chứng injection; developer và test hiện có đánh giá hành vi app. Chưa cần assertion engine hay tự kết luận PASS/FAIL.

### Q3. “Config động” cần thao tác và hiệu lực như thế nào?

Đề xuất: sửa file rồi gọi reload; chỉ request mới dùng cấu hình mới, request đang chạy giữ cấu hình cũ. Bạn có cần auto-watch file, lịch đổi tỉ lệ tự động, hoặc nút dừng ngay các fault đang chạy từ bản đầu không?

**Trả lời:** Request mới mới cần áp dụng cấu hình mới. Những lỗi mạng xảy ra trong quá trình hoạt động. Nếu cấu hình lỗi ngay lập tức thì làm sao app có thể khởi động. Bạn hãy nghĩ giải pháp

**Phản hồi — giải pháp đề xuất:** Tách nạp rule khỏi bật injection. Proxy khởi động ở pass-through dù file có probability 100%; bạn khởi động app, đợi app ready rồi gọi `faultline enable`. Gọi `faultline disable` để request mới trở lại bình thường. Reload đổi rule nhưng không tự bật lỗi. Nếu chủ động test lỗi startup, dùng `--start-enabled`. Chi tiết, counters và tiêu chí hiệu lực nằm ở mục 8.1; không dùng thời gian warm-up cố định hoặc sửa code app.

### Q4. Tỉ lệ lỗi mong muốn là xác suất hay quota chính xác?

Đề xuất: 20% nghĩa là xác suất trên từng request eligible, không đảm bảo đúng 20/100. Có cần đúng N request lỗi trong mỗi batch/window, hoặc áp dụng theo connection/logical operation thay vì attempt không?

**Trả lời:** xác suất trên từng request eligible. Nhưng nếu tôi để là 100% thì chắc chắn phải bị lỗi. Mục tiêu là test ngay trường hợp lỗi đó

**Phản hồi — đã chốt xác suất và hai biên:** Khi injection enabled, 100% luôn chọn mọi request eligible; không có lượt may mắn đi qua selector. Để test lỗi ngay, chọn `close_connection` tại `before_upstream_request`: mọi request eligible bị đóng connection trước khi gọi dependency. Nếu chọn delay 100 ms thì 100% request bị delay, nhưng app có timeout 5 giây vẫn có thể thành công; mất response sau headers cũng chỉ áp dụng khi đã tới phase đó. Vì vậy action/phase phải thể hiện đúng lỗi bạn muốn ép xảy ra, không chỉ cấu hình tỷ lệ.

### Q5. Ba tình huống lỗi quan trọng nhất và phạm vi partition là gì?

Ví dụ: chậm 2 giây, trả 503, mất response sau upstream thành công, hoặc chặn toàn bộ đường A → B trong 30 giây. Partition có cần tác động ngay lên connection/request đang chạy, hoặc chỉ lên request mới là đủ?

**Trả lời:** MVP giả lập các lỗi mạng, bạn thấy có nên tác động lên connection/request không?

**Phản hồi — nên, theo phạm vi cụ thể:** Delay/hold tác động lên request; disconnect tác động lên connection phục vụ request được chọn. Đề xuất không cắt hàng loạt request đang chạy từ trước lúc bật/reload, đúng yêu cầu Q3. Request mới vẫn có thể bị ngắt giữa quá trình xử lý ở phase sau upstream headers. Mục 5.3 có ví dụ R1/R2; cắt giữa body hoặc toàn bộ connection đang tồn tại là phần mở rộng sau.

### Q6. Môi trường đầu tiên có bắt buộc HTTPS/TLS, mTLS hoặc HTTP/2 không?

Đề xuất MVP HTTP rõ trong môi trường test. Nếu không thể đổi endpoint/cấu hình trust của client, mô hình triển khai cần được thiết kế lại trước khi hiện thực.

**Trả lời:** nên có 

**Phản hồi lần 1:** Câu trước gộp HTTPS, mTLS và HTTP/2 nên đã được tách thành các lựa chọn dưới đây. Quyết định sau trả lời bổ sung được ghi ở cuối Q6.

| Khả năng | Khi nào cần | Đề xuất hiện tại |
| --- | --- | --- |
| HTTPS/TLS | App hoặc dependency dùng URL HTTPS | Đưa vào MVP, gồm chứng chỉ và trust |
| mTLS | Server yêu cầu bên gọi xuất trình client certificate | Đưa vào MVP nếu môi trường hiện có bắt buộc; cần biết phía app → proxy, proxy → upstream hay cả hai |
| HTTP/2 | Client/upstream cần HTTP/2 hoặc test nhiều stream chung connection | Đưa vào MVP nếu cần ngay; nếu HTTP/1.1 đáp ứng use case thì để sau |

**Câu hỏi bổ sung đã trả lời:** HTTPS thông thường đã đủ cho môi trường dev của bạn chưa, hay MVP cần cả mTLS và/hoặc HTTP/2? Nếu cần mTLS, ghi phía kết nối cần xác thực client.

**Trả lời bổ sung:** tạm thời có https

**Đã chốt sau review lần 2:** MVP có HTTPS thông thường ở hai phía của proxy; mTLS và HTTP/2 để sau. Không cần trả lời thêm Q6.

### Q7. Có cần phối hợp nhiều loại lỗi trên cùng traffic không?

Đề xuất: một rule đầu tiên match sở hữu request, một action/request. Có cần vừa delay rồi cắt response, hoặc chia traffic thành các nhóm 20% delay, 10% status lỗi, 70% bình thường không?

**Trả lời:** Cho tôi ưu và nhược trong hai trường hợp cần phối hợp nhiều loại lỗi và không

**Phản hồi:** Bảng ưu/nhược và ví dụ ở mục 6.4. Một action/request dễ tìm nguyên nhân và tái hiện lỗi nhất. Chia traffic thành các nhóm cho phép nhiều loại lỗi trong một lần chạy nhưng cần selector có trọng số. Nối nhiều action trên cùng request mô phỏng tình huống phức tạp hơn nhưng phải xử lý thứ tự và action không đến được vì timeout/ngắt trước đó.

**Đề xuất:** Một action/request cho MVP, nhiều rule vẫn được áp dụng cho các matcher khác nhau. Khi cần mở rộng, thêm chia nhóm có trọng số trước, chuỗi action sau.

**Câu hỏi bổ sung đã trả lời:** Giữ một action/request cho MVP, hay đưa chia nhóm tỷ lệ hoặc chuỗi action vào ngay?

**Trả lời bổ sung:** Giữ một action/request cho MVP

**Đã chốt sau review lần 2:** Mỗi request chỉ có tối đa một action; không có chia nhóm trọng số hoặc chuỗi action trong MVP. Không cần trả lời thêm Q7.

### Q8. Sau reload, counters nên reset hay giữ liên tục?

Đề xuất reset khi config hiệu lực thay đổi để mỗi revision dễ giải thích và tái hiện. Nếu đang dùng `nth`/`every`, reload có thể làm các vị trí inject bắt đầu lại.

**Trả lời:** reset

**Phản hồi — đã chốt:** Config hiệu lực thay đổi thì reset counters của revision mới; reload cùng nội dung hiệu lực là no-op. Công tắc enable/disable mới đề xuất không sửa config và chỉ tạm dừng/tiếp tục đếm; không tự reset mỗi lần bật.

### Q9. Tester cần UI ở thời điểm nào, và sẽ dùng trên môi trường nào?

Local trên máy cá nhân, một server test dùng chung hay nhiều môi trường? Đề xuất UI sau proxy MVP; nếu tester là người dùng chính ngay từ đầu thì đưa UI lên trước adapter thứ hai.

**Trả lời:** Một server test dùng chung. Tester chưa phải người dùng chính bây giờ

**Phản hồi — đã chốt:** UI hướng tới server test dùng chung nhưng chưa thuộc MVP. Trước mắt developer dùng CLI trên máy/host chạy proxy; khi có UI từ xa sẽ thêm quyền thao tác, audit và xử lý xung đột config.

### Q10. Quy mô và ràng buộc tài nguyên dự kiến?

Xin ghi ước lượng RPS, connection đồng thời, payload lớn nhất, request/stream dài nhất, hệ điều hành và cách chạy mong muốn: binary, Docker Compose hoặc Kubernetes.

**Trả lời:** Chạy binary, docker. Mục tiêu là để test lỗi ở dev. Những giá trị bạn yêu cầu còn phải phụ thuộc vào test case cụ thể chứ?

**Phản hồi:** Đúng, mỗi test case có thể có tải, payload và timeout khác nhau; không cần bạn đưa một bộ số cố định lúc này. Câu hỏi nhằm tránh nhầm lỗi proxy quá tải với lỗi được inject. MVP sẽ chạy binary/Docker, có defaults và giới hạn cấu hình được trước mỗi lần chạy; benchmark công bố điều kiện đo. Không đặt mục tiêu load testing hoặc SLA throughput cho MVP. Chi tiết ở mục 11.

### Q11. Nhu cầu tái hiện lỗi trong CI nghiêm ngặt đến đâu?

Đề xuất đảm bảo cùng quyết định khi cùng seed và thứ tự input. Nếu cần cùng logical operation luôn bị lỗi dưới tải đồng thời, ứng dụng/test có thể gửi operation ID hoặc header điều khiển ổn định không?

**Trả lời:** Vì không can thiệp vào code nên không đảm bảo

**Phản hồi — ghi nhận ràng buộc:** Chức năng cơ bản không cần thêm header/ID hoặc SDK. Seed chỉ bảo đảm chuỗi quyết định khi thứ tự request eligible giống nhau; không hứa cùng nghiệp vụ luôn lỗi dưới tải đồng thời. Bạn vẫn có thể test lỗi chắc chắn bằng probability 1 hoặc tái hiện với test tuần tự dùng `nth`/`every` và metadata đã có.

### Q12. Khi thêm assertions, nguồn kiểm chứng nghiệp vụ sẽ là gì?

Có endpoint test, quyền đọc database hay script kiểm tra side effect không? Có sẵn operation ID để liên kết retry độc lập với Idempotency-Key không? Đây là dữ liệu cần để kiểm tra “chỉ có một payment” thay vì chỉ quan sát response.

**Trả lời:** Chưa rõ câu hỏi

**Phản hồi — giải thích bằng ví dụ:** Giả sử app tạo payment, dependency đã ghi dữ liệu nhưng Faultline giữ response khiến app timeout và retry. Proxy biết nó đã giữ response, nhưng không biết database cuối cùng có một payment hay hai payment bị tạo trùng.

“Nguồn kiểm chứng nghiệp vụ” là cách kiểm tra kết quả đó, chẳng hạn mở màn hình danh sách payment, dùng API có sẵn, chạy truy vấn database hoặc integration test hiện có. “Assertion” chỉ là điều kiện mong đợi, ví dụ **sau retry vẫn chỉ có một payment**. Nếu sau này Faultline tự kết luận PASS/FAIL thì phải được cung cấp cách kiểm tra này; không thể tự suy ra chỉ từ traffic.

**Kết luận cho phạm vi hiện tại:** Theo Q2, chưa cần trả lời hoặc xây chức năng này trong MVP. Bạn chủ động kiểm tra app bằng công cụ hiện có; demo có thể dùng integration test riêng để minh họa. Không yêu cầu bạn thêm endpoint hoặc sửa code app ngay bây giờ.

### Q13. Cách mở rộng bằng Go và build lại có đáp ứng nhu cầu không?

Đề xuất adapter/action nằm trong source và đăng ký trong binary. Nếu người dùng bên ngoài cần tự viết extension mà không build lại Faultline, xin nêu use case để đánh giá một contract plugin riêng sau này.

**Trả lời:** Cứ để adapter/action trong source. Hiện không cần dạng extension hay plugin

**Phản hồi — đã chốt:** Adapter/action nằm trong source Go, đăng ký trong binary và build lại khi thêm mới. Không thiết kế extension hoặc plugin runtime trong MVP.
