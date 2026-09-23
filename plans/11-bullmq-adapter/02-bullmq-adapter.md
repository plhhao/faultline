# P29 — BullMQ over Redis/RESP adapter

- Trạng thái: **Done** (2026-09-23).
- Phụ thuộc: [P28](01-bullmq-contract.md) **Done** với pinned contract và trace.
- Nguồn: [phạm vi Phase 11](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi dự kiến: `internal/proxy/bullmq/`, `internal/config/`,
  `internal/control/remote/`, `internal/recorder/` nếu cần,
  `examples/bullmq/`, `tests/integration/`, docs và Docker fixture.

## DEFINE

Hiện thực đúng slice `Queue.add` trong contract P28, tái sử dụng
engine/control/recorder và không tạo Redis proxy framework. Flow được chọn là
BullMQ add operation nhận diện từ command sequence đã pin, không phải raw TCP
connection hay mọi Redis command. `close_connection` vẫn có tác động lên toàn
connection theo contract.

Use case chính: Lua script đã thêm job thành công tại Redis, Faultline giữ hoặc
đóng trước reply, caller reconnect/retry và fixture quan sát độc lập job đã tồn
tại. Faultline không tự retry và không kết luận retry có idempotent hay không.

## PLAN → BUILD

1. Thêm `protocol: bullmq` và upstream scheme/name chỉ khi P28 chốt schema.
   Validation chỉ nhận queue matcher, action, phase và TLS/auth combinations
   được P28 cho phép. Listener/protocol/upstream/TLS thay đổi cần restart;
   reload chỉ áp dụng rule/seed cho flow mới theo contract.
2. Hiện thực bounded RESP session parser/writer, TLS/auth forwarding, persistent
   client/duplicate/blocking connection lifecycle và safe command classification.
   Unknown/unrecognized Redis commands chỉ được forward theo policy P28; chúng
   không tạo BullMQ semantic flow hay recorder event chứa data.
3. Nhận diện đầy đủ pinned `Queue.add` script flow và pending replies FIFO,
   gồm script cache/retry path, pipeline/transaction nếu P28 yêu cầu. Pin rule
   decision đến `after_job_add`; ghi `not_reached` nếu operation không có
   successful reply theo contract.
   Ánh xạ reply theo bảng outcome P28, gồm duplicate và lỗi semantic; không
   gán nhãn tạo job mới khi wire reply không chứng minh được. Áp dụng đúng đơn
   vị flow, snapshot, selector counter và event lifecycle cho cold-script và
   reconnect/retry, kể cả reload/enable/disable giữa chuỗi.
4. Áp dụng delay, hold_response và close_connection sau successful job-add
   reply mà không reorder pipeline response, duplicate job hoặc retry tại proxy.
   Ghi ordering/backpressure và blast radius khi có command khác cùng connection.
5. Tích hợp control CLI/API/UI, persisted config và counters/events. UI chỉ hiển
   fields BullMQ hợp lệ. Event chứa protocol, revision, rule, queue/operation
   safe identifier và selected/reached/applied/outcome; không chứa job data,
   options, Redis command args hoặc credentials.
6. Thêm Node/BullMQ Docker fixture theo pin P28: normal add, known job ID retry
   và generated job ID retry; consumer/Redis independent observation, reconnect
   behavior và cleanup. Tài liệu nêu rõ configuration/TLS and expected outcomes.
   Dùng timeout/reconnect/client resend/application retry đã pin ở P28, giới
   hạn thời gian và số attempt, fault lần đầu bằng `nth=1`. Ghi nguồn của từng
   attempt và giữ job đủ lâu cho quan sát độc lập trước cleanup.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| P29-AC1 | BullMQ `Queue.add` pass-through trong matrix P28, gồm normal/cold-script path, persistent connection và fixture consumer quan sát đúng job metadata an toàn. |
| P29-AC2 | Queue matcher (nếu contract mapping cho phép), probability 0/1, nth/every, first-match, disabled rule, reload/snapshot chọn đúng add flow; raw Redis command không bị semantic match. |
| P29-AC3 | `after_job_add` chỉ reached sau reply Redis thành công đúng script/transaction; `NOSCRIPT`, Redis ERR, timeout, reply phụ và job processing không bị nhận nhầm. |
| P29-AC4 | Delay trả reply theo duration. Hold/close làm caller timeout/lỗi nhưng quan sát Redis/consumer độc lập chứng minh job đầu đã tồn tại; proxy không tự retry. |
| P29-AC5 | ioredis/BullMQ reconnect/retry, normal/duplicate/blocking connection và pipeline/transaction trong matrix tuân thủ FIFO/order/blast-radius contract; không rò pending state. |
| P29-AC6 | Known job ID và generated job ID retry có evidence riêng; fixture ghi duplicate/idempotent outcome thực tế, không kết luận ứng dụng đúng/sai. |
| P29-AC7 | TLS/auth matrix P28 pass; CA/hostname/mTLS errors bị từ chối theo contract, không fallback plaintext và log/event không lộ Redis credential/job payload. |
| P29-AC8 | Oversized/malformed RESP, nested/bulk bounds, timeout, client disconnect, Redis close, reconnect và shutdown không treo/rò goroutine/connection; counters về 0. |
| P29-AC9 | Regression HTTP/HTTP2/gRPC/PostgreSQL/MySQL/RabbitMQ, config/control/API/UI, Go tests/race/vet, binary/Docker build, BullMQ Docker runtime và docs/examples/link checks có kết quả thật. |
| P29-AC10 | New/duplicate job ID, lỗi semantic và reply không nhận diện tuân thủ bảng outcome P28; duplicate reached hoặc không reached đúng policy, event không claim tạo mới vượt quá bằng chứng wire. |
| P29-AC11 | Warm/cold-script và reconnect/retry có số flow, `nth/every`, decision và terminal event đúng P28; reload/enable/disable giữa chuỗi không làm đổi snapshot đã pin hoặc đếm lặp ngoài contract. |
| P29-AC12 | Known/generated job ID retry hoàn tất trong time/attempt bounds đã pin, fault lần đầu `nth=1`; evidence phân biệt client resend với application retry và quan sát job độc lập trước cleanup. |

Ghi lệnh, pinned versions, môi trường và PASS/FAIL/SKIP tại
`bullmq-acceptance.md` khi thực hiện. Không dùng evidence adapter khác để claim
Redis hoặc BullMQ support ngoài matrix P28.

## REVIEW

Review RESP parser/limits, script-flow recognition, FIFO/pipeline ordering,
reconnect cleanup, TLS/auth secrecy và rule lifecycle. Xác nhận không đưa Redis
I/O vào engine, không log job payload, không gọi script reply là job processed,
không mở rộng sang generic Redis, Cluster/Sentinel, BullMQ Flow/Worker/QueueEvents
hoặc Kafka ngoài P28. Chỉ chuyển P29 và Phase 11 sang Done khi P29-AC1–AC12 đạt
và evidence được ghi.

## Kết quả

Done 2026-09-23: P29-AC1–AC12 PASS, lệnh và bằng chứng tại
[bullmq-acceptance.md](bullmq-acceptance.md). Quyết định khi thực hiện: idle
connection đóng sau `runtime.request_timeout` giống adapter database; lệnh
làm thay đổi cardinality reply (`MULTI`, `HELLO`, Pub/Sub, `SELECT`…) đóng
connection trước khi forward; Worker không được dùng làm bằng chứng.
