# Faultline

Faultline is a Go proxy for testing how applications handle network failures.
It sits between an application and its dependency, injects configurable faults,
and records what happened.

The first milestone targets HTTP/1.1 over HTTP and HTTPS, with file-based
configuration, runtime injection controls, and one fault action per request.

**Status:** phase 1 core is implemented: strict YAML configuration, runtime
snapshots and rule selection. The CLI, HTTP proxy, fault execution and recorder
are not implemented yet.

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

- `cmd/faultline` will parse CLI arguments and wire components together.
- `config` parses and validates configuration into an immutable document.
  `control` manages snapshots of revisions and injection state.
- `engine` matches metadata and selects one action, with synchronized counters
  and seeded randomness per rule; it performs no protocol I/O.
- `proxy/http` will own HTTP connections and streaming, exposing the lifecycle
  points and capabilities needed to execute a fault.
- `fault` will implement actions using those capabilities, honoring cancellation
  and deadlines.
- `recorder` will record decisions and outcomes with bounded resource use.

Unit tests will live beside the Go files they test. Tests that exercise multiple
components will live in `tests/integration`. Future protocol adapters will be
added when their implementation begins.

## Go module

The initial module path is `faultline`, using the locally installed Go 1.26.4
toolchain. Replace the module path with the repository's canonical path when
that address is established, updating imports at the same time.

The YAML dependency is pinned in `go.mod` and `go.sum`. There is no runnable CLI yet.

## Development checks

```sh
rtk proxy go build ./...
rtk proxy go test ./...
rtk proxy go test -race ./...
rtk proxy go vet ./...
```

These checks cover the core packages. They do not establish HTTP forwarding,
fault execution or end-to-end MVP acceptance. If the workspace sandbox blocks
the default Go build cache, prefix the Go invocation with
`env GOCACHE=/private/tmp/faultline-go-build` after `rtk proxy`.

See [configuration defaults and example](examples/http/README.md) and
[phase 1 implementation decisions](plans/01-core/README.md#quyết-định-hiện-thực).

## Design documents

- [Current specification and agreed MVP scope](specific.md)
- [Implementation phases and feature plans](plans/README.md)
