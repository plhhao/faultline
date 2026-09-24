# BullMQ acceptance — P29

Verified 2026-09-23 on macOS arm64 (Go 1.26.4, Node 24.18.0) with
Docker/OrbStack Linux arm64, against the pins in
[bullmq-contract.md](bullmq-contract.md): BullMQ 5.81.5, ioredis 5.8.2,
Redis 7.4.9 (`redis:7.4.9-alpine@sha256:6ab0b6e7…61ab99`).

## Commands

2026-09-24 follow-up: fixed prompt fault cancellation when Redis closes after
queued replies, rejected noncanonical RESP length headers before selector
accounting, and waited for an actual PING reply on Docker-published Redis ports.
`TestUpstreamCloseCancelsFaultBeforeDeadline`, `TestFinalRepliesDeliveredBeforeUpstreamEOF`,
`TestReplyReadAheadBound`, `TestMalformedScriptDoesNotConsumeSelector`, and
`TestRESPBoundsAndRoundTrip` cover the regressions. The changed tests passed
20 repeated race runs; `go test -race -count=1 ./...`, BullMQ Redis integration
with race detection, BullMQ container runtime, `go vet ./...`, and `go build ./...`
passed. Reply read-ahead is capped at 1 MiB of queued wire bytes.

| Command | Result |
| --- | --- |
| `rtk proxy go vet ./...` | PASS |
| `rtk proxy go build -o bin/faultline ./cmd/faultline` | PASS |
| `rtk proxy go test -race -count=1 ./...` | PASS, all packages |
| `rtk proxy env FAULTLINE_BULLMQ_TEST=1 FAULTLINE_RABBITMQ_TEST=1 go test -race -count=1 ./tests/integration -run '^(TestBullMQ\|TestRabbitMQ)' -timeout 500s` | PASS |
| `rtk proxy env FAULTLINE_MYSQL_TEST=1 go test -race -count=1 ./tests/integration -run '^TestMySQL' -timeout 560s` | PASS |
| `rtk proxy env FAULTLINE_POSTGRES_TEST=1 go test -race -count=1 ./tests/integration -run '^TestPostgreSQLReal$' -timeout 560s` | PASS |
| `rtk proxy env FAULTLINE_DOCKER_TEST=1 FAULTLINE_BULLMQ_TEST=1 go test -count=1 ./tests/integration -run '^TestBullMQContainerRuntime$'` | PASS, non-root image |
| Same with `FAULTLINE_DOCKER_TEST=1`, `^TestContainerRuntime$` and `^TestRabbitMQContainerRuntime$` run one at a time | PASS |
| `rtk proxy node --test internal/control/remote/app_test.cjs internal/control/remote/diff_test.cjs` | PASS, 14 tests |
| `rtk proxy go run ./cmd/faultline validate --config examples/bullmq/config.yaml` | PASS |

When the three container tests ran in one `go test` invocation, the existing
HTTP `TestContainerRuntime` failed once with `connection refused` on the
published port after `status` reported ready. It contains no BullMQ proxy;
three isolated reruns and the sequential final run passed. The Docker port
forward readiness race is recorded, not fixed.

## Criteria

| ID | Evidence | Result |
| --- | --- | --- |
| P29-AC1 | `TestBullMQReal/pass-through`: fresh Redis, so the first add is the cold `EVAL` path on the Queue's persistent connection (handshake, `HSET`, script). Direct Redis observation finds the job. No Worker is used; the contract does not claim processing. | PASS |
| P29-AC2 | `queue-mismatch` (integration), `TestRuleOrderAndRawCommands` (disabled rule skipped, non-matching rule skipped, probability 0 owns its queue, `every: 2`, raw `PING` creates no flow), `TestSelectorsCacheMissAndReload` (`nth`, reload), `TestReplySemantics` (probability 1), `TestBullMQTransportPassThrough` (raw Redis traffic has zero flows). | PASS |
| P29-AC3 | `TestReplySemantics`: only a nonempty bulk string reaches the phase; `:-5`, `NOSCRIPT`, `ERR`, null, empty bulk and `+OK` finish `not_reached`. `TestUpstreamReplyTimeout`: a missing reply ends within `request_timeout`, `not_reached`. `queueName` rejects other hashes, key counts, key layouts and unsafe queue names. | PASS |
| P29-AC4 | `delay` returns after ≥280ms for 300ms; `hold` fails the caller after ≥280ms; `close` fails the caller in ~24ms. In each case the direct Redis connection finds the job; the proxy sent no command of its own. | PASS |
| P29-AC5 | Retry cases use a new ioredis connection. `TestFIFOAndCancellation`: pipelined replies stay in order behind a delayed reply; client close, upstream close and shutdown end a hold. `TestBullMQTransportPassThrough`: pipelined `SET`/`GET`/`PING` answered in order while a second connection blocks in `BLPOP`; connections are independent. `MULTI` and `HELLO` close without a reply. Counters return to 0. | PASS |
| P29-AC6 | `close-generated-retry`: first add fails, retry succeeds, two waiting jobs. `close-known-retry` (`jobId: known-job`): retry succeeds, one waiting job and its key. Outcomes are recorded, not judged. | PASS |
| P29-AC7 | `TestBullMQTLSReal`: TLS on both legs, Redis ACL user, `default` user disabled; close + retry leaves two jobs, events never contain the password. `TestBullMQUpstreamTLSVerification`: certificate without the upstream IP fails the upstream handshake, the add fails, no job and no flow. `TestBullMQValidation`: mixed legs, mTLS, URL credentials and database paths rejected. | PASS |
| P29-AC8 | `TestRESPBoundsAndRoundTrip` + `FuzzRESP` seeds: bad terminators, negative/oversized lengths, >4096 children, depth >8, >8192 nodes, >1 MiB frames and RESP3 push rejected. Integration: garbage and a 99,999,999-byte bulk header close the connection. Unit/integration `settle` checks `Active`/`ActiveFaults` return to 0 after each case. | PASS |
| P29-AC9 | Full race regression, vet, build, UI tests, BullMQ/RabbitMQ/MySQL/PostgreSQL fixtures, container runtimes and the example config above; local documentation links checked. | PASS |
| P29-AC10 | Duplicate and new IDs both return a job ID bulk string and both reach `after_job_add`; events carry outcome `job_add_*` and never claim a new job. The known-ID retry evidence shows the duplicate path. | PASS |
| P29-AC11 | `TestSelectorsCacheMissAndReload`: `NOSCRIPT` is eligible attempt 1 (`not_reached`) and the fallback is attempt 2, selected by `nth: 2`. Disable or reload while a reply is pending keeps the captured decision and revision 1. | PASS |
| P29-AC12 | Fixture policy: no ioredis resend or reconnect, one explicit application retry on a fresh connection, `nth: 1`, 2s command timeout, 15s process bound; observation runs before cleanup. | PASS |

## Limitations

- Idle client connections close after `runtime.request_timeout` (same as the
  database adapters); ioredis reconnects. Blocking commands must also finish
  within it. A long delay or hold can exhaust later replies' deadlines on the
  same connection.
- Recognition is limited to the pinned `addStandardJob` script. Delayed,
  prioritized, parent/flow jobs, `addBulk`, other BullMQ versions, Valkey,
  Cluster/Sentinel, transactions, RESP3, Pub/Sub, Workers and mTLS are
  unsupported. The wire cannot distinguish `Queue.add` from another caller
  issuing the identical script.
- Evidence is from one host and fixture; no long-running load qualification.
- Four pre-existing files are not `gofmt`-clean (`cmd/faultline/user.go`,
  `internal/proxy/mysql/session_test.go`, `tests/integration/mysql_test.go`,
  `tests/integration/postgresql_test.go`); they were not touched.

## Review

Reviewed the RESP bounds, pinned-script recognition, FIFO correlation, snapshot
pinning, per-connection and global pending limits, cancellation paths, TLS
verification and event secrecy. Redis I/O stays in `internal/proxy/bullmq`; the
engine gained only a `Queue` matcher field. No blocking finding remains.
