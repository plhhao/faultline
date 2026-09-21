# Phase 9 — kết quả kiểm chứng

Ngày: 2026-09-16–17. Môi trường: macOS arm64, Docker/OrbStack Linux arm64.
PostgreSQL `17.11-bookworm`, pgx `v5.7.6`; [contract](contract.md).

## Trạng thái

- P23-AC1–AC4: **PASS** — version/matrix, wire traces, lifecycle/resource bounds,
  schema và thiết kế bằng chứng độc lập đã chốt.
- P24-AC1–AC7: **PASS automated** theo các test dưới đây.
- P24-AC8: automated regression/build/assets **PASS**, **manual PostgreSQL UI PASS; mixed-protocol UI pending**.
- Phase 9 / P24 chưa Done trước khi người dùng xác nhận UI.

## Mapping

| AC | Bằng chứng |
| --- | --- |
| AC1 | `TestPostgreSQLReal/baseline`: 4 tổ hợp TLS listener/upstream, simple query và prepared/extended query, SCRAM, BEGIN/INSERT/COMMIT/ROLLBACK; pool recovery |
| AC2 | `fault`: delay/hold_response/close_connection, plaintext và TLS; `TestPostgreSQLCapabilities`: từ chối phase, matcher, selector, upstream và parameters sai; probability zero forwarding |
| AC3 | `fault`: delay đủ thời gian rồi thành công, hold/disconnect gây lỗi; kết nối độc lập đọc được row đã commit; `extended-commit` kiểm tra extended protocol |
| AC4 | `errors-not-commit`: lỗi trước COMMIT, failed transaction được rollback, deferred unique constraint lỗi tại COMMIT, ROLLBACK không tăng applied |
| AC5 | `reload-disable-probability`: session dài hạn nhận revision mới, no-op, nth và disable; `TestCommitBoundaryAndSnapshot`: snapshot đã capture vẫn enabled sau Disable; `cancel-held-commit`: Disable không giải phóng hold |
| AC6 | `client-timeout`, `runtime-timeout`, `shutdown`, `cancel-held-commit`: bounded cleanup, active counters về 0; `TestFramingAndStartupBounds`, `TestPumpCancellation`, `TestRejectChannelBindingWithoutDowngrade`, unsupported COPY; SQL marker/fixture password không có trong recorder |
| AC7 | `retry-data-evidence` và `binary-retry-demo`: 2 attempts tạo 2 row thường / 1 row unique; client cả hai báo lỗi, verification qua direct connection; `pool-recovery` reconnect sau lỗi |
| AC8 | Toàn bộ Go tests/race regression HTTP/HTTP2/gRPC/control; vet, binary và Docker build; 8 Node tests editor/diff; `TestPostgreSQLManagedAPI` kiểm tra capability/validate/apply/enable và từ chối HTTP matcher. Người dùng xác nhận PG1–PG6 PASS ngày 2026-09-17; chuyển proxy HTTP/gRPC/PostgreSQL còn chờ |

## Lệnh đã chạy

```bash
rtk proxy go test ./... -timeout 180s
rtk proxy go test -race ./... -timeout 180s
rtk proxy env FAULTLINE_POSTGRES_TEST=1 go test -race ./tests/integration \
  -run '^TestPostgreSQLReal$' -timeout 180s
rtk proxy go vet ./...
rtk proxy go build -o /tmp/faultline-phase9 ./cmd/faultline
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:phase9 .
rtk proxy node --test internal/control/remote/app_test.cjs internal/control/remote/diff_test.cjs
```

Sau các sửa review, đã chạy lại các test PostgreSQL/config bị ảnh hưởng với race,
fixture thật, binary demo, vet và build. Một lần fixture binary lỗi do admin socket
nằm trực tiếp dưới `/tmp`; đã sửa sang thư mục tạm 0700 và rerun PASS.
Docker fixture dùng port động, cert tạm và tự dọn container. Không sửa database thật.

## Giới hạn của bằng chứng

- Go fixture không đại diện mọi ORM hoặc PostgreSQL version.
- Docker image Faultline đã build; hướng dẫn Docker proxy là kịch bản thủ công riêng.
- Tests không bật `FAULTLINE_POSTGRES_TEST=1` sẽ skip fixture PostgreSQL thật.
- Node tests kiểm tra logic editor, không thay cho tương tác/layout trong browser.
- Checklist UI: [examples/postgresql/README.md](../../examples/postgresql/README.md).
