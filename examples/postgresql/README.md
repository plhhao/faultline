# PostgreSQL — mất xác nhận COMMIT

Chỉ dùng database thử nghiệm. Fixture tạo hai bảng `faultline_retry_plain` và
`faultline_retry_unique`, xóa các row của lần chạy khi hoàn tất. Bảng được giữ lại.

## 1. Chạy dependency và proxy local

```bash
rtk proxy docker network create faultline-pg-demo
rtk proxy docker run --rm -d --name faultline-pg-demo --network faultline-pg-demo \
  -e POSTGRES_PASSWORD=fixture-secret \
  -e POSTGRES_HOST_AUTH_METHOD=scram-sha-256 \
  -p 127.0.0.1:5432:5432 postgres:17.11-bookworm
rtk proxy go build -o /tmp/faultline-pg ./cmd/faultline
rtk proxy mkdir -m 700 /tmp/faultline-pg-admin
rtk proxy /tmp/faultline-pg serve --config examples/postgresql/config.yaml \
  --admin-socket /tmp/faultline-pg-admin/admin.sock
```

Terminal khác, đợi PostgreSQL sẵn sàng (`docker logs faultline-pg-demo`), bật injection:

```bash
rtk proxy /tmp/faultline-pg enable --admin-socket /tmp/faultline-pg-admin/admin.sock
rtk proxy env \
  FAULTLINE_PG_DIRECT='postgres://postgres:fixture-secret@127.0.0.1:5432/postgres?sslmode=disable' \
  FAULTLINE_PG_PROXY='postgres://postgres:fixture-secret@127.0.0.1:15432/postgres?sslmode=disable' \
  go run ./examples/postgresql
```

Kỳ vọng: cả hai lần COMMIT báo `client_commit_success=false`, nhưng kiểm chứng trực tiếp:
- `deduplicate=false independently_verified_rows=2`: retry tạo trùng.
- `deduplicate=true independently_verified_rows=1`: unique operation ID chống trùng.

Tắt injection và chạy lại: COMMIT thành công; số row vẫn 2/1 vì demo cố ý gửi hai lần.
Proxy không tự retry và không kết luận rollback từ lỗi client.

## 2. Các fault và giới hạn

Phase duy nhất `after_commit`, matcher `{}`; select tính trên COMMIT tường minh đã
được database xác nhận. Không dùng HTTP method/path/header. Đổi action trong config rồi
`reload --config examples/postgresql/config.yaml --admin-socket /tmp/faultline-pg-admin/admin.sock`:

```yaml
fault: {phase: after_commit, action: delay, duration: 2s}
# hoặc
fault: {phase: after_commit, action: hold_response, max_duration: 5s}
# hoặc
fault: {phase: after_commit, action: close_connection}
```

Delay tiếp tục gửi ACK; hold hết hạn đóng session. Client timeout/cancel/shutdown cũng
kết thúc session. Disable không giải phóng hold đang chạy; reload áp dụng cycle kế tiếp.
Kết nối lỗi cần được loại khỏi pool và reconnect.

TLS listener dùng `tls: {cert_file: ..., key_file: ...}` và SSLRequest PostgreSQL.
Upstream TLS dùng `postgresqls://host:5432` và `upstream_tls: {ca_file: ...}`; kiểm tra CA
và hostname, không fallback plaintext. Client dùng `sslmode=verify-full` và CA phù hợp.
SCRAM-SHA-256-PLUS không hỗ trợ: client khác pgx fixture phải chủ động chọn
`channel_binding=disable`; proxy không sửa danh sách mechanism để hạ bảo mật.

Giới hạn: PostgreSQL 17 wire 3.0, pgx v5.7.6; không COPY, pipeline, replication,
mTLS, SQL matching hay chỉnh result. Frame tối đa 1 MiB, startup 10 KiB.
`request_timeout` giới hạn startup, idle và từng command cycle;
`max_inflight_requests` giới hạn số session PostgreSQL.
Xem [contract](../../plans/09-semantic-adapters/contract.md).

## 3. Test tự động

```bash
rtk proxy env FAULTLINE_POSTGRES_TEST=1 go test ./tests/integration \
  -run '^TestPostgreSQLReal$' -timeout 180s -v
```

Test tự tạo/xóa container với port động, cert tạm và SCRAM; cần Docker daemon và
image đã tải. Test thường không bật biến trên sẽ ghi SKIP cho fixture Docker.

## 4. Test UI (người dùng thực hiện)

Dùng managed config như [tester setup](../tester/README.md), thêm proxy PostgreSQL
trước khi `configure` offline và khởi động server. Proxy/listener/TLS là cấu hình cần restart.

- [ ] Chọn PostgreSQL: không có HTTP method/path/headers; chỉ 3 action và `after_commit`.
- [ ] Đổi delay/hold/disconnect, validate, xem diff, apply; chạy demo và đối chiếu kết quả.
- [ ] Enable/disable và kiểm tra counters: chỉ confirmed COMMIT mới eligible/selected.
- [ ] Hold đang chạy vẫn chờ sau Disable; lần COMMIT sau không bị fault.
- [ ] HTTP/gRPC proxy trong cùng config vẫn có lựa chọn riêng như trước.

### Kết quả và kiểm tra giao diện nhiều protocol

Người dùng xác nhận PG1–PG6 **PASS ngày 2026-09-17**: form/baseline,
close_connection, delay/diff, hold/disable, validation/probability và restart.
Còn kiểm tra chuyển qua lại PostgreSQL/HTTP/gRPC trong cùng instance.

Config [ui.yaml](ui.yaml) có `database` (15432), `payment` (18080) và `echo`
(18081). Hai port HTTP/gRPC riêng tránh trùng listener của fixture Phase 7;
upstream vẫn là 9000 và 9001. Probability HTTP/gRPC ban đầu bằng 0.
Với môi trường mới, copy file này làm config trước khi start lần đầu.
Với managed instance đã chạy, dừng server rồi dùng `configure` với config thay thế;
để giữ các rule đã apply, lấy config active trong `data/state.json` làm nền và
chỉ bổ sung hai proxy. Không dùng thẳng `ui.yaml` nếu muốn giữ rule PostgreSQL đã sửa.

Chạy các dependency ở hai terminal (bỏ qua nếu đã chạy):

```bash
rtk proxy go run ./examples/http/paymentdemo/cmd serve --listen 127.0.0.1:9000
rtk proxy go run ./examples/grpc/unary/cmd -mode server -address 127.0.0.1:9001
```

- [ ] Sau khi dừng server, xóa/thêm proxy bằng `configure` rồi restart và đăng nhập lại:
  draft chưa sửa phải tải danh sách proxy mới mà không cần reload trang. Nếu có draft
  chưa lưu, UI giữ draft và thông báo; dùng Discard draft and load active để lấy danh sách mới.
- [ ] Chuyển `database → payment → echo → database`: form đúng protocol,
  không còn field/action của proxy trước; PostgreSQL chỉ có `after_commit`.
- [ ] `payment`: đổi probability của `payment-fault` thành 1, validate/review/apply,
  enable rồi gọi `rtk proxy curl -i http://127.0.0.1:18080/healthz`: nhận 503.
  Disable rồi gọi lại nhận 200.
- [ ] `echo`: đổi probability thành 1, giữ delay 2000 ms, validate/review/apply,
  enable rồi chạy lệnh dưới: lỗi deadline. Disable và chạy lại: thành công.
- [ ] Sửa draft ở một proxy rồi chuyển sang proxy khác và quay lại: draft giữ nguyên;
  diff chỉ highlight thay đổi đã nhập, các rule PostgreSQL không bị đổi ngoài ý muốn.

```bash
rtk proxy go run ./examples/grpc/unary/cmd -mode client \
  -address 127.0.0.1:18081 -timeout 500ms -size 1024
```

## 5. Chạy chính proxy bằng Docker (tùy chọn)

Dừng proxy local trước để giải phóng port 15432; giữ dependency và network ở bước 1.
Chạy từ root repository:

```bash
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:phase9 .
rtk proxy docker run --rm -d --name faultline-pg-proxy \
  --network faultline-pg-demo -p 127.0.0.1:15432:15432 \
  -v "$PWD/examples/postgresql:/fixture:ro" faultline:phase9 \
  serve --config /fixture/docker.yaml
rtk proxy docker exec faultline-pg-proxy /faultline enable
```

Chạy lại demo với hai DSN ở bước 1. Dependency vẫn publish port 5432 để driver có
kết nối kiểm chứng trực tiếp. Image dùng user không phải root và default admin socket
trong thư mục riêng của user; không mount admin socket từ host.

## 6. Dọn fixture

Dừng proxy bằng Ctrl-C, sau đó:

```bash
rtk proxy docker stop faultline-pg-proxy  # chỉ khi đã chạy proxy Docker
rtk proxy docker stop faultline-pg-demo
rtk proxy docker network rm faultline-pg-demo
```
