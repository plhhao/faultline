# P28 — BullMQ over Redis/RESP contract

- Trạng thái: **Planned**.
- Phụ thuộc: nền config/control/recorder hiện có; không phụ thuộc Phase 8,
  Phase 10 hay RabbitMQ.
- Nguồn: [phạm vi Phase 11](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi khi thực hiện: contract/fixture trace trước; P29 mới thay đổi
  adapter, config, API/UI, examples và integration tests sau khi contract Done.

## DEFINE

Chốt BullMQ semantics trên RESP trước khi viết adapter. `Queue.add` thường dùng
Lua script và BullMQ có thể mở nhiều Redis connections; Redis replies theo thứ tự
trên một connection nhưng pipeline, transaction, retry, script cache và blocking
connections làm "một command = một job" không còn đúng. P28 phải chứng minh
điểm gọi script nào là successful job add trong version đã pin.

P28 không hiện thực proxy. Kết quả là contract/version/trace thật cho P29.

## PLAN → BUILD

1. Pin Node.js, BullMQ, Redis client (mặc định ioredis nếu fixture dùng),
   Redis hoặc Valkey, direct standalone topology, image digest, queue prefix và
   Docker fixture. Không dùng Redis/BullMQ cài đặt của người dùng làm evidence.
2. Trace wire-safe flow của `Queue.add` thường và retry/cold-script path:
   handshake/TLS/auth command type, `EVAL`/`EVALSHA`, `NOSCRIPT`, argument count,
   reply type/length, pipeline/MULTI/EXEC nếu có, duplicate/blocking connection
   và disconnect/reconnect. Không lưu Redis password, ACL user, script argument
   values, job data/options hoặc token.
3. Định nghĩa lifecycle/metadata:
   - Một add flow gồm chuỗi command đã pin và chỉ quyết định selector khi queue
     identity được nhận diện an toàn.
   - `after_job_add` chỉ reached khi complete script/transaction có successful
     Redis reply theo trace. Redis `ERR`, `NOSCRIPT`, timeout, partial write hay
     reply command phụ không là successful add.
   - Queue name chỉ được parse từ key syntax/prefix đã pin; mismatch hoặc key
     không nhận diện chỉ forward, không match semantic rule.
   - Job tồn tại trên Redis không đồng nghĩa Worker đã nhận/xử lý job. Không
     suy ra business success hoặc idempotency từ reply proxy.
4. Chốt capability matrix. Mặc định đề xuất `delay`, `hold_response` và
   `close_connection` tại `after_job_add`. Không thêm response mutation, generic
   Redis command fault, Worker/QueueEvents fault hoặc `addBulk` nếu trace không
   chứng minh correlation/order/lifecycle.
5. Chốt TLS/auth matrix cho app → Faultline và Faultline → Redis. Adapter chỉ
   parse leg đã terminate TLS; CA/hostname upstream bắt buộc verify, không
   fallback plaintext. Redis AUTH/ACL credential chỉ forward và không log; mTLS
   chỉ nằm trong matrix nếu fixture pass.
6. Chốt correlation/ordering theo connection. Pending reply phải theo RESP FIFO
   và transaction boundary; pipeline reply không được reorder. Nếu delay/hold
   cần pause connection reader/writer hoặc ảnh hưởng command theo sau, contract
   phải nêu blast radius, backpressure và giới hạn queue.
7. Chốt frame/session/lifecycle bounds: RESP bulk length/nesting, command/reply
   buffer, pending operation, hold duration, blocking connection, idle deadline,
   client disconnect, reconnect, malformed RESP, Redis close và shutdown. Proxy
   không tự reconnect hoặc retry job add.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| P28-AC1 | Contract pin Node/BullMQ/Redis/client/image digest, topology/prefix fixture và trace không chứa credential hoặc job payload. |
| P28-AC2 | Trace `Queue.add` normal/cold-script/retry xác định chính xác command-reply boundary của successful add; `NOSCRIPT`, Redis ERR và partial operation không bị nhận nhầm. |
| P28-AC3 | Trace persistent, pipelined/transaction (nếu client phát sinh), duplicate và blocking connections xác định FIFO/order/blast-radius policy. |
| P28-AC4 | Matrix TLS/auth plaintext/TLS/mTLS từng leg, CA/hostname, ACL/password secrecy và các trường hợp từ chối được chốt; không downgrade. |
| P28-AC5 | Queue-key metadata mapping, frame/session/hold/deadline bounds, malformed RESP, reconnect và shutdown policy có thể kiểm thử ở P29. |

Ghi contract hoàn chỉnh tại `bullmq-contract.md` khi P28 được thực hiện, gồm
lệnh, pin versions, trace summary và giới hạn thật. Không chuyển P28 sang Done
chỉ vì đã đọc source BullMQ hoặc Redis protocol.

## REVIEW

Review việc phân biệt BullMQ library với Redis wire protocol, script/reply
correlation, FIFO/pipeline ordering, TLS/auth secrecy và connection lifecycle.
Không suy ra queue from arbitrary Redis keys, không log payload và không gọi job
đã add là job đã xử lý.

## Sources to consult

- [BullMQ connections](https://docs.bullmq.io/guide/connections)
- [BullMQ production reconnect guidance](https://docs.bullmq.io/guide/going-to-production)
- [Redis serialization protocol specification](https://redis.io/docs/latest/develop/reference/protocol-spec/)
- [Redis TLS documentation](https://redis.io/docs/latest/operate/oss_and_stack/management/security/encryption/)
