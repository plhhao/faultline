# P25 — MySQL acceptance

Ngày kiểm chứng: **2026-09-17**. Trạng thái: **PASS trong contract đã chốt**.

## Phiên bản và phạm vi

- Go 1.26.4; MySQL 8.4.8 Docker, manifest digest và driver v1.10.1 tại [contract](mysql-contract.md).
- Go fixture, InnoDB, explicit transaction, command-cycle snapshots. Không claim mọi ORM/driver hoặc MariaDB.
- TLS matrix: plaintext/plaintext và TLS/TLS; mixed TLS/plaintext bị validate từ chối. Không mTLS.
- UI dùng lại form PostgreSQL; người dùng đồng ý automated JS/API regression thay manual UI khi không có interaction mới.

## Kết quả

| AC | Kết quả | Bằng chứng |
| --- | --- | --- |
| AC1 | PASS | Trace trực tiếp cold RSA/full và warm fast auth trước code; pinned image/driver, auth/TLS/command/resource contract. Unit auth-switch caching_sha2_password, từ chối native-password và malformed greeting. |
| AC2 | PASS | MySQL thật: cold/warm auth cả plaintext và TLS, ping/query, prepared SELECT/INSERT, BEGIN/COMMIT/ROLLBACK, reconnect pool. TLS sai CA và hostname bị từ chối. |
| AC3 | PASS | Cả ba action ở plaintext/TLS: delay >=140ms cho cấu hình150ms; hold/disconnect trả lỗi; direct connection thấy row đã commit. |
| AC4 | PASS | Thật: rollback, autocommit, DDL implicit commit, OK không phải COMMIT, comment/data có COMMIT, statement error. Synthetic wire test: COMMIT ERR được forward, không chọn fault; không claim đã tạo deferred constraint failure trên MySQL. |
| AC5 | PASS | Thật: probability0/1, nth/every, first rule, disabled rule, no-op/reload trên pool, disable không thả hold. Synthetic delayed backend: COMMIT giữ snapshot cũ khi reload giữa cycle; cycle tiếp theo dùng revision mới. |
| AC6 | PASS | Client cancellation, runtime timeout, shutdown, active/active_faults về0; session limit và128 prepared statements thật; unit packet bounds, sequence, unsupported/malformed commands. Binary logs không lộ fixture credential. |
| AC7 | PASS | Actual binary validate/serve/status/enable/disable/reload, Go retry demo4 commit errors và independently verified rows2/1; managed API auth + capabilities/validate/apply/enable và rejection matcher HTTP. |
| AC8 | PASS | Full Go race suite gồm real MySQL/PostgreSQL và HTTP/gRPC; vet, binary build, Docker build. MySQL proxy Docker runtime demo2/1 và counters cũng PASS. |
| AC9 | PASS | 12 Node tests cho editor/diff: MySQL capabilities, đổi MySQL/PostgreSQL/HTTP/gRPC giữ draft, phase/field đúng; managed API mutation/validation tests PASS. Không có manual browser mới. |

## Lệnh đã chạy

```bash
rtk proxy env FAULTLINE_MYSQL_TEST=1 go test -race ./tests/integration -run '^TestMySQL' -count=1 -v -timeout 240s
rtk proxy env FAULTLINE_MYSQL_TEST=1 FAULTLINE_POSTGRES_TEST=1 go test -race ./... -timeout 300s
rtk proxy go test -race ./internal/proxy/mysql
rtk proxy go vet ./...
rtk proxy go build -o /tmp/faultline-mysql-p25-bin ./cmd/faultline
rtk proxy node --test internal/control/remote/app_test.cjs internal/control/remote/diff_test.cjs
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:mysql-p25 .
rtk proxy /tmp/faultline-mysql-p25-bin validate --config examples/mysql/ui.yaml
rtk proxy git diff --check
```

Full race suite: tất cả package PASS, integration59.355s. Auth-switch, fragmented read và legacy/modern result-boundary tests bổ sung sau full suite được chạy riêng bằng race detector và PASS.

Docker runtime: dùng image `faultline:mysql-p25`, network tạm và fixture MySQL riêng, hai port loopback động; chạy `examples/mysql` từ host. Cả4 commit phía client lỗi, bảng thường2 row/unique1 row. Container `status`: eligible=4, selected=4, applied=4, active=0, active_fault_flows=0, dropped_events=0, write_errors=0. Đây là plaintext runtime smoke; TLS đã kiểm chứng với proxy Go local và MySQL Docker, không claim TLS proxy-container riêng.

## Giới hạn / lỗi đã xử lý

- Lần đầu TLS fixture thất bại vì certificate root-owned mode0600, MySQL không đọc được; sửa cert thành0644, key vẫn0600 thuộc mysql. Direct TLS và toàn matrix sau đó PASS.
- Một lượt test sandbox không bind được localhost; chạy lại với quyền local listener/Docker và PASS.
- COMMIT có comment/AND CHAIN/RELEASE được chuyển tiếp nhưng không inject. Server ERR xóa tracking transaction thận trọng; cần BEGIN mới để inject tiếp.
- Giới hạn packet1MiB, statement128, metadata4096 và idle/runtime timeout là phạm vi sản phẩm, không phải benchmark capacity.
- Không yêu cầu Phase8, Node/TypeORM fixture, MariaDB, XA, replication, arbitrary SQL matching hoặc sửa kết quả.

Hướng dẫn tái hiện: [examples/mysql](../../examples/mysql/README.md).
