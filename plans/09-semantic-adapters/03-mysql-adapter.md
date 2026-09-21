# P25 — Adapter MySQL

- Trạng thái: **Done — xem [acceptance](mysql-acceptance.md)**.
- Quyết định: người dùng chọn MySQL 8.4 LTS và fixture Go ngày 2026-09-17.
- Phụ thuộc: tái sử dụng nền config/control/UI và kinh nghiệm P23/P24; không phụ thuộc Phase 8.
- Nguồn: [phạm vi Phase 9](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi dự kiến: `internal/proxy/mysql/`, config, CLI, API/UI capabilities,
  recorder nếu cần; `examples/mysql/` và `tests/integration/`.

## DEFINE

Thêm adapter hiểu giao thức MySQL để tái hiện database đã commit nhưng client
không nhận được xác nhận. Không dùng generic TCP để suy đoán transaction outcome.

Phạm vi đã chọn:

- MySQL 8.4 LTS, kết nối trực tiếp, bảng InnoDB, transaction tường minh.
- Fixture Go; MySQL 8.4.8 và `github.com/go-sql-driver/mysql v1.10.1`, pin digest tại
  [contract](mysql-contract.md); không claim mọi patch, driver hoặc ORM.
- Phase duy nhất `after_commit`; action `delay`, `hold_response`, `close_connection`.
- Matcher rỗng, dùng enabled và probability/nth/every hiện có; không match SQL.
- Tích hợp CLI, managed API/UI, counters và log theo cấu trúc hiện tại.
- Không thêm fixture Node.js/TypeORM, MariaDB, replication/binlog, cluster, XA,
  stored procedures, multi-statements, compression, LOCAL INFILE, mTLS hoặc SQL/result editing.
- Autocommit writes và implicit commits không thuộc điểm inject/nghiệm thu;
  không được gắn nhãn confirmed explicit COMMIT cho các trường hợp này.

## PLAN → BUILD

### 1. Contract trước implementation

Đọc tài liệu MySQL/driver chính thức và ghi trace từ fixture để chốt:

1. Handshake, capability flags, authentication `caching_sha2_password`, auth switch,
   fast/full authentication; kết nối plaintext/TLS từng chặng. Kiểm chứng cold và
   warm authentication cache. Chốt rõ RSA/full-auth trên plaintext: chỉ công bố
   hỗ trợ khi đã kiểm chứng; nếu chưa hỗ trợ phải từ chối rõ, không hạ bảo mật.
2. Packet framing/sequence, response OK/ERR/result sets, command boundaries,
   prepared statements và pooling mà driver fixture thực sự sử dụng. Không nhầm
   OK packet của command khác hoặc phần kết thúc result set với xác nhận COMMIT.
3. Quy tắc nhận diện explicit COMMIT: kết hợp command trong phạm vi cú pháp đã
   định nghĩa, trạng thái transaction và phản hồi thành công từ upstream. Không
   dùng tìm substring `COMMIT` hoặc chỉ thấy OK để kết luận. Phải định nghĩa cách
   xử lý comment, nhiều statement và command không hỗ trợ; không xây SQL parser tổng quát.
4. START TRANSACTION/BEGIN, COMMIT/ROLLBACK, lỗi statement và commit failure;
   implicit commit, autocommit và session reset phải không gây false positive.
5. TLS termination ở proxy, xác thực CA/hostname upstream, không fallback plaintext.
   Chốt matrix TLS/plaintext khả thi với authentication; ghi rõ tổ hợp bị từ chối.
6. Đơn vị flow dự kiến là command cycle; snapshot pin ở đầu cycle, chọn fault chỉ
   tại confirmed explicit COMMIT. Reload/disable ảnh hưởng cycle mới; kết nối
   pooled dài hạn nhận revision mới. Disable không giải phóng fault đã bắt đầu.
7. Giới hạn packet/buffer/session và deadline; policy với fragmentation, EOF,
   timeout, client disconnect, shutdown và command ngoài phạm vi. Không giả định
   cơ chế cancellation của PostgreSQL áp dụng cho MySQL.

Ghi kết quả tại `mysql-contract.md` khi thực hiện bước này. Đây là gate trước
code adapter; không coi các lựa chọn wire/auth chưa kiểm chứng là capability đã có.

### 2. Adapter và tích hợp

- Đề xuất config `protocol: mysql`, upstream `mysql://host:3306` hoặc
  `mysqls://host:3306`; TLS theo mỗi chặng. Chốt tên/schema trong contract trước code.
- Credentials do client cung cấp qua handshake, không nhúng trong upstream URL.
- Reuse engine/control/fault theo trách nhiệm hiện tại; protocol I/O nằm trong adapter.
  Không tạo framework plugin hoặc refactor PostgreSQL nếu không cần.
- Tại confirmed COMMIT, chặn toàn bộ acknowledgment trước khi gửi client:
  delay chờ rồi chuyển tiếp; hold_response chờ giới hạn rồi đóng; close_connection
  đóng ngay. Client error không đồng nghĩa rollback; proxy không tự retry.
- Validate matcher/phase/action/parameters; API capabilities và UI chỉ hiện lựa chọn
  MySQL hợp lệ, không có HTTP method/path/headers.
- Recorder phân biệt confirmed commit, fault applied và client outcome; không log
  credentials, auth payload, SQL hoặc parameter values.

### 3. Fixture và hướng dẫn

- Dependency MySQL Docker tạm, port động, cleanup; proxy chạy binary local và có
  cấu hình Docker mẫu. Không dùng database người dùng để chạy automated tests.
- Go fixture tạo transaction ghi dữ liệu rồi commit. Kết nối độc lập đọc dữ liệu
  sau lỗi client, retry cùng operation ID cho bảng thường và bảng có unique key:
  chứng minh 2 row so với 1 row. Kết nối lỗi phải được loại khỏi pool/reconnect.
- Hướng dẫn chạy CLI và UI; config hỗn hợp MySQL/PostgreSQL/HTTP/gRPC để kiểm tra
  chuyển proxy, draft, diff, validate/apply và restart managed config.
- Docker local đã có. Agent chạy MySQL thật, CLI/integration, managed API và JavaScript regression.
- Không lặp lại manual UI nếu tái sử dụng form PostgreSQL; chỉ yêu cầu manual khi có giao diện hoặc interaction mới.

## VERIFY

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P25-AC1 | Contract pin image/driver, capability/auth/TLS matrix, traces và resource bounds; trường hợp không hỗ trợ có policy rõ. |
| P25-AC2 | Baseline auth, query/result, prepared statements trong matrix, BEGIN/INSERT/COMMIT/ROLLBACK và client pool hoạt động trên MySQL thật. TLS hợp lệ thành công; CA/hostname sai bị từ chối. |
| P25-AC3 | Delay trả ACK sau khoảng cấu hình; hold/disconnect gây lỗi client nhưng kết nối độc lập thấy dữ liệu đã commit; cả ba action có test thật. |
| P25-AC4 | Rollback, commit thất bại, OK không phải COMMIT, autocommit/implicit commit và chuỗi COMMIT trong dữ liệu/comment không bị nhận nhầm. Lỗi statement có test theo trạng thái transaction thực của MySQL. |
| P25-AC5 | Probability 0/1, nth/every, thứ tự rule, disabled rule, no-op/reload và snapshot trên session dài hạn đúng contract; disable không giải phóng hold đang chạy. |
| P25-AC6 | Timeout/disconnect/shutdown, malformed/unsupported packets và giới hạn tài nguyên kết thúc có bound, counters về 0; không lộ secrets/SQL. |
| P25-AC7 | Retry demo có bằng chứng độc lập 2/1 row và pool recovery; test actual binary validate/serve/status/enable/disable/reload ở file mode; managed mutations qua API có auth. |
| P25-AC8 | Go tests/race/vet, binary/Docker build và HTTP/gRPC/PostgreSQL regression pass; ghi riêng build và Docker runtime đã/chưa chạy. |
| P25-AC9 | UI MySQL đúng capability, diff/apply/validation, toggle và chuyển qua các protocol không lỗi; có automated JavaScript/API regression; manual chỉ khi thêm giao diện/interaction mới. |

Ghi lệnh, phiên bản, kết quả PASS/FAIL/SKIP và giới hạn tại `mysql-acceptance.md`
khi thực hiện. Không phụ thuộc scenario runner Phase 8.

## REVIEW

Đối chiếu trace với semantics: chỉ dùng acknowledgment thành công của explicit
COMMIT đúng cycle làm bằng chứng; không đồng nhất mọi OK với commit. Kiểm tra
TLS/auth không downgrade, packet bounds, cleanup và hồi quy adapter cũ.
Chỉ chuyển Done khi P25-AC1–AC9 đạt; manual UI chỉ bắt buộc khi thêm giao diện/interaction mới.

## Tiến độ

Đã hiện thực adapter/config/CLI/API/UI, fixture và hướng dẫn. P25-AC1–AC9 PASS theo [acceptance](mysql-acceptance.md). Không thêm interaction UI; nghiệm thu bằng JavaScript/API theo quyết định người dùng. Mixed TLS/plaintext bị từ chối theo contract; hỗ trợ hai chặng cùng plaintext hoặc cùng TLS.
