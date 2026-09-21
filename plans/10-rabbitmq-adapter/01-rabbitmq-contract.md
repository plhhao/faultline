# P26 — RabbitMQ AMQP 0-9-1 contract

- Trạng thái: **Done — xem [contract](rabbitmq-contract.md) và [acceptance](rabbitmq-acceptance.md)**.
- Phụ thuộc: nền config/control/recorder hiện có; không phụ thuộc Phase 8.
- Nguồn: [phạm vi Phase 10](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi khi thực hiện: contract/fixtures trước; sau khi contract được
  chấp thuận, P27 mới được thay đổi `internal/proxy/`, config, API/UI, examples
  và integration tests.

## DEFINE

Chốt protocol contract trước khi viết adapter. Một connection AMQP dài hạn có
nhiều channel; publisher confirm có sequence number theo channel và một confirm
có thể acknowledge nhiều publish. Vì vậy không được chọn fault theo connection,
frame bất kỳ hoặc chỉ vì thấy `basic.ack`.

P26 không hiện thực proxy. Kết quả bắt buộc là contract có version và trace thật
làm đầu vào cho P27.

## PLAN → BUILD

1. Pin RabbitMQ image/version, Go AMQP client/version, direct-node topology,
   exchange/queue durability và persistence policy. Ghi digest image và lệnh
   fixture; không dùng RabbitMQ cài đặt của người dùng làm evidence.
2. Trace handshake, AMQP frame boundaries, channel open/close, heartbeat,
   confirm.select, `basic.publish`, content header/body, `basic.ack`,
   `basic.nack`, return và consumer delivery. Trace chỉ giữ loại frame, channel,
   sequence/correlation và kích thước; tuyệt đối không lưu credential hoặc
   payload.
3. Định nghĩa lifecycle/metadata:
   - Một publish là flow bắt đầu từ method + content header/body hoàn chỉnh trên
     một channel; snapshot và selector được pin đúng một lần.
   - `after_publish_confirm` chỉ reached khi upstream `basic.ack` xác nhận đúng
     publish sequence. Xử lý `multiple`, nack, return, channel/connection close
     và publish không confirm phải được định nghĩa riêng.
   - Consumer delivery là flow riêng; `basic.ack` do consumer gửi không là bằng
     chứng broker đã xác nhận durable. Không đặt phase "after consumer ack"
     nếu protocol không có evidence tương ứng.
   - Matcher chỉ giữ metadata xuất hiện trực tiếp trong frame; không parse hoặc
     log body, không suy ra queue routing từ topology.
4. Chốt capability matrix action × phase. Mặc định chỉ đề xuất
   `delay`/`hold_response`/`close_connection` ở `after_publish_confirm`; các
   fault trước publish hoặc trước consumer delivery chỉ thêm khi trace chứng minh
   ordering, cancellation và blast radius rõ.
5. Chốt TLS/auth matrix cho client → Faultline và Faultline → RabbitMQ. Adapter
   chỉ parse được AMQP khi terminate TLS ở leg tương ứng. Xác minh CA/hostname
   upstream, không fallback plaintext; mTLS chỉ có khi fixture hai phía pass.
   Credentials trong AMQP handshake được forward có kiểm soát và không ghi log.
6. Chốt concurrency/ordering. Bảng pending confirm phải key theo channel và
   publisher sequence; policy với `multiple` confirm, concurrent channels,
   delayed confirmation, heartbeat và các frame không liên quan phải không làm
   sai protocol. Nếu delay/hold buộc chặn thêm frame, ghi rõ phạm vi tác động.
7. Chốt resource/lifecycle bounds: negotiated frame size, số channel và confirm
   in-flight, body/frame allocation, request/idle deadline, hold limit,
   backpressure, reconnect, client disconnect, malformed frames và shutdown.
   Proxy không tự reconnect hay retry publish.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| P26-AC1 | Contract pin broker/client/image digest, topology fixture và phiên bản AMQP; trace không chứa credential/payload. |
| P26-AC2 | Capability matrix phân biệt publisher confirm, consumer acknowledgement, return/nack và channel/connection close; không có false claim về durable outcome. |
| P26-AC3 | Trace nhiều publish/channel gồm `multiple` confirm chứng minh key/correlation và ordering policy; heartbeat không bị nhận nhầm là operation. |
| P26-AC4 | Matrix TLS/auth plaintext/TLS/mTLS từng leg, CA/hostname và các trường hợp từ chối được chốt; không có plaintext downgrade. |
| P26-AC5 | Frame/session/hold/deadline bounds cùng policy malformed frame, EOF, reconnect và shutdown có thể kiểm thử ở P27. |

Ghi contract hoàn chỉnh tại `rabbitmq-contract.md` khi P26 được thực hiện, gồm
lệnh, trace summary và giới hạn thật. Không chuyển P26 sang Done chỉ vì đã đọc
specification.

## REVIEW

Đối chiếu với AMQP 0-9-1 và tài liệu RabbitMQ chính thức. Review đặc biệt:
publisher confirm là semantic của channel, không phải TCP acknowledgement;
`basic.ack` từ consumer không cùng nghĩa với publisher confirm; TLS termination
không được làm lộ credential hoặc hạ bảo mật; policy delayed frame không được
reorder dữ liệu trái contract.

## Sources to consult

- [RabbitMQ publisher confirms](https://www.rabbitmq.com/docs/confirms)
- [RabbitMQ AMQP 0-9-1 client connections](https://www.rabbitmq.com/docs/networking)
- [RabbitMQ TLS support](https://www.rabbitmq.com/docs/ssl)
- [AMQP 0-9-1 specification](https://www.rabbitmq.com/resources/specs/amqp0-9-1.pdf)

## BUILD / VERIFY — 2026-09-21

P26 pinned the broker/client, recorded the frame and publisher-confirm contract,
and added parser/config tests plus real RabbitMQ plaintext and TLS/TLS traces.
The contract deliberately rejects RabbitMQ mTLS in this slice. P26-AC1–AC5
pass; P27 owns implementation/regression completion.
