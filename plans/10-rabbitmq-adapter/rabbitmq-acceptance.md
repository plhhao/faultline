# RabbitMQ acceptance — P26/P27

Environment: macOS arm64, Go 1.26.4, Docker/OrbStack. RabbitMQ 4.2.9 image
digest and amqp091-go v1.15.0 are pinned in [the contract](rabbitmq-contract.md).

## Evidence — 2026-09-22

| Criterion | Evidence | Result |
| --- | --- | --- |
| P27-AC1 | `TestRabbitMQReal`, `TestRabbitMQPersistentChannelsAndRetry`: PLAIN handshake, publisher confirms, consume/deliver/ack, consumer nack/requeue, heartbeat intervals, two channels and channel close/reopen. | PASS |
| P27-AC2 | `TestRabbitMQSelectorsReload`: probability 0/1, nth/every, disabled and shadowed rules, exchange mismatch, routing-key match and reload on the same connection. `TestPublishSnapshotBeforeConfirm` pins a decision before ACK; `TestRabbitMQConcurrentSnapshot` verifies reload/disable during a delay and a new publish on another channel. | PASS |
| P27-AC3 | Frame/confirm parsing, `TestMultipleConfirmOrdering`, `TestMultipleConfirmMarksAllSelectedPublishesReached`, `TestConfirmTagBoundaries`, `TestNackAndChannelCorrelation`; real broker `TestRabbitMQPublisherNack` uses queue overflow with reject-publish, and `TestRabbitMQReturnDoesNotInject` tests mandatory return. Partial-content and channel-reset tests cover flow boundaries. | PASS |
| P27-AC4 | `TestRabbitMQReal`, `TestRabbitMQTLSReal`: delay timing; hold/close lose confirm while a direct independent client observes the message. | PASS |
| P27-AC5 | `TestRabbitMQPersistentChannelsAndRetry`: application reconnects and retries; independent reads observe exactly two messages, original plus retry. The proxy does not retry. The example documents this duplicate outcome. | PASS |
| P27-AC6 | `TestRabbitMQConcurrentSnapshot` exercises overlapping channels during a delay. `TestDelayedFramesPreserveConnectionOrder` feeds ACK, heartbeat and another channel's ACK through the session, verifies delay and exact order. Multiple-confirm tests verify one action per frame. | PASS |
| P27-AC7 | Real plaintext and TLS/TLS fixtures; `TestRabbitMQTLSRejections` rejects an untrusted upstream CA, wrong upstream hostname and untrusted listener certificate. Config rejects mTLS. `TestRabbitMQRecorderSecrecy` rejects bad credentials and verifies no password/body/header values in recorder output. | PASS |
| P27-AC8 | `lifecycle_test.go`: upstream TLS and AMQP handshake deadlines, released connection slot, frame/pending bounds, confirm-write failure, cancellation of hold/delay on disconnect, broker EOF and shutdown cleanup. Existing malformed-frame and channel-reset tests supplement these; race detection and active counters verify the exercised cleanup paths. | PASS |
| P27-AC9 | Full Go race regression, vet/build, 13 Node UI tests, CLI process startup test and non-root Docker runtime fixture. The Docker test builds the delivery image, enables injection via CLI, observes the lost-confirm message independently and verifies exit code 0. Documentation links and example validation checked. | PASS |

Commands run:

```sh
rtk proxy env FAULTLINE_RABBITMQ_TEST=1 go test -race ./tests/integration \
  -run '^TestRabbitMQ' -count=1 -timeout 240s
rtk proxy env FAULTLINE_DOCKER_TEST=1 FAULTLINE_RABBITMQ_TEST=1 \
  go test -race ./tests/integration -run '^TestRabbitMQContainerRuntime$' \
  -count=1 -timeout 240s
rtk proxy go test -race ./internal/proxy/rabbitmq -count=1
rtk proxy go test -race ./...
rtk proxy go vet ./...
rtk proxy go build ./...
rtk proxy node --test internal/control/remote/app_test.cjs internal/control/remote/diff_test.cjs
rtk proxy go run ./cmd/faultline validate --config examples/rabbitmq/config.yaml
rtk proxy git diff --check
```

The final targeted container rerun passed after fixing HTTP startup to bind only
HTTP/HTTP2/gRPC listeners. Previously HTTP startup also bound RabbitMQ's port,
which adapter-only fixtures did not exercise. `TestProcessRabbitMQListener`
now catches this through the CLI in the standard regression suite.

One full-regression attempt timed out in the existing HTTP/2 request-truncate
case (`TestBodyFaultMatrix/http2/request/truncate/mode0`). Three isolated reruns
and the subsequent complete race suite passed; no HTTP truncate implementation
was changed. This intermittent test observation is retained rather than counted
as a RabbitMQ failure or omitted from the verification record.

## Evidence limits

- Exact `multiple` ACK grouping, tag-zero boundaries and frame ordering use
  deterministic protocol tests; the broker is not required to batch ACKs on
  every run. Publisher nack has a separate real-broker overflow fixture.
- Cleanup assertions concern exercised sessions, joined goroutines and counters;
  these tests are not a long-running load or memory benchmark.
- TLS/TLS and plaintext/plaintext are the verified broker matrix. Mixed TLS
  legs remain configurable but are not claimed as a broker acceptance result.
- AMQP 1.0, Streams, clusters, mTLS, consumer fault injection and application
  idempotency remain outside scope. Return correlation uses the documented
  same-metadata FIFO policy; it is not a unique message identifier.

P27-AC1–AC9 are complete for the pinned P26 contract.
