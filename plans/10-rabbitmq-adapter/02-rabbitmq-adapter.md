# P27 — RabbitMQ AMQP 0-9-1 adapter

- Trạng thái: **In progress — automated publisher-confirm fixtures pass; see [acceptance](rabbitmq-acceptance.md)**.
- Phụ thuộc: [P26](01-rabbitmq-contract.md) **Done** với contract và fixture
  matrix đã pin.
- Nguồn: [phạm vi Phase 10](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi dự kiến: `internal/proxy/rabbitmq/`, `internal/config/`,
  `internal/control/remote/`, `internal/recorder/` nếu cần,
  `examples/rabbitmq/`, `tests/integration/`, docs và Docker fixture.

## DEFINE

Hiện thực đúng contract P26, tái sử dụng engine/control/recorder và không tạo
plugin framework. Đơn vị chọn fault là publish hoặc consumer delivery đã định
nghĩa trong contract, không phải connection. Một connection có thể giữ nhiều
channel và operations; `close_connection` vẫn cố ý là fault cấp connection.

Use case chính: RabbitMQ đã gửi publisher confirm thành công, Faultline chặn
confirm đó, publisher timeout hoặc mất connection, reconnect và retry. Độc lập
kiểm tra broker để chứng minh message đầu tiên đã tồn tại trước retry.

## PLAN → BUILD

1. Thêm `protocol: rabbitmq` và upstream scheme/name chỉ sau khi P26 chốt schema.
   Validation chỉ nhận matcher, phase, action và TLS/auth combination trong
   capability matrix. Các protocol fields thay đổi phải thuộc restart
   fingerprint; reload chỉ thay rule/seed theo contract hiện có.
2. Hiện thực session/frame state machine có bounded reader/writer, TLS theo
   contract, handshake/auth forwarding, heartbeat và channel lifecycle. Không
   buffer message body hoặc confirm queue không giới hạn.
3. Theo dõi publish đầy đủ và pending publisher confirms theo channel/sequence;
   xử lý `multiple`, nack, return, unexpected frame và close đúng contract.
   Quyết định rule được giữ cho đến phase hoặc ghi `not_reached`.
4. Áp dụng `delay`, `hold_response`, `close_connection` sau confirmed publish
   mà không retry hay duplicate message ở proxy. Ghi rõ response/frame ordering
   và blast radius khi fault xảy ra trên connection có channel khác.
5. Forward/kiểm thử consumer lifecycle trong phạm vi P26. Không gọi consumer ack
   là confirmed broker result; delivery delay/close chỉ thêm nếu capability P26
   đã chốt.
6. Tích hợp control CLI/API/UI, config persistence, event/counter. UI chỉ hiển
   matcher/phase/action RabbitMQ hợp lệ. Event ghi protocol, revision, rule,
   channel/operation safe identifier và selected/reached/applied/outcome; không
   ghi credentials, headers chứa secret hoặc payload.
7. Thêm Docker fixture RabbitMQ và client thật đã pin. Hướng dẫn rõ certificate,
   endpoint, reconnect/retry, expected duplicate/redelivery và cleanup.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| P27-AC1 | Plain pass-through trong matrix P26: handshake/auth, heartbeat, nhiều channel, publish/confirm, consume/deliver/ack/nack hoạt động với broker/client thật. |
| P27-AC2 | `exchange`, `routing_key`, `consumer_tag` (nếu contract cho phép), probability 0/1, nth/every, first-match, disabled rule, reload và snapshot chọn đúng logical flow trên connection dài hạn. |
| P27-AC3 | `after_publish_confirm` chỉ reached sau upstream confirm đúng sequence/channel; `multiple`, nack, return và close không bị nhận nhầm là successful confirm. |
| P27-AC4 | Delay chuyển đúng confirm sau khoảng cấu hình. Hold/close làm publisher timeout/lỗi trong khi consumer độc lập quan sát message đã publish; proxy không tự retry. |
| P27-AC5 | Client reconnect/retry với persistent connection có evidence cho kết quả first publish và retry; fixture ghi rõ trường hợp duplicate hay idempotent outcome, không tự kết luận ứng dụng đúng/sai. |
| P27-AC6 | Concurrent channel/in-flight operation, delayed confirmation, heartbeat và fault connection tuân thủ ordering/blast-radius contract; operation không liên quan không bị phân loại nhầm. |
| P27-AC7 | TLS/auth matrix P26 pass, CA/hostname/mTLS lỗi bị từ chối theo contract và không fallback plaintext; recorder/log không lộ credential/payload. |
| P27-AC8 | Oversized/malformed frame, timeout, client disconnect, broker close, shutdown và resource bounds không treo/rò goroutine/connection; counters về 0. |
| P27-AC9 | Regression HTTP/HTTP2/gRPC/PostgreSQL/MySQL, config/control/API/UI, Go tests/race/vet, binary/Docker build và RabbitMQ Docker runtime có kết quả thật; docs/examples/link checks pass. |

Ghi lệnh, phiên bản, môi trường và PASS/FAIL/SKIP tại `rabbitmq-acceptance.md`
khi thực hiện. Không dùng evidence từ protocol khác để claim RabbitMQ support.

## REVIEW

Review frame/channel state machine, confirm correlation và `multiple` handling,
TLS/auth secrecy, ordering/backpressure, disconnect/reconnect and shutdown.
Xác nhận không đưa AMQP I/O vào engine, không biến consumer `basic.ack` thành
publisher confirm, không mở rộng AMQP 1.0/Streams/cluster ngoài P26. Chỉ chuyển
P27 và Phase 10 sang Done khi toàn bộ P27-AC1–AC9 đạt và evidence được ghi.

## BUILD / VERIFY — 2026-09-21

Added `internal/proxy/rabbitmq`, `protocol: rabbitmq`, AMQP matcher/phase,
binary/managed UI registration, a public example and pinned RabbitMQ Docker
fixtures. Plaintext and TLS/TLS publisher confirms, delay/hold/close, direct
message observation, consumer delivery/ack, returned mandatory publish,
frame bounds and selected unit/UI tests pass. P27 remains In progress because
the acceptance record still carries multi-channel/heartbeat broker evidence,
application retry policy, selector reload behavior and lifecycle-bound gates.
