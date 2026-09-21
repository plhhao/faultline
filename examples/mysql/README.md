# MySQL: mất xác nhận COMMIT

Adapter cho MySQL **8.4.8**, Go driver **v1.10.1**, bảng InnoDB và transaction tường minh.
Ba action: `delay`, `hold_response`, `close_connection`; phase duy nhất `after_commit`.
Proxy chờ database xác nhận commit rồi mới giữ/ngắt ACK. Dữ liệu có thể đã lưu dù client nhận lỗi.

## 1. Dựng dependency và chạy proxy local

Từ thư mục gốc repo:

```bash
rtk proxy docker run -d --name faultline-mysql-demo \
  -p 127.0.0.1:3306:3306 \
  -e MYSQL_ROOT_PASSWORD=fixture-secret \
  -e MYSQL_DATABASE=fixture -e MYSQL_USER=fixture -e MYSQL_PASSWORD=fixture-secret \
  mysql:8.4.8@sha256:2952e3be7807f06fc18de50b3ea1a632d5c70d63482ff7d7376fe3aa8999babf
rtk proxy docker logs faultline-mysql-demo
rtk proxy go build -o bin/faultline ./cmd/faultline
rtk proxy ./bin/faultline validate --config examples/mysql/config.yaml
rtk proxy ./bin/faultline serve --config examples/mysql/config.yaml \
  --admin-socket /tmp/faultline-mysql/admin.sock
```

Chờ MySQL báo ready trên port 3306 trước khi chạy fixture. Proxy listen 13306;
`serve` mặc định tắt injection. Nếu port 3306 đang dùng, publish port khác và sửa upstream/config cùng DSN direct.
Các mật khẩu ở đây chỉ dùng cho database demo tạm.

Terminal khác:

```bash
rtk proxy ./bin/faultline enable --admin-socket /tmp/faultline-mysql/admin.sock
rtk proxy env \
  'FAULTLINE_MYSQL_DIRECT=fixture:fixture-secret@tcp(127.0.0.1:3306)/fixture?timeout=3s&readTimeout=5s&writeTimeout=5s' \
  'FAULTLINE_MYSQL_PROXY=fixture:fixture-secret@tcp(127.0.0.1:13306)/fixture?timeout=3s&readTimeout=5s&writeTimeout=5s' \
  go run ./examples/mysql
```

Kỳ vọng với close_connection/probability 1:

- Cả bốn commit phía client báo `client_commit_success=false`.
- Retry hai lần cùng operation ID: bảng thường `independently_verified_rows=2`.
- Có unique operation ID và upsert: `independently_verified_rows=1`.
- Fixture dùng pool một kết nối, tự reconnect sau mất ACK; kiểm tra bằng kết nối direct riêng, rồi xóa row của lần chạy. Giữ lại hai bảng fixture.

Đổi rule để thử delay/hold:

```yaml
fault: {action: delay, phase: after_commit, duration: 500ms}
# hoặc
fault: {action: hold_response, phase: after_commit, max_duration: 2s}
```

```bash
rtk proxy ./bin/faultline reload --config examples/mysql/config.yaml --admin-socket /tmp/faultline-mysql/admin.sock
rtk proxy ./bin/faultline status --admin-socket /tmp/faultline-mysql/admin.sock
rtk proxy ./bin/faultline disable --admin-socket /tmp/faultline-mysql/admin.sock
```

Delay trả ACK thành công sau thời gian chờ; hold trả lỗi khi hết hold hoặc client timeout.
Disable chỉ ảnh hưởng command mới, không thả hold đã bắt đầu. `request_timeout` cũng giới hạn command/hold và idle session.

## 2. Admin UI hiện có

[ui.yaml](ui.yaml) có MySQL/PostgreSQL/HTTP/gRPC, MySQL mặc định probability 0.
Dùng chung cách thiết lập HTTPS/user trong [hướng dẫn PostgreSQL](../postgresql/README.md).
Nếu dùng data-dir đã tồn tại, dừng instance rồi chạy:

```bash
rtk proxy ./bin/faultline configure --data-dir /tmp/faultline-phase9-ui/data --config examples/mysql/ui.yaml
```

Sau đó `serve` lại với data-dir, cert/key và admin/API port của instance đó.
`configure` thay toàn bộ config đang lưu: giữ lại các rule riêng cần dùng trước khi thay.
MySQL dùng form giống PostgreSQL, không có HTTP method/path/headers. Chỉ có `after_commit` và ba action trên.
Theo quyết định hiện tại, automated JavaScript/API regression là nghiệm thu UI; không cần lặp lại manual UI khi không thêm interaction mới.

## 3. TLS và giới hạn

- Plaintext cả hai chặng: `mysql://...`, không khai báo listener `tls`.
- TLS cả hai chặng: `mysqls://...`, thêm listener `tls.cert_file/key_file` và `upstream_tls.ca_file` nếu CA riêng. Client phải tin CA listener; hostname/SAN phải khớp.
- TLS ở một chặng và plaintext ở chặng kia bị validate từ chối; không tự hạ xuống plaintext.
- Auth `caching_sha2_password`: cold/full RSA trên plaintext hoặc full qua TLS; fast auth được hỗ trợ. Không có mTLS/native-password/MariaDB compatibility claim.
- Nhận diện BEGIN, BEGIN WORK, START TRANSACTION; COMMIT, COMMIT WORK; không phân biệt hoa/thường, cho phép một dấu `;` cuối. Prepared COMMIT cũng hỗ trợ.
- COMMIT có comment/AND CHAIN/RELEASE được chuyển tiếp nhưng không inject. Sau server ERR, tracking transaction bị xóa thận trọng; muốn inject lại cần BEGIN mới.
- Không inject autocommit/implicit commit. Không match SQL, sửa result, multi-statements/results, stored procedures, XA, compression, LOCAL INFILE, cursor/long-data chunks hoặc replication.
- Mỗi packet tối đa 1 MiB, tối đa 128 prepared statements/session, 4096 columns/parameters; idle connection có deadline. Không phải proxy production/general-purpose cho mọi workload.

Xem [contract](../../plans/09-semantic-adapters/mysql-contract.md) và [bằng chứng kiểm thử](../../plans/09-semantic-adapters/mysql-acceptance.md).

## 4. Chạy proxy trong Docker (tùy chọn)

Với MySQL demo ở mục 1:

```bash
rtk proxy docker network create faultline-mysql-net
rtk proxy docker network connect faultline-mysql-net faultline-mysql-demo
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:mysql .
rtk proxy docker run --rm --name faultline-mysql-proxy \
  --network faultline-mysql-net -p 127.0.0.1:13306:13306 \
  --mount "type=bind,src=$PWD/examples/mysql,dst=/config,readonly" \
  faultline:mysql serve --config /config/docker.yaml --admin-socket /tmp/admin/admin.sock
```

Dừng proxy local trước để tránh trùng port 13306. Enable/status bằng:

```bash
rtk proxy docker exec faultline-mysql-proxy /faultline enable --admin-socket /tmp/admin/admin.sock
rtk proxy docker exec faultline-mysql-proxy /faultline status --admin-socket /tmp/admin/admin.sock
```

Demo Go vẫn chạy trên host với hai DSN ở mục 1. Dọn container/network demo khi xong:

```bash
rtk proxy docker stop faultline-mysql-proxy
rtk proxy docker rm -f faultline-mysql-demo
rtk proxy docker network rm faultline-mysql-net
```

## 5. Automated verification

```bash
rtk proxy env FAULTLINE_MYSQL_TEST=1 go test -race ./tests/integration -run '^TestMySQL' -count=1 -timeout 240s
rtk proxy node --test internal/control/remote/app_test.cjs internal/control/remote/diff_test.cjs
```

Integration tạo database Docker riêng với port động, cleanup sau test; không dùng database của ứng dụng.
