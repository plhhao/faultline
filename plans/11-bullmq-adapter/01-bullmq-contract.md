# P28 — BullMQ over Redis/RESP contract

- Trạng thái: **Done**.
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
   - Lập bảng reply → semantic outcome cho script đã pin: tạo job mới,
     duplicate job ID, lỗi semantic và reply không nhận diện. Chốt duplicate có
     reached `after_job_add` hay không; không coi mọi reply không phải Redis
     `ERR` là tạo job mới. Nếu wire reply không phân biệt được new/duplicate,
     contract phải ghi giới hạn và định nghĩa phase theo điều quan sát được.
   - Chốt đơn vị flow cho `EVALSHA → NOSCRIPT → EVAL` và reconnect/retry:
     thời điểm lấy snapshot, tăng `nth/every`, giữ hay lấy decision mới và kết
     thúc recorder event. Nêu cách correlation dựa trên wire trace; không giả
     định proxy nhận diện được cùng một lời gọi ứng dụng qua reconnect. Chốt
     hành vi khi reload/enable/disable xảy ra giữa các bước của chuỗi.
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
8. Pin retry fixture: timeout của caller, reconnect policy, tự gửi lại command
   của client và retry tường minh của ứng dụng; ghi rõ lớp nào phát sinh từng
   attempt. Giới hạn tổng thời gian và số attempt, dùng `nth=1` cho fault lần
   đầu để retry có thể hoàn tất. Known/generated job ID có case riêng; giữ job
   đủ lâu để quan sát độc lập, chốt cleanup và expected outcome theo trace.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| P28-AC1 | Contract pin Node/BullMQ/Redis/client/image digest, topology/prefix fixture và trace không chứa credential hoặc job payload. |
| P28-AC2 | Trace `Queue.add` normal/cold-script/retry xác định chính xác command-reply boundary của successful add; `NOSCRIPT`, Redis ERR và partial operation không bị nhận nhầm. |
| P28-AC3 | Trace persistent, pipelined/transaction (nếu client phát sinh), duplicate và blocking connections xác định FIFO/order/blast-radius policy. |
| P28-AC4 | Matrix TLS/auth plaintext/TLS/mTLS từng leg, CA/hostname, ACL/password secrecy và các trường hợp từ chối được chốt; không downgrade. |
| P28-AC5 | Queue-key metadata mapping, frame/session/hold/deadline bounds, malformed RESP, reconnect và shutdown policy có thể kiểm thử ở P29. |
| P28-AC6 | Bảng reply → outcome phân biệt new/duplicate/error trong giới hạn quan sát được; policy `after_job_add` cho duplicate và reply không nhận diện được chốt bằng trace. |
| P28-AC7 | Cold-script và reconnect/retry có định nghĩa flow, snapshot, `nth/every`, decision và event lifecycle; reload/enable/disable giữa chuỗi có expected outcome kiểm thử được. |
| P28-AC8 | Fixture pin caller timeout, reconnect, client resend và application retry; có time/attempt bounds, fault lần đầu `nth=1`, known/generated job ID, retention và quan sát độc lập với expected outcomes. |

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
