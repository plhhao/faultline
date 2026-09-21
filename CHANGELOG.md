# Changelog

Completed work following DEFINE → PLAN → BUILD → VERIFY → REVIEW.

## 2026-09-20

- Added `docs/deployment.md` for Linux/systemd deployment: file and managed UI
  modes, service-account permissions, Unix socket operations, HTTPS/network
  boundaries, journald retention, managed-state backup and Docker handoff.
  Linked it from `docs/README.md`; documentation links and whitespace checked.

## 2026-09-19

- Added `docs/cli-reference.md`, a Vietnamese reference for every Faultline CLI
  command and flag, including defaults, required values, managed-mode constraints
  and operational effects. Linked it from `docs/README.md`; command help output,
  local links and Markdown whitespace were checked.

## 2026-09-18

- Documented managed UI account creation, role updates and deletion in
  `docs/operations.md`, including password piping that avoids shell history.
  Reviewed command syntax and documentation whitespace; no runtime checks apply.

## 2026-09-17

- Added Vietnamese user documentation in `docs/`: navigation, HTTP quick start,
  configuration/rules, CLI operations, tester UI and PostgreSQL/MySQL commit
  testing. Linked it from the root README; internal links and whitespace checked.

- Completed P25 MySQL 8.4.8 adapter (`internal/proxy/mysql`) with caching_sha2_password, confirmed explicit-COMMIT delay/hold/disconnect, bounded sessions/packets/prepared statements, config/CLI/API/UI integration and Go retry fixture (`examples/mysql`). Full Go race regression including real MySQL/PostgreSQL, vet, binary/Docker builds, MySQL Docker runtime 2/1-row evidence, 12 Node checks and example/link/whitespace checks passed; additional auth/wire unit tests passed with race detection. Contract and acceptance record TLS/TLS or plaintext/plaintext only, conservative transaction tracking after errors, and protocol limits; reused UI verified automatically without new manual browser tests.

- Planned P25 MySQL 8.4 LTS semantic adapter with a Go fixture, explicit confirmed-COMMIT faults, auth/TLS contract gate, lifecycle bounds and nine acceptance criteria. Updated Phase 9 index, roadmap and specification; local plan links and whitespace checked. Planning only; MySQL implementation and runtime verification have not started.

- Fixed stale proxy choices after re-login in the admin UI: untouched drafts reload from active config; edited drafts remain available with an explanation and require validation again. Added login regression tests for removed/added proxies and preserved edits; all 10 Node editor/diff tests and whitespace checks passed. Browser retest pending.

- Recorded user-reported PostgreSQL UI PG1–PG6 PASS and added `examples/postgresql/ui.yaml` plus mixed-protocol UI instructions. HTTP/gRPC use ports 18080/18081; prepared a local replacement from managed revision 4 preserving PostgreSQL rules. Both configs passed CLI validation and whitespace checks; mixed-protocol browser testing remains pending.

- Completed the P23 PostgreSQL contract and implemented the adapter, config/CLI/API/UI integration and Go retry fixture (`internal/proxy/postgresql`, `examples/postgresql`). Added confirmed-COMMIT delay/hold/disconnect, SCRAM/TLS, bounded lifecycle and cancellation; unit/integration tests, real PostgreSQL Docker fixture, binary retry demo, full Go/race regression, vet, binary/Docker builds and 8 Node checks passed. P24/Phase 9 remains In progress pending user manual UI acceptance; versions and limits are recorded in `plans/09-semantic-adapters/acceptance.md`.

## 2026-09-16

- Planned Phase 9 PostgreSQL ahead of deferred Phase 8: separated delay/hold/disconnect from protocol phases, including successful COMMIT acknowledgment interception; defined P23 contract work and P24 acceptance, Go/Docker fixture, SCRAM/TLS scope and exclusions. Synchronized roadmap/specification; local link targets and whitespace checks passed. Documentation only; adapter remains unimplemented.

- Added a shared teal F/fault-line SVG favicon and 36px header logo before FAULTLINE in the embedded admin UI. Extended public asset serving and its existing test; targeted remote asset/session test and whitespace checks passed. Browser visual confirmation remains pending for this addition.

- Closed Phase 7 (P19/P20/P20a) after the user reported every tester-guide case PASS, including operations, Docker, path patterns and diff UI. Updated checklists, acceptance and roadmap; reviewed documentation consistency and whitespace. Manual results are user-reported; no runtime tests rerun.

- Corrected the tester Docker command to mount `/tmp` with `--tmpfs /tmp:mode=1777`, matching the managed integration fixture and allowing host UID 501 to create its admin directory. Reviewed against Dockerfile ownership and integration arguments; whitespace passed. Docker was not rerun for this documentation fix.

- Recorded user-reported U1–U9 PASS in the tester guide, P20 notes and acceptance mapping. Operational/Docker and supplemental path-pattern/diff checks remain separately scoped; documentation reviewed, no runtime tests rerun.

- Fixed false null/absent highlights for optional Fault parameters in `internal/control/remote/ui/diff.js` without mutating drafts or suppressing actual zero/empty-string values. Updated P20 notes; all six Node diff tests and whitespace checks passed. Browser confirmation remains with the tester.

- Added field-level Active/Draft rule diff highlighting in `internal/control/remote/ui/diff.js`: added/removed/changed markers, proxy/rule ID pairing, position changes and duplicate-ID warnings; renamed IDs appear as remove/add. Served the embedded diff script and added tester instructions. Five Node logic/render tests, app syntax, remote asset/session test and whitespace checks passed. Browser visual/interaction acceptance remains pending.

- Made the Runtime readiness label uppercase, bold and 20px in `internal/control/remote/ui/style.css`. Checked the readiness DOM target and whitespace; browser visual verification remains pending.

## 2026-09-15

- Removed the duplicate large injection status from the Runtime heading in `internal/control/remote/ui/`; retained the status badge beside Enable/Disable and removed obsolete DOM updates. JavaScript syntax, DOM-reference and whitespace checks passed; browser confirmation remains pending.

- Revised injection feedback in `internal/control/remote/ui/` after user approval: removed sticky header, placed ON/OFF badge beside Runtime controls, added dismissible bottom-corner notifications (success expires after 5s, errors persist). Updated tester retest instructions and phase 7 notes. JavaScript syntax, DOM references/unique IDs, remote asset/session test and whitespace checks passed; visual/interaction acceptance remains with the user.

- Addressed tester U1–U4 feedback in `internal/control/remote/ui/`: sticky injection badge, pending/confirmed toggle feedback, stale status-response guard, scrolling/focus to operation errors, phase choices constrained by fault/direction, and method/observation retention explanations. Recorded user U1–U4 PASS and added focused retest instructions in `examples/tester/README.md` and phase 7 plans. JavaScript syntax, 7-action × 2-direction phase checks, remote asset/session test, documentation link targets and whitespace passed. Browser acceptance of the revised UI remains pending; P20 stays In progress.

- Added `TestGRPCHoldResponseClientDeadline` in `tests/integration/grpc_test.go`: plaintext, TLS and mTLS on both legs; 10s response hold with 1s client deadline and 15s proxy timeout. Verifies upstream processing and fault entry before deadline, client `DeadlineExceeded`, applied/canceled report, prompt hold cleanup and a successful subsequent RPC with one inflight slot. Targeted test passed three consecutive runs with race detection (9 subcases); integration-package vet and whitespace checks passed. No production changes; mixed TLS modes and Docker were not rerun for this addition.

- Implemented P20a path-pattern backend and UI in config/engine/remote: whole-segment `:param`, exclusive exact/pattern fields, unchanged first-match ownership and query forwarding, Any/Exact/Pattern form. Added config/engine/HTTP1/HTTP2 tests and API persistence/gRPC regressions, fixture and manual UI checklist in `examples/tester/`; synchronized specification and phase plans. Full Go race suite, vet, CLI build/fixture validation and JavaScript syntax passed. P20a remains In progress pending user UI acceptance; Docker opt-in not rerun.

- Added planned P20a path-pattern work in `plans/07-tester-experience/03-path-pattern.md` and synchronized phase/roadmap indexes. Proposed separate `match.path_pattern` with single-segment `:param`, preserving exact matching and query forwarding; defined schema, engine, API/UI, persistence and six acceptance criteria. Verified local link targets, workflow sections, acceptance mapping and whitespace. Documentation only; implementation and UI acceptance remain pending.

- Completed P19 managed administration in `internal/control/remote/`, control persistence/revision publication and CLI `user/configure`: independent viewer/editor accounts, HTTPS sessions/CSRF, durable config/apply result and bounded audit, fail-closed writes, read-only Unix admin in managed mode. Added embedded rule-editing UI, shared action capabilities, bounded outcome counters and `examples/tester/` setup/manual-test guide. Full Go tests/race, final targeted race, vet, CLI build/help, real binary and Docker HTTPS/apply/restart smoke, JavaScript syntax and setup-script smoke passed; checked 115 local links/anchors, DOM IDs, 13 AC mappings and whitespace. Docker uses tmpfs for runtime sockets and a separate data volume. P20/browser interaction and visual acceptance remain pending user testing; phase 7 remains In progress. See `plans/07-tester-experience/acceptance.md` for evidence and storage/session/draft limits.

- Updated phase 7 plans in `plans/07-tester-experience/`, `plans/README.md` and `specific.md` to Planned: single-instance API/UI, persistent managed config with revision checks, disabled-by-default restart, independent login with two instance-wide roles, HTTPS and rule editing on existing proxies. Defined P19 → P20 order and 13 acceptance criteria; implementation remains pending. Verified 78 local links/anchors, plan statuses, workflow sections, acceptance IDs and whitespace; reviewed scope and specification consistency. Runtime tests were not run for this documentation-only update.

## 2026-09-14

- Completed phase 6 (P17 → P16 → P18): HTTP/2 on both legs, optional per-leg mTLS, unary gRPC service/method/metadata matching, and request/response truncate/throttle. Updated config validation/TLS restart fingerprints, shared proxy stream lifecycle, body fault wrappers, recorder protocol/status, runnable gRPC examples and binary/Docker fixtures; recorded 15 AC in `plans/06-protocol-extensions/acceptance.md`. Full tests and final full race suite, vet, CLI build, example validation, binary smoke, Docker MVP/payment regression and phase 6 smoke passed; checked 130 local documentation links/anchors and whitespace. Review fixed HTTP/2 connection-closing headers and preserved upstream status in applied events. gRPC streaming/certificate hot reload remain deferred; each flow owns a fresh upstream connection to prevent proxy retries.

- Updated phase 6 planning in `plans/06-protocol-extensions/`, `plans/README.md` and `specific.md`: P17 HTTP/2 on both legs with optional per-leg mTLS → P16 unary gRPC → P18 request/response truncate then throttle for HTTP and gRPC. Defined 15 acceptance criteria, TLS restart behavior, stream scope, verification matrix and deferred streaming/certificate rotation; all three plans are Planned, not implemented. Verified 84 local links/anchors, plan statuses, workflow sections, acceptance IDs and whitespace; reviewed scope/dependencies and specification consistency. Runtime tests were not run for this documentation-only update.

- Completed phase 5 (P13–P15) and MVP AC1–AC24: bounded graceful shutdown in `internal/proxy/http/` and CLI; resource recovery/timeout tests and bounded benchmarks; payment dependency/retry driver in `examples/http/paymentdemo/`; non-root source-built Docker image in `deploy/docker/`. Binary and Docker demos both produce two payments without idempotency and one with it after two lost-response timeouts. Added action/selector examples, delivery guidance, acceptance mapping and measured benchmark results; updated specification and plans.
- Verification: full `go test -race ./... -timeout 120s`, vet, native macOS arm64 build, Linux arm64 cross-build, source-only export build, binary help and five example validations passed. Delivery Docker smoke passed config/TLS/admin/reload rejection, payment variants and clean exit; extended binary CLI and 24-held-flow shutdown tests passed separately under race. Benchmark passed three fixed 500-attempt samples at one/eight workers. Reviewed code, cleanup, documentation links and formatting. Measurements are local dev samples, not capacity guarantees; no image/release published and post-MVP protocols/UI remain deferred.

## 2026-09-13

- Completed phase 4 (P10–P12) after Docker/OrbStack became available. `tests/integration/container_test.go` passed three consecutive runs: mounted root/includes/TLS, startup readiness/disabled state, runtime commands, changed/no-op reload, duplicate-ID rejection preserving revision/state, and shutdown exit code 0. Fixture writes use rename and read-only validation waits for bind-mount visibility before reload; admin mutations still have no retry. Test-owned images/containers are cleaned up. Reviewed response assertions and updated phase plans, README and AGENTS; release packaging and full MVP acceptance remain phase 5.

- Implemented and verified host runtime administration and P12 recorder: `status/enable/disable/reload` over a private per-instance Unix socket, absolute-root reload with includes, live active-fault status and current-revision rule counters. Added bounded JSON lifecycle/control events, run counters independent of queue delivery, redaction, shutdown summary/flush deadline and sink-loss diagnostics. `serve` keeps running after a closed stdout pipe (SIGPIPE). Main changes: `cmd/faultline/`, `internal/control/admin/`, `internal/recorder/`, HTTP lifecycle reporting and builtin action-start notification; usage/specification/plans updated.
- Verification: full `go test -race ./... -timeout 90s` passed (Docker opt-in skipped); CLI suite passed again after SIGPIPE handling. Process tests cover startup/nth, toggles, reload on keep-alive, old snapshots, restart and JSON privacy; queue/sink/overload/timeout/TLS and concurrent reload tests passed. `go vet ./...`, macOS CLI and Linux arm64 cross-build, binary help/multi-file validate passed; timeout classification repeated 10 times. Reviewed source/diffs and corrected the existing overload observer test for its newly emitted rejection report. **Phase 4 is not fully Done:** the explicit Docker smoke test fails at daemon connection; all available contexts are unavailable and OrbStack VM startup timed out. P10/P11 container verification remains blocked, with a reproducible opt-in test ready; no claim of Docker execution from cross-build alone.

- Completed phase 3 (P07–P09): `internal/fault/builtin.go` implements delay, respond, close_connection, hold_request and hold_response; CLI now wires the executor into `serve --start-enabled`. HTTP capabilities enforce bodyless responses and early-response framing, cancel upstream before holding its response, close client connections and clean up discarded request uploads. Updated usage/specification and phase plans.
- Verification: `go test -race ./... -timeout 90s`, `go vet ./...`, CLI build, binary help and multi-file validate passed. Added deterministic timer tests, four-way HTTP/HTTPS fault coverage, probability/listener isolation, bodyless keep-alive, partial uploads, cancellation/deadline/shutdown, side-effect preservation and concurrent hold cleanup. Upload cleanup passed three repeated race runs; the additional unread-upload delay deadline regression passed separately. Sandbox blocked initial network tests; reran with localhost binding permission. Reviewed source/diffs manually because graph analysis returned no function/flow coverage. Fixed early-response unexpected EOF and upload-reader/close interference found by tests. Delay preserves backpressure, so disconnect detection during an unread upload can wait for I/O; deadlines/shutdown still bound it. Admin/recorder, full payment demo, resource benchmarks and Docker remain phases 4–5.

- Completed phase 2 (P04–P06): CLI `validate`/`serve` with includes and disabled startup; multi-listener HTTP/HTTPS streaming, HTTP/1.1 negotiation, verified upstream trust/SNI, no-retry transport, inflight/deadline enforcement and cancellation/shutdown cleanup. Added per-request snapshot hooks, protocol-independent executor capabilities and in-process outcome reports in `cmd/faultline/`, `internal/proxy/http/`, `internal/fault/`, with CLI and `tests/integration/` coverage. Updated plans and usage documentation.
- Verification: full race/coverage suite (`go test -race -coverpkg=./... ./... -timeout 60s`), vet, CLI build and binary help/multi-file validate passed; integration race suite passed again after adding same-connection snapshot assertions. Initial network tests were blocked by sandbox TCP binding and rerun successfully with localhost permission. Reviewed source manually because the graph contains no source nodes; formatting and whitespace checked. Upstream connections are not reused (TCP/TLS cost); production fault actions, admin commands, recorder and Docker remain later phases. `--start-enabled` currently returns 501 for a selected action at its phase, without reporting it applied.

## 2026-09-12

- Completed P01a and phase 1: `config.Load` supports root includes and proxy fragments, reports duplicate IDs with both source locations, rejects repeated physical files, and resolves TLS paths against the declaring file. Added `control.ReloadFile`, multi-file examples and regression tests; standalone byte parsing explicitly rejects includes.
- Verification: unit/race tests, package build and vet passed using the writable temporary Go cache. Coverage measured at config 92.2%, control 97.8%, engine 100%; additional add/remove-proxy reload regression tests also passed under the race detector. Checked documentation links and whitespace. Snapshot/no-op/error behavior is covered at core level; CLI/Docker MC5 remains scheduled in later phases.

- Planned multi-file configuration in specification section 7.1 and new P01a: root includes proxy fragments, duplicate IDs are rejected within their scope, TLS paths follow the declaring file, and reload validates the complete set before atomic apply. Reopened phase 1 for this addition and aligned CLI, reload, Docker and example documentation. Verified local documentation links and whitespace; loader implementation and MC1–MC5 checks remain pending.

- Removed references to the deleted original design document from `AGENTS.md`, `README.md`, `specific.md` and this changelog; `specific.md` is the primary specification. Verified no remaining filename references in Markdown files and no whitespace errors. No runtime tests needed for this documentation-only change.

- Implemented phase 1 (P01–P03): strict YAML configuration and TLS validation in `internal/config/`, immutable runtime snapshots and atomic reload/toggle in `internal/control/`, and matching, seeded selectors and synchronized rule counters in `internal/engine/`. Added unit/race tests, a config example, pinned YAML dependency, and updated plans and documentation.
- Verification: package build, unit tests, `go test -race -cover ./...`, `go vet ./...`, formatting and module tidy passed on Go 1.26.4 darwin/arm64. Coverage: config 93.1%, control 97.6%, engine 100%. Used a writable temporary Go build cache. Phase 1 documentation links passed; pre-existing links to a removed design document were unresolved at that time. CLI, proxy I/O, fault execution and end-to-end MVP acceptance remain pending.

- Added 9 implementation phases and 24 feature plans under `plans/`: 15 MVP plans and 9 deferred extension plans, with dependencies, workflow steps, verification criteria, and an AC1–AC24 coverage map. Linked the roadmap from `AGENTS.md` and `README.md`.
- Verification: reviewed scope against `specific.md`; checked local links and anchors, plan IDs and statuses, dependency cycles, required sections, and all 24 acceptance mappings. Checks passed; runtime tests were not run because this change only adds planning documentation. Feature implementation remains pending.

- Added the AI task workflow in `.agents/rules/workflow.md` and linked it from `AGENTS.md`, including completion criteria and changelog requirements.
- Verification: reviewed instructions for consistency and checked workflow stages, completion criteria, history preservation, and Markdown links; all checks passed. Runtime tests were not run for this documentation-only change.
