# Phase 06 — Bằng chứng kiểm chứng

Ngày chạy: 2026-09-14. Go 1.26.4, macOS arm64; Docker Linux arm64 (aarch64).
Certificate/key là fixture tạm, không commit private key. Các AC bổ sung cho
AC1–AC24 của MVP. Streaming gRPC chưa nằm trong phạm vi nghiệm thu.

## Lệnh và kết quả

| Lệnh | Kết quả |
| --- | --- |
| `rtk proxy go test ./... -count=1 -timeout=180s` | PASS; integration 30.770s, gồm binary và regression MVP; Docker opt-in chạy riêng |
| `rtk proxy go test -race ./... -count=1 -timeout=240s` | PASS; integration 35.572s (lần cuối sau sửa recorder) |
| `rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^(TestContainerRuntime|TestExtensionDocker)$' -count=1 -v -timeout=360s` | PASS: MVP Docker/payment demo và phase 6, 28.421s |
| `rtk proxy go vet ./...` | PASS |
| `rtk proxy go build -o bin/faultline ./cmd/faultline` | PASS |
| CLI `validate` với `examples/http/http2.yaml`, `examples/http/body-faults.yaml`, `examples/grpc/faultline.yaml` | PASS |
| `rtk proxy go test ./tests/integration -run '^TestExtensionBinary$' -count=1 -v -timeout=90s` | PASS: gRPC mTLS hai phía, startup disabled, enable/disable, reload, bốn action/direction |
| `rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestExtensionDocker$' -count=1 -v -timeout=360s` | PASS: cùng matrix qua Docker và file mount, 9.640s |

Test TCP/Unix socket cần chạy ngoài sandbox chặn bind localhost. Lỗi bind ban đầu
đã được giải quyết bằng quyền chạy test. Lần Docker đầu tiên phát hiện admin ready
trước cổng publish; fixture đã chờ RPC thành công trước inject, lần chạy lại pass.
Lần chạy gate chung tiếp theo gặp OrbStack đã dừng (socket không tồn tại);
daemon đã được khôi phục và gate chung chạy lại PASS (28.421s).

## Đối chiếu 15 AC

| AC | Bằng chứng chính |
| --- | --- |
| H2-1 | `TestExtensionTLSMatrix` (HTTP/1 và HTTP/2 × 3 chế độ mỗi phía); `TestIndependentHTTPVersions`; `TestHTTP2RejectsTLSFallback` |
| H2-2 | `TestHTTP2StreamIsolationAndSnapshots`, `TestHTTP2BuiltinCapabilities`; matrix body kiểm tra reuse connection sau lỗi |
| H2-3 | Snapshot cũ/mới cùng connection; `TestHTTP2CancellationAndHold`; raw REFUSED_STREAM trong `TestHTTP2NoRetryOnRefusedStream` chỉ có một connection/một attempt; `TestHTTP2DialFailureDoesNotDrainUpload` |
| MT-1 | `TestMTLSAuthenticationFailures` listener: thiếu cert/sai CA/hết hạn, cả HTTP/1 và HTTP/2; TLS matrix có chế độ không yêu cầu cert |
| MT-2 | Cùng test cho upstream; matrix mTLS một/hai phía; regression `TestUpstreamTLSFailuresAreNatural` và SNI |
| MT-3 | `TestMTLSIncludesAndRestart`, `TestProtocolAndBodyValidation`, TLS/config/control regression: resolve include, clone, cặp cert/key, CA, không TLS, fingerprint và atomic rejection |
| MT-4 | Authentication không tới application/không applied; `TestExtensionBinary`, `TestExtensionDocker` dùng file mount thật và kiểm tra log không chứa private key |
| GR-1 | `TestGRPCUnaryMatrix`: gRPC client/server thực, payload 3 byte/1 MiB, gzip, repeated/binary metadata, header/trailer, OK/FailedPrecondition, 9 tổ hợp TLS/mTLS |
| GR-2 | `TestGRPCSelectorsAndReload`: service/method, first-match, probability 0/1, nth/every, disabled không tăng sequence; `TestGRPCBodyFaultMatrix` kiểm tra metadata matcher |
| GR-3 | `TestGRPCConcurrentSnapshotAndDeadline`: RPC đang chạy và RPC mới dùng snapshot riêng; no-op/reset counters; body matrix kiểm đếm upstream attempt |
| GR-4 | `TestGRPCActiveCancellation`: deadline, client cancel, shutdown đều dừng upstream; report not_reached khi chưa có headers; natural RPC status và validation capability |
| FT-1 | `TestTruncateHTTP1FramingBoundaries`: request/response × Content-Length/chunked × N=0/2/4/9; `TestTruncateBoundaries`, `TestTruncateNaturalError`, `TestBodylessResponsesAreNotTruncated` |
| FT-2 | `TestBodyFaultMatrix`, `TestGRPCBodyFaultMatrix`: hai chiều, plain/TLS/mTLS, cắt giữa prefix message, connection tiếp tục dùng được, không RPC thành công giả |
| FT-3 | Cùng matrix đo 4096 byte với 16384 B/s; `TestThrottlePacingAndCancellation` dùng virtual clock kiểm tra chunk/burst và hai flow có ngân sách riêng; binary/Docker disabled pass-through |
| FT-4 | `TestHTTP2CancellationAndHold`, `TestHTTP2BodyFaultResourceBound`; snapshot/counters tests, wrapper natural error và race suite; semantics body giống nhau qua TLS/mTLS |

Mã test: [HTTP/2/mTLS/body](../../tests/integration/protocol_extensions_test.go),
[gRPC](../../tests/integration/grpc_test.go),
[binary/Docker](../../tests/integration/extension_delivery_test.go),
[body unit tests](../../internal/fault/body_test.go),
[config extensions](../../internal/config/extensions_test.go).

## Matrix và số đo

- Pass-through HTTP/1 và HTTP/2: 18 tổ hợp protocol × listener plain/TLS/mTLS × upstream plain/TLS/mTLS; kiểm tra version thực tại hai phía. Test version độc lập bổ sung HTTP/1 → HTTP/2 và HTTP/2 → HTTP/1.
- Pass-through gRPC: 9 tổ hợp TLS hai phía, với payload nhỏ/lớn và status lỗi.
- Body fault HTTP: 24 tổ hợp 2 protocol × 2 action × 2 direction × 3 chế độ plain/TLS/mTLS cả hai phía. Body fault gRPC: 12 tổ hợp tương ứng. Các tổ hợp TLS bất đối xứng kiểm tra ở pass-through; không chạy tích Descartes của mọi selector/fault/certificate.
- Fixture timing HTTP/gRPC: 4096 byte, 16384 B/s, tối thiểu 230ms, tối đa 1250ms đã chọn trước test. Binary: request 257ms, response 259ms; Docker: request 265ms, response 258ms (gRPC framing cộng vài byte). Docker smoke cho phép tối đa 1500ms vì VM.
- Tám flow HTTP/2, mỗi body 512 MiB ảo, throttle 100 B/s trong 250ms: heap growth request khoảng 1.87 MB, response 34.64 MB; cleanup sau cancel khoảng 0.25ms. Ngưỡng kiểm tra 128 MiB và 2s. Số đo bao gồm cả client/upstream fixture cùng process, không phải heap riêng của proxy hay capacity/SLA. HTTP/2 flow-control buffers vẫn tồn tại; proxy không đọc toàn bộ 4 GiB body vào RAM.

## REVIEW

- Đã sửa header `Connection: close` trong đường lỗi HTTP/2; test reuse/sibling stream pass. Không đổi nghĩa close_connection thành reset stream.
- Mỗi flow HTTP/2 dùng `ClientConn.RoundTrip` trực tiếp; raw REFUSED_STREAM không kích hoạt vòng retry. Không dùng pool chung upstream trong phase này; chi phí TCP/TLS cao hơn là trade-off đã ghi rõ.
- Callback upload dùng atomic applied flag và immutable event tạo tại phase reached để giữ upstream status ở after-headers; `TestBodyFaultAppliedEventRetainsHeaderStatus` kiểm tra event ở hai chiều. Wrapper close joins active reads. Race suite không phát hiện data race.
- TLS/mTLS giữ server hostname/CA verification, client CA riêng, paths/content thuộc restart fingerprint. Không chuyển danh tính certificate gốc của app.
- Config/schema, capability matrix và ví dụ thống nhất; shared engine chỉ thêm service metadata, không nhập protocol I/O. gRPC fixture dependency không cần cho ứng dụng được proxy.
- Truncate vẫn giữ Content-Length/framing bất thường. Close-delimited HTTP/1 và gRPC streaming có giới hạn được ghi trong [hướng dẫn](../../examples/grpc/README.md).
