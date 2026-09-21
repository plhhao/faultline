# Phase 11 — BullMQ qua Redis/RESP adapter

Phạm vi: **Sau MVP**. Trạng thái: **Planned**.

## DEFINE — Phạm vi dự kiến

BullMQ là thư viện queue chạy trên Redis/Valkey, không phải wire protocol riêng.
Phase này là adapter hiểu một slice RESP đủ để kiểm thử BullMQ đã thêm job thành
công nhưng caller không nhận được kết quả; nó không biến Faultline thành generic
Redis proxy và không diễn giải mọi command Redis là job operation.

Phạm vi dự kiến:

- BullMQ `Queue.add` một job qua Redis direct standalone node. P28 phải pin
  BullMQ, Redis/Valkey lựa chọn, Redis client và Docker image trước khi code.
- Adapter trace command sequence thực tế của client, gồm Lua
  `EVAL`/`EVALSHA`, script cache miss, pipeline/transaction nếu fixture dùng;
  không giả định command sequence giữa các version BullMQ là giống nhau.
- Fault trọng tâm `after_job_add`: Redis đã trả reply thành công cho complete
  add-job script/operation nhưng Faultline delay, hold hoặc đóng connection trước
  khi caller nhận reply.
- Metadata matcher dự kiến là queue name/operation chỉ khi rút ra được từ key và
  command đã pin một cách xác định. Không match/lưu job data, options, token,
  Redis password hoặc arbitrary key/value.
- TLS termination ở mỗi chặng và Redis authentication có matrix rõ; không
  fallback plaintext. mTLS chỉ được công bố sau fixture hai phía.
- Engine, control, selector và recorder dùng chung; RESP parser, command/reply
  correlation, connection state và protocol I/O thuộc adapter.

Không thuộc phase: generic Redis command faulting, Redis Cluster/Sentinel,
Pub/Sub/RESP3 push, Redis Functions ngoài trace cần thiết, BullMQ Pro,
FlowProducer, queue scheduler/rate limiter, `Queue.addBulk`, Worker/QueueEvents
semantics, payload editing, proxy retry, RabbitMQ và Kafka. Worker có thể chỉ
được fixture dùng để quan sát job đã tồn tại, không là capability semantic.

## Các plan

| Thứ tự | Plan | Kết quả | Phụ thuộc | Trạng thái |
| --- | --- | --- | --- | --- |
| 1 | [P28](01-bullmq-contract.md) | Pin BullMQ/Redis/client, trace RESP và `Queue.add` contract | Nền config/control/recorder hiện có | Planned |
| 2 | [P29](02-bullmq-adapter.md) | Adapter, config/API/UI, Node/Docker fixture và acceptance evidence | P28 Done | Planned |

## Điều kiện hoàn tất phase

- Contract pin version/image/client, command/reply trace và queue-key mapping;
  command hoặc topology ngoài matrix không được gán nhãn BullMQ semantic.
- `after_job_add` chỉ reached sau successful Redis reply cho operation add đã
  được contract nhận diện; không phải chỉ sau khi client gửi command hoặc sau
  reply của command phụ.
- Fixture Node/BullMQ thật chứng minh add bình thường, job đã tồn tại nhưng
  caller timeout/disconnect, reconnect/retry và kết quả được quan sát độc lập.
- Connection dài hạn, pipeline/transaction và blocking connection của client
  được xử lý theo contract. Fault cấp connection phải nêu blast radius.
- TLS/auth, resource bounds, control/UI/recorder, full regression và docs có
  evidence thật; các khả năng ngoài scope được từ chối hoặc ghi rõ giới hạn.

## Quyết định cần chốt ở P28

1. BullMQ, Node.js, Redis/Valkey và Redis client version; image digest, direct
   topology, queue prefix và fixture job options.
2. Trace chính xác `Queue.add` thành RESP command/reply, Lua script cache miss,
   pipeline/MULTI/EXEC nếu xuất hiện, duplicate connection và reconnect behavior.
3. Queue metadata derivation không lộ payload; policy script hash đổi, `NOSCRIPT`,
   Redis `ERR`, timeout hoặc reply không nhận diện.
4. Ordering response trên RESP connection. Delay/hold một reply không được
   reorder pipeline response; contract phải ghi gate scope và backpressure.
5. TLS/auth matrix, Redis ACL/password secrecy, frame/buffer, in-flight, hold,
   blocking command, timeout, reconnect, malformed RESP và shutdown bounds.

Xem [lộ trình và quy tắc thực hiện](../README.md).
