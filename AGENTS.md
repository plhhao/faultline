# Repository Guidelines

## Agent Workflow

Follow [task workflow](.agents/rules/workflow.md): **DEFINE → PLAN → BUILD → VERIFY → REVIEW**. After Done, record the completed work and verification results in [CHANGELOG.md](CHANGELOG.md).

Use [implementation plans](plans/README.md) for phase order, feature dependencies, and acceptance criteria. Update the relevant plan as work progresses; deferred plans require scope definition before implementation.

## Project Structure & Module Organization

Faultline is a Go network-failure testing proxy. Core, CLI validate/serve and HTTP/HTTPS forwarding are implemented; fault actions and runtime administration are pending. Follow `specific.md` for the agreed HTTP/HTTPS MVP and future roadmap.

- `cmd/faultline/`: CLI entry point and component wiring.
- `internal/config/`: configuration parsing and validation.
- `internal/control/`: active snapshots, reload, injection enable/disable, and status.
- `internal/engine/`: protocol-independent rule matching and fault selection.
- `internal/fault/`: fault actions; `internal/proxy/http/`: HTTP I/O, TLS, and lifecycle hooks.
- `internal/recorder/`: structured events and counters.
- `examples/http/`: demo configurations; `tests/integration/`: cross-component tests; `deploy/docker/`: Docker packaging.

Remove `.gitkeep` when adding real files to its directory.

## Build, Test, and Development Commands

The module is `faultline`, declaring Go 1.26.4. Prefix shell commands with `rtk` in this workspace; `rtk proxy` preserves unfiltered output.

- `rtk proxy go list -m`: verify the module now.

CLI commands:

- `rtk proxy go build -o bin/faultline ./cmd/faultline`: build the executable.
- `rtk proxy go run ./cmd/faultline --help`: inspect implemented CLI usage.

Available now:

- `rtk proxy go build ./...`: compile implemented packages.
- `rtk proxy go test ./...`: run tests.
- `rtk proxy go test -race ./...`: check exercised paths for data races.
- `rtk proxy go vet ./...`: run static checks.
- `rtk proxy go fmt ./...`: format Go packages.

YAML is pinned in `go.mod`; no Dockerfile or additional linter exists yet.

## Coding Style & Architecture

Use `gofmt`, lowercase package names, exported `MixedCaps` names, and descriptive filenames such as `selector.go`. Prefer small interfaces and explicit error handling. Follow [comment rules](.agents/rules/code-comments.md).

Keep protocol I/O outside `engine`. Adapters provide protocol-specific capabilities; shared fault code must not import concrete adapters. CLI and future UI integrations should reuse `control`. Reuse helpers to avoid duplication. Introduce `common`/`utils` only for shared behavior; prefer purpose-specific packages and avoid import cycles.

## Testing Guidelines

Use Go's standard `testing` package. Place unit tests beside implementations in `*_test.go`, with functions named `TestBehavior`. Use table-driven cases where appropriate. Prioritize probability boundaries, reload consistency, cancellation, and resource cleanup. No numeric coverage threshold is established. HTTP/TLS integration tests require localhost TCP binding; end-to-end MVP acceptance remains pending.

## Commit & Pull Request Guidelines

No Git repository or commit history exists yet. Adopt short, imperative subjects, for example `feat(config): validate fault probabilities`. Keep changes focused.

PRs should explain behavior changes, reference relevant specification sections or issues, and list validation results and limitations. Update examples when configuration changes. Never commit real TLS private keys or credentials.
