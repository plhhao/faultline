# Repository Guidelines

## Agent Workflow

Follow [task workflow](.agents/rules/workflow.md): **DEFINE → PLAN → BUILD → VERIFY → REVIEW**. After Done, record the completed work and verification results in [CHANGELOG.md](CHANGELOG.md).

Use [implementation plans](plans/README.md) for phase order, feature dependencies, and acceptance criteria. Update the relevant plan as work progresses; deferred plans require scope definition before implementation.

## Project Structure & Module Organization

Faultline is a Go network-failure testing proxy. The HTTP/HTTPS MVP, local admin, JSON recorder, payment demo and binary/Docker delivery are implemented. Phase 5 records AC1–AC24 evidence and resource measurements. Phase 6 adds HTTP/2, unary gRPC, optional per-leg mTLS, and request/response truncate/throttle, with 15 additional acceptance criteria. Follow `specific.md` for the agreed scope.

- `cmd/faultline/`: CLI entry point and component wiring.
- `internal/config/`: configuration parsing and validation.
- `internal/control/`: active snapshots, reload, injection enable/disable, and status.
- `internal/control/admin/`: private Unix socket server and runtime CLI client.
- `internal/engine/`: protocol-independent rule matching and fault selection.
- `internal/fault/`: fault actions; `internal/proxy/http/`: shared HTTP/1–HTTP/2 I/O, TLS/mTLS and lifecycle hooks; `internal/proxy/grpc/`: gRPC metadata/status/deadline semantics.
- `internal/recorder/`: structured events and counters.
- `examples/http/`: configs and payment demo/driver; `examples/grpc/`: unary fixture, TLS/mTLS and Docker examples; `tests/integration/`: cross-component tests and benchmarks; `deploy/docker/`: non-root Docker packaging.

Remove `.gitkeep` when adding real files to its directory.

## Build, Test, and Development Commands

The module is `faultline`, declaring Go 1.26.4. Prefix shell commands with `rtk` in this workspace; `rtk proxy` preserves unfiltered output.

- `rtk proxy go list -m`: verify the module now.

CLI commands:

- `rtk proxy go build -o bin/faultline ./cmd/faultline`: build the executable.
- `rtk proxy go run ./cmd/faultline --help`: inspect implemented CLI usage.
- `status`, `enable`, `disable`, `reload --config FILE` target `--admin-socket PATH`; `serve` emits JSON stdout and human diagnostics stderr.

Available now:

- `rtk proxy go build ./...`: compile implemented packages.
- `rtk proxy go test ./...`: run tests.
- `rtk proxy go test -race ./...`: check exercised paths for data races.
- `rtk proxy go vet ./...`: run static checks.
- `rtk proxy go fmt ./...`: format Go packages.

YAML is pinned in `go.mod`; Docker builds with Go 1.26.4. No additional linter is configured. Build: `rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:local .`. Opt-in verification: `rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestContainerRuntime$' -timeout 360s`.

## Coding Style & Architecture

Use `gofmt`, lowercase package names, exported `MixedCaps` names, and descriptive filenames such as `selector.go`. Prefer small interfaces and explicit error handling. Follow [comment rules](.agents/rules/code-comments.md).

Keep protocol I/O outside `engine`. Adapters provide protocol-specific capabilities; shared fault code must not import concrete adapters. CLI and future UI integrations should reuse `control`. Reuse helpers to avoid duplication. Introduce `common`/`utils` only for shared behavior; prefer purpose-specific packages and avoid import cycles.

## Testing Guidelines

Use Go's standard `testing` package. Place unit tests beside implementations in `*_test.go`, with functions named `TestBehavior`. Use table-driven cases where appropriate. Prioritize probability boundaries, reload consistency, cancellation, and resource cleanup. No numeric coverage threshold is established. HTTP/TLS integration tests require localhost TCP binding; Docker tests are opt-in. Follow the bounded benchmark command in `plans/05-mvp-delivery/benchmark-results.md`.

## Commit & Pull Request Guidelines

Use short, imperative subjects, for example `feat(config): validate fault probabilities`. Keep changes focused.

PRs should explain behavior changes, reference relevant specification sections or issues, and list validation results and limitations. Update examples when configuration changes. Never commit real TLS private keys or credentials.
