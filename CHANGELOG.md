# Changelog

Completed work following DEFINE → PLAN → BUILD → VERIFY → REVIEW.

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
