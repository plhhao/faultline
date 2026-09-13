# Kế hoạch hiện thực Faultline

Nguồn yêu cầu: [specific.md](../specific.md). **Phase 01 đã Done**, gồm P01–P03 và [P01a — cấu hình nhiều file](01-core/04-multi-file-config.md), snapshot và rule engine. Các plan MVP còn lại chưa hiện thực; các plan sau MVP vẫn Deferred.

## Các phase

Phase 01–05 chia nhỏ giai đoạn A trong mục 13 của đặc tả. Phase 06–09 tương ứng B–E, ở trạng thái Deferred. Thứ tự sau MVP có thể đổi theo nhu cầu; UI không phải đợi hỗ trợ thêm protocol.

| Phase | Phạm vi | Kết quả | Số plan |
| --- | --- | --- | --- |
| [01 — Core và cấu hình](01-core/README.md) | MVP | Schema, include nhiều file, snapshot và quyết định fault độc lập với HTTP I/O. | 4 |
| [02 — HTTP/HTTPS proxy](02-http-proxy/README.md) | MVP | CLI validate/serve và proxy nhiều listener chuyển tiếp HTTP/1.1, TLS, lifecycle hooks. | 3 |
| [03 — Fault actions](03-fault-actions/README.md) | MVP | Tất cả action MVP hoạt động tại đúng phase, có cancellation và bằng chứng kiểm thử. | 3 |
| [04 — Điều khiển runtime và quan sát](04-runtime-control/README.md) | MVP | Thao tác CLI trên process đang chạy, reload atomically, JSON events và counters. | 3 |
| [05 — Nghiệm thu và bàn giao MVP](05-mvp-delivery/README.md) | MVP | Đạt AC1–AC24, có demo lost response và chạy được bằng binary/Docker. | 3 |
| [06 — Mở rộng protocol và fault](06-protocol-extensions/README.md) | Sau MVP | Chứng minh khả năng mở rộng qua adapter thứ hai; thêm HTTP capabilities theo nhu cầu. | 3 |
| [07 — API và UI cho tester](07-tester-experience/README.md) | Sau MVP | Tester quản lý cấu hình trên server test chung qua cùng control service. | 2 |
| [08 — Scenario và đánh giá kết quả](08-failure-testing/README.md) | Sau MVP | Điều phối kịch bản, correlation có bằng chứng và kết quả PASS/FAIL/inconclusive. | 2 |
| [09 — Adapter theo công nghệ](09-semantic-adapters/README.md) | Sau MVP | Một adapter database/broker có semantics và integration tests được xác định rõ. | 2 |

## Thứ tự và ranh giới

- MVP: phase 01 → 02 → 03 → 04 → 05. Theo cột phụ thuộc của từng plan; có thể làm các plan độc lập sau khi đủ đầu vào.
- Hoàn tất P01a trước P04; MC1–MC4 được kiểm chứng ở P01a/P11, MC5 ở P04/P11/P15. Các tiêu chí bổ sung không thay thế AC1–AC24.
- Sau MVP: phase 06, 07, 08 và bước thiết kế phase 09 đều có thể bắt đầu từ P15 theo ưu tiên thực tế. gRPC cần nền HTTP/2; adapter công nghệ có thể bổ sung phụ thuộc sau khi chọn protocol.
- P01–P15 và P01a là kế hoạch cụ thể cho MVP; P16–P24 là khung công việc mở rộng cần DEFINE lại khi được chọn, không phải cam kết làm mọi capability.
- Tận dụng cấu trúc package hiện tại. Chỉ thêm package/interface khi implementation cần; ưu tiên helper có trách nhiệm rõ, không tạo sẵn common/utils hoặc framework plugin.
- Schema, CLI transport và defaults còn là đề xuất trong đặc tả: chốt ở plan sở hữu, ghi quyết định và cập nhật tài liệu liên quan nếu thay đổi hợp đồng.

## Theo dõi thực hiện

Mỗi tính năng tuân theo [workflow](../.agents/rules/workflow.md): **DEFINE → PLAN → BUILD → VERIFY → REVIEW → Done**.

- Trạng thái: `Planned` → `In progress` → `Done`; dùng `Blocked` kèm lý do nếu thiếu đầu vào bắt buộc. `Deferred` dành cho công việc chưa được ưu tiên.
- Khi bắt đầu, kiểm tra spec và phụ thuộc đã hoàn tất, làm rõ acceptance criteria và cập nhật plan nếu thông tin mới thay đổi thiết kế.
- Khi thực hiện, ghi tiến độ/quyết định ngắn ngay trong plan. VERIFY ghi lệnh, kết quả thật, checks bị bỏ qua và lý do; REVIEW ghi findings và cách xử lý.
- Chỉ Done khi đạt tiêu chí, required checks không còn unresolved và review không còn blocker. Sửa lỗi thì chạy lại checks bị ảnh hưởng, review lại.
- Sau Done, cập nhật trạng thái plan/phase và thêm kết quả vào [CHANGELOG.md](../CHANGELOG.md). Hoàn tất việc viết kế hoạch không có nghĩa tính năng đã Done.
- Unit tests đặt cạnh code; integration tests trong `tests/integration/`. Khi có Go source, dùng `rtk proxy go test ./...`, `rtk proxy go test -race ./...`, `rtk proxy go vet ./...` và build CLI khi phù hợp. Những lệnh này chưa được coi là đã chạy chỉ vì xuất hiện trong plan.

## Đối chiếu nghiệm thu MVP

Bảng chỉ vị trí sở hữu kiểm chứng; nhiều AC cần cả unit và integration tests. P14 tổng hợp AC1–AC23, P15 kiểm tra packaging và gate cuối AC1–AC24.

| AC | Plan kiểm chứng chính |
| --- | --- |
| AC1 | [P04](02-http-proxy/01-cli-and-forwarding.md) |
| AC2 | [P04](02-http-proxy/01-cli-and-forwarding.md), [P08](03-fault-actions/02-close-connection.md) |
| AC3 | [P03](01-core/03-rule-engine.md), [P08](03-fault-actions/02-close-connection.md) |
| AC4 | [P03](01-core/03-rule-engine.md) |
| AC5 | [P03](01-core/03-rule-engine.md) |
| AC6 | [P03](01-core/03-rule-engine.md) |
| AC7 | [P07](03-fault-actions/01-delay-and-respond.md) |
| AC8 | [P07](03-fault-actions/01-delay-and-respond.md) |
| AC9 | [P09](03-fault-actions/03-hold-request-response.md), [P14](05-mvp-delivery/02-payment-demo.md) |
| AC10 | [P02](01-core/02-runtime-snapshots.md), [P11](04-runtime-control/02-atomic-reload.md) |
| AC11 | [P01](01-core/01-config-schema.md), [P11](04-runtime-control/02-atomic-reload.md) |
| AC12 | [P02](01-core/02-runtime-snapshots.md), [P11](04-runtime-control/02-atomic-reload.md) |
| AC13 | [P06](02-http-proxy/03-flow-lifecycle.md), [P12](04-runtime-control/03-events-and-counters.md) |
| AC14 | [P09](03-fault-actions/03-hold-request-response.md), [P13](05-mvp-delivery/01-resource-limits.md) |
| AC15 | [P12](04-runtime-control/03-events-and-counters.md), [P13](05-mvp-delivery/01-resource-limits.md) |
| AC16 | [P12](04-runtime-control/03-events-and-counters.md) |
| AC17 | [P04](02-http-proxy/01-cli-and-forwarding.md), [P10](04-runtime-control/01-local-admin.md) |
| AC18 | [P10](04-runtime-control/01-local-admin.md), [P12](04-runtime-control/03-events-and-counters.md) |
| AC19 | [P10](04-runtime-control/01-local-admin.md), [P11](04-runtime-control/02-atomic-reload.md) |
| AC20 | [P03](01-core/03-rule-engine.md), [P10](04-runtime-control/01-local-admin.md) |
| AC21 | [P09](03-fault-actions/03-hold-request-response.md) |
| AC22 | [P05](02-http-proxy/02-https.md), [P07](03-fault-actions/01-delay-and-respond.md), [P08](03-fault-actions/02-close-connection.md), [P09](03-fault-actions/03-hold-request-response.md), [P14](05-mvp-delivery/02-payment-demo.md) |
| AC23 | [P01](01-core/01-config-schema.md), [P05](02-http-proxy/02-https.md) |
| AC24 | [P15](05-mvp-delivery/03-binary-docker.md) |
