# Phase 10 — RabbitMQ AMQP 0-9-1 adapter

Phạm vi: **Sau MVP**. Trạng thái: **Done — P26/P27; P27-AC1–AC9 verified 2026-09-22**.

## DEFINE — Phạm vi dự kiến

Mục tiêu là adapter hiểu AMQP 0-9-1 để mô phỏng kết quả mơ hồ khi RabbitMQ đã
xác nhận publish nhưng publisher mất xác nhận đó. Đây không phải TCP proxy tổng
quát: một connection có thể chứa nhiều channel, publisher confirms, consumer
delivery, heartbeat và nhiều operation đồng thời.

Phạm vi dự kiến của phase:

- RabbitMQ direct node, AMQP 0-9-1; phiên bản broker, Go AMQP client và Docker
  image phải được pin ở P26 trước khi code.
- Publisher bật confirm mode, `basic.publish` và `basic.ack`/`basic.nack` từ
  broker; consumer `basic.consume`, `basic.deliver`, `basic.ack` và `basic.nack`
  được forward và kiểm chứng lifecycle.
- Fault trọng tâm tại `after_publish_confirm`: `delay`, `hold_response` và
  `close_connection` sau khi adapter thấy publisher confirm thành công từ
  upstream, trước khi chuyển confirm đó cho client.
- Metadata matcher dự kiến chỉ gồm `exchange`, `routing_key` và `consumer_tag`
  khi protocol cung cấp chính xác. Không suy ra queue đích của publish từ
  bindings, payload hoặc tên key.
- TLS termination ở hai chặng và authentication phải có contract/matrix rõ
  ràng. Không fallback plaintext; mTLS chỉ được công bố khi fixture kiểm chứng.
- Engine, control, selector và recorder tiếp tục dùng chung; AMQP framing,
  channel state và connection I/O nằm trong adapter.

Không thuộc phase: AMQP 1.0, RabbitMQ Streams/MQTT/STOMP, management API,
cluster/federation/shovel, topology replication, message payload matching/editing,
publisher retry của proxy, BullMQ/Redis và Kafka. Không tuyên bố semantic support
chỉ nhờ có TCP forwarding.

## Các plan

| Thứ tự | Plan | Kết quả | Phụ thuộc | Trạng thái |
| --- | --- | --- | --- | --- |
| 1 | [P26](01-rabbitmq-contract.md) | Pin version/client; AMQP, TLS/auth, channel và publisher-confirm contract | Nền config/control/recorder hiện có | Done |
| 2 | [P27](02-rabbitmq-adapter.md) | Adapter, config/API/UI, fixture Docker và acceptance evidence | P26 Done | Done |

## Điều kiện hoàn tất phase

- Có contract pin broker/client, capability matrix và giới hạn frame/session;
  mọi protocol/auth/TLS không nằm trong matrix bị từ chối hoặc chuyển tiếp theo
  policy đã ghi, không downgrade âm thầm.
- `after_publish_confirm` chỉ áp dụng khi upstream gửi confirm thành công đúng
  publish sequence/channel; không đồng nhất mọi AMQP `basic.ack` với confirm.
- Fixture client thật chứng minh: publish bình thường, publish đã được broker
  xác nhận nhưng client nhận timeout/disconnect, reconnect/retry và kết quả
  message được quan sát độc lập.
- Connection dài hạn có nhiều channel/in-flight operation được xử lý theo
  contract; fault cấp connection nêu rõ ảnh hưởng tới operation khác.
- HTTP/HTTP2/gRPC/PostgreSQL/MySQL, control, recorder, binary/Docker và UI/API
  regression pass; tài liệu nêu rõ giới hạn và cấu hình được kiểm chứng.

## Quyết định cần chốt ở P26

1. RabbitMQ image/version, Go AMQP client/version và kiểu queue/exchange của
   fixture.
2. Matrix plaintext/TLS/mTLS từng chặng, certificate trust và cơ chế AMQP auth
   được forward mà không log credential.
3. Semantics chính xác của publisher confirms gồm `multiple` acknowledgement,
   `basic.nack`, mandatory return, channel close và message persistence trong
   fixture.
4. Ordering: delay một confirm không được vô tình reorder frame vi phạm protocol;
   nếu phải gate cả connection hoặc channel, contract phải nêu blast radius.
5. Bounds: frame size, số channel/confirm in-flight, hold duration, heartbeat,
   backpressure, timeout, client disconnect và shutdown.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Tiến độ

P26 đã pin RabbitMQ 4.2.9 và `amqp091-go v1.15.0`; xem
[contract](rabbitmq-contract.md). P27 hoàn tất adapter, config/control/UI,
plaintext/TLS broker fixtures, multi-channel/heartbeat, selector/reload, retry,
TLS rejection, lifecycle và CLI/container runtime. Xem
[acceptance](rabbitmq-acceptance.md) cho bằng chứng P27-AC1–AC9 và giới hạn
kiểm chứng. Phase 10 Done ngày 2026-09-22 trong phạm vi contract đã pin.
