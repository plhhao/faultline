# Faultline

Faultline is a Go proxy for testing how applications handle network failures.
It sits between an application and its dependency, injects configurable faults,
and records what happened.

The first milestone targets HTTP/1.1 over HTTP and HTTPS, with file-based
configuration, runtime injection controls, and one fault action per request.

**Status:** phases 1–3 are implemented: strict YAML configuration, runtime
snapshots, rule selection, CLI validate/serve, and HTTP/HTTPS forwarding with
lifecycle hooks, and all five MVP fault actions. Runtime administration and the
recorder (phase 4) are pending.

## Run locally

```sh
rtk proxy go build -o bin/faultline ./cmd/faultline
rtk proxy ./bin/faultline validate --config examples/http/faultline.yaml
rtk proxy ./bin/faultline serve --config examples/http/faultline.yaml
```

Run your upstream at `127.0.0.1:9000`, then send requests to `127.0.0.1:8080`.
The multi-file example also works with both commands. All files and listeners
are prepared before serving; listener readiness does not establish app or
upstream readiness. Ctrl+C/SIGTERM cancels active flows and closes listeners.

Injection starts disabled, so configured rules do not affect startup traffic or
consume selector counters. `serve --start-enabled` enables configured fault
actions immediately. Only `validate`
and `serve` are available; enable/disable/reload/status commands come in phase 4.

```sh
rtk proxy ./bin/faultline serve --config examples/http/faultline.yaml --start-enabled
```

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

- HTTP/1.1 is enforced on both sides; TLS uses system trust plus any configured
  CA, with hostname verification. Client-facing and upstream TLS are independent.
- Bodies stream with bounded copy buffers. Method, escaped path, raw query,
  body, status, repeated headers and trailers are forwarded using Go reverse
  proxy semantics; Host becomes the configured upstream authority. Hop-by-hop
  headers are handled by `httputil.ReverseProxy`; client forwarding headers are
  removed. Automatic compression and environment forward proxies are disabled.
- Each upstream request uses a fresh connection to prevent transport retries.
  This increases TCP/TLS connection cost; downstream keep-alive remains supported.
- The inflight limit is shared across listeners; excess requests receive 503.
  Request deadlines cover hooks and body I/O, and terminate connections when
  necessary. Header reads, idle connections and TLS handshakes are also bounded
  using `request_timeout`. An incomplete upload is closed after an early response.
- Natural upstream failures return 502 before final headers; failures during a
  streamed body abort the response. CONNECT, upgrades/WebSocket, forward-proxy
  requests and HTTP/1.0 are rejected. HTTP/2, mTLS and unbounded streams are outside
  this milestone.

The adapter exposes before-request and after-final-headers hooks; informational
1xx responses do not trigger the latter. Each request pins one control snapshot.
An optional in-process observer receives selection, reach/application state and
outcome; persistent events and aggregate observability are deferred to phase 4.

Multi-file configuration with a root file and `include` is implemented in
[P01a](plans/01-core/04-multi-file-config.md). See the
[multi-file example](examples/http/multi-file/faultline.yaml) and specification
section 7.1. Single-file configurations remain supported.

## Project layout

```text
faultline/
├── cmd/
│   └── faultline/          # CLI entry point and application wiring
├── internal/
│   ├── config/             # Configuration schema, parsing, and validation
│   ├── control/            # Active snapshots, reload, enable/disable, status
│   ├── engine/             # Protocol-independent matching and fault selection
│   ├── fault/              # Fault actions and execution
│   ├── proxy/
│   │   └── http/           # HTTP/1.1 forwarding, TLS, and lifecycle hooks
│   └── recorder/           # Structured events, counters, and flow timelines
├── examples/
│   └── http/               # Configuration example; runnable demos come later
├── tests/
│   └── integration/        # Tests across the proxy, client, and upstream
├── deploy/
│   └── docker/             # Docker packaging and demo deployment files
├── plans/                  # Phases, feature plans and verification results
├── specific.md
├── go.mod
└── README.md
```

Empty directories contain `.gitkeep` files so they can be tracked when the
project is added to Git. Remove each placeholder when its directory gains files.

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
- `recorder` will record decisions and outcomes with bounded resource use.

Unit tests will live beside the Go files they test. Tests that exercise multiple
components will live in `tests/integration`. Future protocol adapters will be
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
integration. Full MVP acceptance and resource benchmarks remain in phase 5. If the workspace sandbox blocks
the default Go build cache, prefix the Go invocation with
`env GOCACHE=/private/tmp/faultline-go-build` after `rtk proxy`.
Integration tests require permission to bind localhost TCP ports.

See [configuration defaults and example](examples/http/README.md) and
[phase 1 implementation decisions](plans/01-core/README.md#quyết-định-hiện-thực).

## Design documents

- [Current specification and agreed MVP scope](specific.md)
- [Implementation phases and feature plans](plans/README.md)
