# P29 — BullMQ over Redis/RESP adapter

- Trạng thái: **Planned**.
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

Ghi lệnh, pinned versions, môi trường và PASS/FAIL/SKIP tại
`bullmq-acceptance.md` khi thực hiện. Không dùng evidence adapter khác để claim
Redis hoặc BullMQ support ngoài matrix P28.

## REVIEW

Review RESP parser/limits, script-flow recognition, FIFO/pipeline ordering,
reconnect cleanup, TLS/auth secrecy và rule lifecycle. Xác nhận không đưa Redis
I/O vào engine, không log job payload, không gọi script reply là job processed,
không mở rộng sang generic Redis, Cluster/Sentinel, BullMQ Flow/Worker/QueueEvents
hoặc Kafka ngoài P28. Chỉ chuyển P29 và Phase 11 sang Done khi P29-AC1–AC9 đạt
và evidence được ghi.
