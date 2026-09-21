# RabbitMQ adapter contract — P26

## Pinned baseline

- RabbitMQ **4.2.9**, image
  `rabbitmq:4.2.9-management@sha256:e002010fddc214d38f7c8acf058a2fb62350daa10316431cd2cf959f42761e34`.
- AMQP 0-9-1 and `github.com/rabbitmq/amqp091-go v1.15.0` fixture client.
- Direct standalone broker, default `/` vhost and a non-exclusive temporary
  classic queue. The fixture uses `guest` only inside its disposable Docker
  container; credentials are never emitted by Faultline or test traces.

## Confirm and connection contract

- The adapter forwards the protocol header, `connection.start/start-ok/tune/
  tune-ok/open/open-ok`, channel methods, heartbeats and content frames. The
  initial slice requires the default PLAIN start flow; SASL challenge/response
  variants are rejected rather than downgraded or guessed.
- A flow starts only when a channel with confirmed `confirm.select-ok` has sent
  a complete `basic.publish` content sequence. Metadata is exact `exchange` and
  `routing_key` from that method only. Message header/body, credentials and
  properties are never parsed into matching or recorder output.
- Publisher sequence is per AMQP channel. A `basic.ack` is successful only for
  matching pending sequences; `multiple=true` selects all pending sequences at
  or below the delivery tag (tag zero means all). `basic.nack`, unknown tags,
  returns and connection/channel closure never count as successful confirm.
- Selector decision is pinned at publish. Phase `after_publish_confirm` is
  reached only when RabbitMQ sends matching successful `basic.ack` upstream.
  A `basic.ack` from a consumer is unrelated and has no semantic phase.
- A `basic.return` is associated with the oldest pending publish on its channel
  with the same exchange and routing key; it is recorded as `publish_returned`
  and is never eligible for a confirm fault. AMQP returns carry no publisher
  delivery tag, so concurrent same-metadata publishes use this explicit FIFO
  correlation policy.
- A delayed `multiple` confirm is gated as one server-to-client frame. This
  preserves AMQP frame order but can delay unrelated confirms/heartbeats on the
  same connection. `hold_response` and `close_connection` close the full client
  connection; all in-flight channels have that blast radius. If more than one
  selected publish shares that `multiple` frame, the lowest pending sequence
  selects the frame action; the other selected publishes record the common
  outcome without applying a second fault.

## Transport and bounds

- Verified matrix: plaintext/plaintext and TLS/TLS, TLS 1.2 or later, server
  certificate/CA/hostname verification on the upstream leg. Mixed legs are
  allowed only if independently configured; current automated evidence covers
  both legs TLS together. RabbitMQ mTLS is rejected by config in this slice.
- Each AMQP frame body is bounded to 1 MiB. A session accepts at most
  `runtime.max_inflight_requests` unconfirmed publishes per channel. Malformed
  frame/end marker, invalid publish encoding, unexpected confirm-select-ok and
  I/O failure close the session without echoing traffic.
- Handshake and fault waits are bounded by `runtime.request_timeout`; client
  disconnect, broker close, channel close and shutdown close or clear the
  affected state and finish pending flows. Faultline never reconnects or retries
  a publish.

## Evidence

`TestRabbitMQReal` ran against the pinned broker with race detection: baseline,
delay, hold and close after confirm; direct broker observation verified that the
message existed for lost confirms. `TestRabbitMQTLSReal` verified TLS termination
on both legs and a lost confirm. The concrete commands/results are recorded in
[acceptance](rabbitmq-acceptance.md).

## Sources

- [RabbitMQ publisher confirms](https://www.rabbitmq.com/docs/confirms)
- [RabbitMQ TLS support](https://www.rabbitmq.com/docs/ssl)
- [AMQP 0-9-1 specification](https://www.rabbitmq.com/resources/specs/amqp0-9-1.pdf)
