# RabbitMQ acceptance — P26/P27

Environment: macOS arm64, Go 1.26.4, Docker/OrbStack, RabbitMQ 4.2.9 image
digest and AMQP client pinned in [the contract](rabbitmq-contract.md).

## Automated evidence — 2026-09-21

```sh
FAULTLINE_RABBITMQ_TEST=1 go test -race ./tests/integration \
  -run '^TestRabbitMQ(ReturnDoesNotInject|TLSReal|Real)$' -count=1 -timeout 240s
```

**PASS.** `TestRabbitMQReal` starts a fresh broker on a dynamic localhost port
and verifies baseline publisher confirms plus `delay`, `hold_response` and
`close_connection` after confirm. For hold/close, an independent direct AMQP
connection reads the persisted message after the publisher loses its confirm.
The baseline also verifies `basic.consume`, delivery and consumer `basic.ack`
forwarding.
`TestRabbitMQTLSReal` starts a TLS-only broker, terminates TLS on both Faultline
legs, verifies trust, and observes the same lost-confirm outcome. Fixture
containers are removed at test cleanup. `TestRabbitMQReturnDoesNotInject`
verifies that an unroutable mandatory publish receives both return and ack
without a `close_connection` fault.

```sh
go test ./internal/config ./internal/engine ./internal/control/remote \
  ./internal/proxy/rabbitmq
node --test internal/control/remote/app_test.cjs
```

**PASS.** Covers RabbitMQ schema/default port/capability validation, frame and
confirm parsing, match metadata, managed API capabilities, and RabbitMQ-specific
editor fields/phase.

```sh
go test -race ./...
go vet ./...
go build ./...
docker build -f deploy/docker/Dockerfile -t faultline:local .
```

**PASS.** Full Go race regression, static checks, binary build, and Docker
build passed. The Node editor/diff suite (13 tests) also passed.

## Remaining P27 checks

- The current evidence covers the single-channel confirmed-publish path,
  consumer forwarding, and TLS/TLS server-auth path. P27-AC1–AC3 and AC7 still
  need their explicit multi-channel, heartbeat, `multiple`/nack and
  certificate-rejection cases. Unit coverage now includes return parsing,
  channel-state reset, complete-content flow creation and multiple-confirm
  telemetry.
- P27-AC2 still needs a persistent-connection integration fixture for
  selectors, disable/reload and snapshot behavior. P27-AC5 still needs an
  application reconnect/retry example that records duplicate or idempotent
  outcome.
- P27-AC6 needs real-broker concurrent-channel and delayed-frame ordering
  evidence. P27-AC8 needs its complete disconnect/broker-close/shutdown bound
  matrix. These gates remain required before P27 or Phase 10 is Done.
