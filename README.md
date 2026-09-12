# Faultline

Faultline is a Go proxy for testing how applications handle network failures.
It sits between an application and its dependency, injects configurable faults,
and records what happened.

The first milestone targets HTTP/1.1 over HTTP and HTTPS, with file-based
configuration, runtime injection controls, and one fault action per request.

**Status:** directory scaffold only. The CLI, proxy, and fault actions are not
implemented yet.

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
│   └── http/               # Example configurations and reproducible demos
├── tests/
│   └── integration/        # Tests across the proxy, client, and upstream
├── deploy/
│   └── docker/             # Docker packaging and demo deployment files
├── plans/                  # Existing implementation planning directory
├── faultline-project-spec.md
├── specific.md
├── go.mod
└── README.md
```

Empty directories contain `.gitkeep` files so they can be tracked when the
project is added to Git. Remove each placeholder when its directory gains files.

## Component boundaries

- `cmd/faultline` will parse CLI arguments and wire components together.
- `config` will define and validate configuration data. `control` will manage
  the running process's configuration revisions and injection state.
- `engine` will choose a rule and action without performing protocol I/O.
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

There are no external dependencies or runnable commands at this stage.

## Design documents

- [Current specification and agreed MVP scope](specific.md)
- [Implementation phases and feature plans](plans/README.md)
- [Original project vision](faultline-project-spec.md)
