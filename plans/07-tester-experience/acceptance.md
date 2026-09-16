# Phase 7 — Bằng chứng kiểm chứng

Ngày cập nhật: **2026-09-16**. **P19, P20, P20a và Phase 7 Done**. Người dùng xác nhận mọi case trong hướng dẫn tester PASS; kiểm thử browser do người dùng thực hiện.

## Kết quả tự động

Môi trường: macOS arm64, Go 1.26.4; Docker/OrbStack Linux arm64. Go cache dùng `/private/tmp/faultline-go-cache`.

| Kiểm tra | Lệnh / bằng chứng | Kết quả |
| --- | --- | --- |
| Regression toàn bộ | `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go test ./... -timeout 120s` | Pass; opt-in Docker không chạy trong lệnh này |
| Race toàn bộ | `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go test -race ./... -timeout 180s` | Pass; gồm remote API, commit crash tests và binary smoke |
| Targeted final race checks | `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go test -race ./internal/control/remote ./tests/integration -run 'Test(Apply|Validation|Storage|Expiry|Managed|RuleCapabilities|CrashCommit|PasswordReset)' -timeout 180s` | Pass after final review and password-reset coverage |
| Static Go checks | `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go vet ./...` | Pass |
| CLI build | `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go build -o /private/tmp/faultline-phase7-review ./cmd/faultline` | Pass |
| Binary thật | `TestManagedDelivery/binary` trong [managed_test.go](../../tests/integration/managed_test.go) | HTTPS login, validate/apply/conflict, HTTP 503, chặn Unix write, restart giữ revision/config và tắt injection: pass |
| Docker thật | `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestManagedDelivery/docker$' -v -timeout 360s` | Pass; kill container sau apply rồi khởi động container mới cùng data volume, config/revision còn nguyên, session mất, injection disabled |
| JavaScript | `rtk proxy node --check internal/control/remote/ui/app.js` | Pass; đây không phải kiểm thử tương tác browser |
| Setup hướng dẫn | Chạy `examples/tester/setup.py` với fixture tạm và input password test qua mock getpass, sau đó chạy binary `validate` | Pass: build, OpenSSL certificate, hai user, YAML HTTP/gRPC hợp lệ, key mode 0600; fixture tạm đã xóa |

Test đầu tiên trong sandbox bị chặn bind TCP/Unix; chạy lại với quyền localhost và pass. Docker smoke ban đầu lỗi quyền `/tmp` khi dùng UID của bind mount, rồi gặp socket còn lại sau kill nếu socket đặt trên volume; fixture và hướng dẫn đã chuyển runtime socket sang tmpfs `/tmp`, test cuối pass. Không sửa chính sách không ghi đè Unix socket của file mode.

Documentation checks: 115 local links/anchors, static HTML/JavaScript DOM IDs, all 13 AC mappings and `git diff --check` passed.

## Đối chiếu P19

| AC | Bằng chứng | Trạng thái |
| --- | --- | --- |
| P19-AC1 | Full file-mode regression; `TestApplyConflictPersistenceAndNoOp`, `TestManagedDelivery`, `TestManagedExclusiveLockAndCorruptState`: bootstrap chỉ một lần, file hỏng không ghi đè managed state, không bypass local admin | Pass |
| P19-AC2 | `TestValidationPermissionsAndSessionRevocation`, `TestRuleCapabilitiesAndGRPCValidation`: config lỗi giữ revision, chặn field hạ tầng, capability HTTP2/gRPC qua validation chung | Pass |
| P19-AC3 | `TestApplyConflictPersistenceAndNoOp`: hai apply cùng base, một 200/một 409; HTTP delivery trả conflict | Pass |
| P19-AC4 | `TestCrashCommitBoundary` dừng process trước/sau durable commit nhưng trước publish; `TestStorageFailureLeavesRuntimeAndAuditBounded`; Docker kill/recover | Pass |
| P19-AC5 | Login đúng/sai, viewer/editor, CSRF/origin, logout/expiry/rate limit, account deletion và `TestPasswordResetRevokesSessions` | Pass |
| P19-AC6 | Durable actor/revision/outcome audit; limit 1.000; storage lỗi chặn apply/toggle; không log password/session; apply failure có recorder event | Pass |
| P19-AC7 | Managed old-snapshot/no-op assertions, full control/admin/fault lifecycle regression; status dùng snapshot/counters hiện có | Pass |
| P19-AC8 | HTTPS asset/API delivery từ binary và Docker thật; restart/session/disabled assertions; setup script chạy thành công; [hướng dẫn vận hành](../../examples/tester/README.md) | Pass |

Code tests chính: [remote/server_test.go](../../internal/control/remote/server_test.go), [managed delivery](../../tests/integration/managed_test.go), [outcome counters](../../internal/recorder/outcomes_test.go). Counters outcome độc lập với queue và trả bản copy, không có lịch sử traffic.

## P20 — người dùng kiểm thử

Ngày 2026-09-16, người dùng xác nhận **U1–U9 PASS, hoạt động ổn định**. Sau đó người dùng xác nhận mọi case trong hướng dẫn PASS, bao gồm vận hành/Docker và checklist bổ sung path pattern/diff.

| AC | Checklist trong hướng dẫn | Trạng thái |
| --- | --- | --- |
| P20-AC1 | U2 HTTP và U6 gRPC: sửa → validate → diff → apply → revision/traffic | Pass — user |
| P20-AC2 | U3 validation/draft và U4 conflict hai phiên | Pass — user |
| P20-AC3 | U1 quyền/login/logout và U8 revocation | Pass — user |
| P20-AC4 | U5 hold/disable, U7 stale status, U2 outcomes | Pass — user |
| P20-AC5 | U3 capabilities, U7 restart/session, U9 hiển thị; Docker UI tùy chọn | Pass — user, gồm Docker UI |

Bắt đầu từ [examples/tester/README.md](../../examples/tester/README.md); có script setup, lệnh chạy và mẫu trả kết quả.

## Review và giới hạn

- Dùng cùng config validation và control service, không có engine frontend. Form lấy danh sách action từ capability chung phía Go.
- Review đã sửa: nội dung JSON header lỗi bị mất khi đổi proxy; draft thay đổi khi validate còn đang chạy; chỉnh tiếp trong lúc apply làm mất draft mới. Apply khóa tương tác trong thời gian gửi, validation phải khớp đúng draft đã gửi.
- Config và audit commit cùng file, ghi trước publish; audit ring 1.000 là lịch sử quản trị hữu hạn, không phải hệ thống audit bên ngoài chống sửa bởi operator. Operator có quyền file hệ điều hành là trust boundary.
- Audit lỗi trước commit chặn thao tác. Lỗi directory fsync sau rename đặt trạng thái commit uncertain và yêu cầu process dừng/phục hồi. Crash tests mô phỏng process death trước/sau commit, không mô phỏng mất điện/đĩa hỏng vật lý.
- Storage private, một process sở hữu mỗi directory; không có HA hoặc nhiều writer. Mỗi apply/audit ghi lại state file, phù hợp server test, không có cam kết throughput control plane.
- Draft chỉ ở bộ nhớ tab, không tồn tại qua reload/đóng tab. Diff là hai bản rules cạnh nhau; khi conflict, người dùng chủ động quyết định dùng toàn draft trên base mới, không tự merge.
- Session 8 giờ, tối đa 256; giới hạn login 20/phút toàn instance. Account/password/role do operator quản lý qua local CLI. HTTPS trực tiếp với certificate do operator cấp.
- JSON API nhận tối đa 1 MiB cho login/validate/apply. Fields ngoài scope bị từ chối; rule fields dùng JSON tên Go (ví dụ `ID`, `Select.Probability`, `Fault.Duration`), duration là nanoseconds; UI hiển thị milliseconds. Không có REST API ổn định cho tích hợp bên ngoài được cam kết thêm trong phase này.
- Sau binary SIGKILL có thể cần operator xóa socket stale khi đã xác nhận process dừng; Docker fixture dùng tmpfs cho socket và volume riêng cho state. TLS đường dẫn đã chuẩn hóa cần tồn tại trong môi trường phục hồi.
- Agent không chạy browser automation; bằng chứng tương tác/hiển thị là kết quả PASS người dùng báo.

## P20a — Path pattern (2026-09-15)

- Backend/unit/integration PASS: `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go test -race ./... -timeout 180s`. Bao phủ config round-trip/validation, matching/ownership, HTTP/1–HTTP/2 forwarding raw query và decoded path, file reload/no-op, API conflict/persistence/restart, gRPC selector/reload và regression snapshot.
- PASS: `go vet ./...`, build CLI `/private/tmp/faultline-path-pattern`, validate `examples/tester/config.yaml`, `node --check internal/control/remote/ui/app.js`.
- Sandbox chặn TCP binding ở lần test đầu; chạy lại có quyền localhost và PASS. Docker opt-in không chạy lại; UI interaction chưa chạy.
- AC1–AC5 có kiểm chứng tự động như [plan P20a](03-path-pattern.md); AC6 PASS do người dùng xác nhận ngày 2026-09-16 theo [hướng dẫn tester](../../examples/tester/README.md#p20a--kiểm-thử-path-pattern). P20a Done.

## Bổ sung regression gRPC hold_response / client deadline (2026-09-15)

- `TestGRPCHoldResponseClientDeadline` kiểm tra plaintext, TLS và mTLS trên cả hai chặng. `max_duration: 10s`, client deadline 1s, proxy timeout 15s; upstream đã xử lý và executor đã vào hold trước deadline. Client nhận `DeadlineExceeded`, report xác nhận applied/canceled; RPC tiếp theo thành công với giới hạn một request inflight.
- PASS: `rtk proxy env GOCACHE=/private/tmp/faultline-go-cache go test -race ./tests/integration -run '^TestGRPCHoldResponseClientDeadline$' -count=3 -v -timeout 90s` (9 subcases); `go vet ./tests/integration` và whitespace check PASS. Không đổi production code, không chạy Docker trong lần bổ sung này.
- Đã REVIEW test và cleanup; bổ sung này hoàn tất. Trạng thái nghiệm thu UI phase 7 vẫn pending người dùng.

## Phản hồi nghiệm thu U1–U4

- Người dùng báo **U1, U2, U3, U4 PASS**, kèm yêu cầu tối ưu UI; không suy ra các case còn lại đã pass.
- Đã bổ sung badge injection sát nút Enable/Disable trong Runtime, pending/confirmed state cho toggle; chặn phản hồi status cũ ghi đè trong lúc toggle. Theo phản hồi tiếp theo, bỏ header cố định; thông báo nổi xác nhận tự ẩn sau 5 giây, lỗi giữ tới khi đóng. Phase giới hạn theo action/direction; giải thích method optional và observation retention trên UI.
- JavaScript syntax và kiểm tra lựa chọn phase cho 7 action × 2 direction PASS; asset/session test PASS. Chưa kiểm thử browser các thay đổi mới; xem [checklist kiểm tra lại](../../examples/tester/README.md#kiểm-tra-lại-ui-sau-phản-hồi-u1u4). P20 giữ In progress.

## Kết luận nghiệm thu — 2026-09-16

Người dùng xác nhận **mọi case** trong hướng dẫn tester PASS, bao gồm vận hành/configure, Docker, P20a và diff. P20-AC1–AC5 và P20a-AC6 đã có bằng chứng thủ công; kết hợp kiểm chứng tự động P19-AC1–AC8 và P20a-AC1–AC5, Phase 7 **Done**. Các ghi chú chờ kiểm thử trong lịch sử phía trên được thay thế bởi xác nhận này. Không chạy lại runtime tests trong lần cập nhật tài liệu.
