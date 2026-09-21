# Faultline

Faultline is a Go proxy for testing how applications handle network failures.
It sits between an application and its dependency, injects configurable faults,
and records what happened.

Faultline supports HTTP/1.1, HTTP/2 and unary gRPC, with optional TLS/mTLS,
file-based configuration, runtime controls, and one fault action per flow.

Start with the Vietnamese [user documentation](docs/README.md): quick start,
configuration, CLI operations, tester UI, and PostgreSQL/MySQL adapters.

**Status:** the HTTP/HTTPS MVP (phases 1–5) is complete: five fault actions, local
administration, JSON events, payment demo and binary/Docker delivery. See
[AC1–AC24 evidence](plans/05-mvp-delivery/acceptance.md),
[resource measurements](plans/05-mvp-delivery/benchmark-results.md) and the
[runnable lost-response demo](examples/http/paymentdemo/README.md).

**Phase 6 is complete:** HTTP/2, unary gRPC, optional mTLS, and request/response
truncate and throttle. See the [examples and protocol contract](examples/grpc/README.md)
and [15 acceptance criteria with verification results](plans/06-protocol-extensions/acceptance.md).

**Phase 7:** managed HTTPS API and embedded tester UI are implemented; browser
interaction acceptance is pending user testing. See the [tester setup and checklist](examples/tester/README.md)
and [verification record](plans/07-tester-experience/acceptance.md).

**Phase 9:** PostgreSQL explicit-transaction faults are implemented with SCRAM-SHA-256
and per-leg TLS. Delay, bounded hold and disconnect can intercept confirmed COMMIT
acknowledgments. See the [runnable PostgreSQL demo and UI checklist](examples/postgresql/README.md)
and [verification record](plans/09-semantic-adapters/acceptance.md). Manual UI acceptance is pending.

**MySQL (P25):** explicit-transaction COMMIT faults are implemented for MySQL8.4.8,
with caching_sha2_password and plaintext/plaintext or TLS/TLS connections.
See the [MySQL setup and retry demo](examples/mysql/README.md),
[protocol limits](plans/09-semantic-adapters/mysql-contract.md) and
[automated acceptance](plans/09-semantic-adapters/mysql-acceptance.md).

## Run locally

```sh
rtk proxy go build -o bin/faultline ./cmd/faultline
rtk proxy ./bin/faultline validate --config examples/http/faultline.yaml
rtk proxy ./bin/faultline serve --config examples/http/faultline.yaml
```

Run your upstream at `127.0.0.1:9000`, then send requests to `127.0.0.1:8080`.
The multi-file example also works with both commands. All files and listeners
are prepared before serving; listener readiness does not establish app or
upstream readiness. Ctrl+C/SIGTERM stops admission, drains requests for up to 5s,
then cancels remaining HTTP flows. PostgreSQL/MySQL sessions close immediately on shutdown.

Injection starts disabled, so configured rules do not affect startup traffic or
consume selector counters. `serve --start-enabled` enables configured fault
actions immediately. Otherwise, enable injection after the application is ready:

```sh
rtk proxy ./bin/faultline status
rtk proxy ./bin/faultline enable
rtk proxy ./bin/faultline reload --config examples/http/faultline.yaml
rtk proxy ./bin/faultline disable
```

## Runtime administration

Admin uses HTTP/JSON over a local Unix socket on macOS/Linux, separate from fault
traffic. The default is `/tmp/faultline-<uid>/admin.sock`. For multiple instances,
pass the same `--admin-socket PATH` to `serve` and each command targeting it.
The parent directory must be private (0700); missing directories are created and
the socket is 0600. Existing sockets are never overwritten. Normal shutdown
removes the socket; after an unclean exit, remove a stale socket only after
confirming its instance has stopped.

Runtime commands print JSON and support `--timeout` (default 5s). Failure exits
with code 1; success/help exits with 0. A timeout can leave the mutation outcome
unknown; check `status` before retrying. Commands do not retry automatically.
`reload` sends an absolute root path, and the serving process reads/validates the
complete config tree. Paths must exist in that process's filesystem. Finish all
file edits before reloading. Rules/seed can change; listener/upstream/TLS/runtime
changes require restart. Equivalent reloads and repeated toggles are no-ops.

`status` includes control identity/timestamps/sequence, listener readiness,
current-revision rule eligible/selected counters and run-wide recorder counters.
Recorder totals persist through reloads; rule counters reset for a new revision.
Counters can advance while status is sampled. `active_fault_flows` counts live
flows whose action has started, including old snapshots after disable/reload.
Disable changes new requests only; it does not end those flows.

In a container, run the admin CLI using `docker exec` and the container's config
paths. No admin TCP port needs publishing. See [Docker instructions](deploy/docker/README.md)
for the non-root image, mounted config/certs and local admin commands.

## Shared tester server

Use `serve --data-dir DIR --api-listen HOST:PORT --api-cert PEM --api-key PEM`
with `--config FILE` to enable managed mode. Create individual viewer/editor
accounts first with `faultline user`; passwords are read from stdin. The
[setup script and guide](examples/tester/README.md) prepare an isolated local
HTTP/gRPC fixture and describe HTTPS, accounts, Docker and manual UI tests.

The file bootstraps managed state only once. Later starts restore the last
committed config/revision and default injection to disabled. All rule edits use
the authenticated HTTPS API with a revision precondition; the Unix admin socket
is read-only in this mode. Change infrastructure while stopped with
`configure --data-dir DIR --config FILE`. Omit `--data-dir` for the existing
file/CLI workflow. UI assets are embedded in the binary; Node is not a runtime
dependency. The API also exposes bounded per-run outcome counts independently
of JSON event delivery.

## Events and counters

`serve` reserves stdout for newline-delimited JSON events. Readiness and errors
go to stderr, so events can be captured using normal stdout redirection.
Events cover flow start, decision, fault reached/applied, flow finish and control
operations. They contain run/flow/proxy/protocol, revision/state/sequence,
timestamps, rule/selector, phase/action and observed outcome. Raw errors, URLs,
headers and request/response/config bodies are excluded. Configured IDs remain
visible. Upstream status records an observation, not proof of business commit.

Outcomes distinguish pass-through, fault applied, not-reached selection,
upstream/TLS failure, proxy overload/timeout, client cancellation and shutdown.
`selected`, `reached`, `applied` and `not_reached` are separate fields. Use control
sequence/revision to correlate concurrent events; line order alone does not
establish when a control change became visible to another goroutine.

The queue holds at most `--event-buffer` events (default 1024), plus one event
being written. When full, it drops the new event and increments `dropped_events`.
Total/eligible/selected/applied/active counters are updated independently of
queue delivery. `write_errors`, `written_events` and `pending_events` describe
the sink. Nonzero dropped/write-error/pending counts can mean an incomplete
artifact. No historical revision or completed-flow map is retained.

Shutdown closes admin/listeners, drains flows for up to 5s, cancels the remainder,
queues a summary and flushes events for at most 2s. Missing events/write failures
are also reported on stderr. Allow at least 10s for Docker stop.
A blocked generic writer may retain one writer goroutine until process exit;
shutdown reports a flush timeout rather than waiting indefinitely. A failed or
partial sink write can damage the JSON stream; counters do not make it complete.
`serve` ignores SIGPIPE so a closed stdout consumer becomes a counted sink error
while the proxy and admin channel keep running.

## Available faults

| Action | Phase | Behavior |
| --- | --- | --- |
| `delay` | Before request or after final upstream headers | Wait `duration`, then continue; deadlines/cancellation can stop the wait. |
| `respond` | Before request | Return configured status/body without calling upstream. |
| `close_connection` | Before request or after final upstream headers | Cancel upstream and close the client connection; no guaranteed TCP RST. |
| `hold_request` | Before request | Withhold the request from upstream; hold the client until cancellation or `max_duration`, then close. |
| `hold_response` | After final upstream headers | Cancel/close upstream immediately; withhold the final response until cancellation or `max_duration`, then close. |

`respond` suppresses body bytes for HEAD and statuses 204/205/304. `hold_request`
reads and discards an incoming upload with bounded memory to detect disconnects.
Delay does not drain bodies into memory; it applies backpressure. During an unread
upload, client disconnect detection can wait until body I/O resumes; the flow
deadline and server shutdown still bound the wait.

If the client timeout is shorter than the hold duration, the client sees a timeout;
otherwise it sees the connection close. A selected after-headers fault cannot run
if upstream fails before those headers arrive. Canceling upstream does not prove
its business work was rolled back. An applied delay/hold means the wait started,
even if its final report records cancellation before the configured duration.

## HTTP behavior

- `protocol: http1|http2|grpc` selects the listener; `upstream_protocol` selects
  the upstream HTTP version. gRPC requires HTTP/2. TLS uses system trust plus any
  configured CA, with hostname verification. TLS/mTLS on each leg is independent.
- Bodies stream with bounded copy buffers. Method, escaped path, raw query,
  body, status, repeated headers and trailers are forwarded using Go reverse
  proxy semantics; Host becomes the configured upstream authority. Hop-by-hop
  headers are handled by `httputil.ReverseProxy`; client forwarding headers are
  removed. Automatic compression and environment forward proxies are disabled.
- Each upstream request uses a fresh connection to prevent transport retries.
  This increases TCP/TLS connection cost; downstream keep-alive remains supported.
- The inflight limit is shared across listeners; excess HTTP requests receive 503
  and gRPC calls receive RESOURCE_EXHAUSTED.
  Request deadlines cover hooks and body I/O; HTTP/2 uses stream deadlines. Header reads, idle connections and TLS handshakes are also bounded
  using `request_timeout`. An incomplete upload is closed after an early response.
- Natural upstream failures return 502 before final headers; failures during a
  streamed body abort the response. CONNECT, upgrades/WebSocket, forward-proxy
  requests and HTTP/1.0 are rejected. gRPC streaming remains outside the verified
  scope; all flows are bounded by request_timeout.

The adapter exposes before-request and after-final-headers hooks; informational
1xx responses do not trigger the latter. Each request pins one control snapshot.
The recorder receives lifecycle events; the optional in-process observer also
receives the final report, including rejected overload/unsupported requests.

Multi-file configuration with a root file and `include` is implemented in
[P01a](plans/01-core/04-multi-file-config.md). See the
[multi-file example](examples/http/multi-file/faultline.yaml) and specification
section 7.1. Single-file configurations remain supported.

## Runtime defaults

| Limit | Default | Override before startup |
| --- | --- | --- |
| Inflight requests, shared by listeners | 1000 | `runtime.max_inflight_requests` |
| Flow/header/read/write/idle timeouts | 30s | `runtime.request_timeout` |
| Queued JSON events, plus one in the writer | 1024 | `serve --event-buffer N` |
| CLI admin timeout | 5s | `--timeout DURATION` |
| CLI shutdown drain / recorder flush | 5s / 2s | Fixed in this MVP |

These are limits, not measured capacity. Inflight does not cap all TCP connections;
the proxy targets dev/test workloads. The [action/selector examples](examples/http/faults.yaml)
cover all five actions plus probability, nth and every selection.

## Project layout

```text
faultline/
├── cmd/
│   └── faultline/          # CLI entry point and application wiring
├── internal/
│   ├── config/             # Configuration schema, parsing, and validation
│   ├── control/            # Snapshots and local admin socket/client
│   ├── engine/             # Protocol-independent matching and fault selection
│   ├── fault/              # Fault actions and execution
│   ├── proxy/
│   │   ├── http/           # HTTP/1.1–HTTP/2 forwarding, TLS, stream lifecycle
│   │   └── grpc/           # gRPC metadata, status and deadline semantics
│   └── recorder/           # Structured events, counters, and flow timelines
├── examples/
│   └── http/               # Configurations and runnable payment demo
├── tests/
│   └── integration/        # Tests across the proxy, client, and upstream
├── deploy/
│   └── docker/             # Docker packaging and demo deployment files
├── plans/                  # Phases, feature plans and verification results
├── specific.md
├── go.mod
└── README.md
```

Remove a `.gitkeep` placeholder when its directory gains real files.

## Component boundaries

- `cmd/faultline` parses CLI arguments and wires components together.
- `config` parses and validates configuration into an immutable document.
  `control` manages snapshots of revisions and injection state.
- `engine` matches metadata and selects one action, with synchronized counters
  and seeded randomness per rule; it performs no protocol I/O.
- `proxy/http` owns HTTP connections and streaming, exposing the lifecycle
  points and capabilities needed to execute a fault.
- `fault` implements actions using those capabilities, honoring cancellation
  and deadlines.
- `recorder` records decisions and outcomes through a bounded queue, retaining
  run counters independently of event delivery.

Unit tests live beside the Go files they test. Tests that exercise multiple
components live in `tests/integration`. Future protocol adapters will be
added when their implementation begins.

## Go module

The initial module path is `faultline`, using the locally installed Go 1.26.4
toolchain. Replace the module path with the repository's canonical path when
that address is established, updating imports at the same time.

The YAML dependency is pinned in `go.mod` and `go.sum`; the CLI uses the standard
library flag parser and HTTP server/transport.

## Development checks

```sh
rtk proxy go build ./...
rtk proxy go test ./...
rtk proxy go test -race ./...
rtk proxy go vet ./...
```

These checks cover core packages, CLI behavior, fault execution and real HTTP/TLS
integration and the payment demo. See [MVP acceptance](plans/05-mvp-delivery/acceptance.md).
If the workspace sandbox blocks
the default Go build cache, prefix the Go invocation with
`env GOCACHE=/private/tmp/faultline-go-build` after `rtk proxy`.
Integration tests require permission to bind localhost TCP ports.

The optional container smoke test needs a running Docker daemon. It builds a
temporary image from the delivery Dockerfile and checks mounted root/includes/TLS
files, admin commands and the payment demo, then removes its own containers/image:

```sh
rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestContainerRuntime$' -timeout 360s
```

See [configuration defaults and example](examples/http/README.md) and
[phase 1 implementation decisions](plans/01-core/README.md#quyết-định-hiện-thực).

## Design documents

- [Current specification and agreed MVP scope](specific.md)
- [Implementation phases and feature plans](plans/README.md)
