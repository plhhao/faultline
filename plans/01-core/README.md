# Phase 01 — Core và cấu hình

Phạm vi: **MVP**. Trạng thái: **Done** — gồm phần bổ sung cấu hình nhiều file.

Schema một/nhiều file, snapshot và quyết định fault đã hiện thực và kiểm thử độc lập với HTTP I/O. P01–P03 và P01a đều Done; CLI và HTTP adapter tiếp tục ở phase 02.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P01](01-config-schema.md) | Schema và validation YAML | Scaffold |
| [P02](02-runtime-snapshots.md) | Snapshot và trạng thái injection | P01 |
| [P03](03-rule-engine.md) | Matcher, selector và sequence | P01, P02 |
| [P01a](04-multi-file-config.md) | File gốc và include proxies | P01, P02, P03 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Quyết định hiện thực

Các quyết định và kết quả ban đầu dưới đây ghi nhận P01–P03 một file. Phần multi-file cùng bằng chứng kiểm chứng bổ sung nằm ở [P01a](04-multi-file-config.md) và mục 7.1 đặc tả. `Load`/`ReloadFile` xử lý file gốc và includes; `Parse`/`Reload` bytes giữ semantics standalone.

- `internal/config`: `Load`/`Parse` trả `Document` đã validate; `Config()` trả deep copy. Dùng YAML v3.0.5, strict field/type validation và chẩn đoán không echo scalar. Xem [defaults và schema](../../examples/http/README.md).
- `internal/control`: `New` khởi động disabled, `Acquire` lấy snapshot nhất quán, `SetEnabled` đổi công tắc, `Apply` nhận Document hợp lệ, `Reload` parse lại dữ liệu trước apply. `Info` có run ID, revision, control sequence và timestamps; readiness/active fault status sẽ thêm khi có adapter.
- Mỗi revision sở hữu một engine và counters. Toggle dùng lại engine; config thực sự đổi tạo engine mới. Control chỉ giữ revision hiện hành, flow giữ reference tới revision cũ. Caller phải bỏ snapshot khi flow kết thúc; không có API release/refcount thủ công.
- Document được giữ theo giá trị trong revision để caller thay biến Document không sửa được snapshot. Config maps/slices/pointers và Fault trong decision đều được sao chép khi trả ra.
- Effective-config hash dựa trên dữ liệu chuẩn hóa và digest TLS files. Proxy order không ảnh hưởng hash; rule order có ảnh hưởng. Đổi file TLS tại cùng path vẫn cần restart.
- `internal/engine`: `Decide` nhận metadata từ adapter, trả rule/eligible sequence/selected/fault. Header nhiều giá trị match nếu có một giá trị chính xác; không join hoặc đổi value.
- PCG từ Go `math/rand/v2`, seed dẫn xuất bằng SHA-256 từ config seed + proxy ID + rule ID. Sequence/random/counters được bảo vệ bằng mutex riêng mỗi rule; run/revision ID không ảnh hưởng chuỗi random. Cam kết replay theo cùng config và thứ tự eligible, không theo logical operation dưới concurrency.
- Phase 1 chỉ đếm eligible/selected. Applied/not_reached cần lifecycle hooks/fault executor; không tăng applied giả khi chỉ chọn action.

## Bằng chứng kiểm chứng — 2026-09-12

| Kiểm tra | Kết quả |
| --- | --- |
| `go build ./...` | Pass: ba package core |
| `go test ./...` | Pass |
| `go test -race -cover ./...` | Pass; config 93,1%, control 97,6%, engine 100% |
| `go vet ./...` | Pass |
| `go mod tidy` | Pass; YAML dependency được pin trong go.mod/go.sum |

Commands đã chạy qua `rtk proxy`; dùng `env GOCACHE=/private/tmp/faultline-go-build` cho build/test/vet vì sandbox không cho ghi Go build cache mặc định. Race tests gồm 1.000 request chọn sequence đồng thời và acquire/apply/toggle đồng thời. Statistical test dùng 10.000 mẫu ở p=0,2, khoảng chấp nhận định trước 1.700–2.300.

Review đã kiểm tra ownership, scope/counter reset, no-op normalization và capability validation. Knowledge graph chưa có node nên đã review source trực tiếp. Không còn blocker trong phase 1; chưa có CLI, HTTP traffic, fault execution hoặc assertion end-to-end.
