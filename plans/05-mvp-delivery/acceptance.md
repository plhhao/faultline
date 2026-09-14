# MVP acceptance evidence

Verification completed on macOS arm64 (Go 1.26.4) and Linux arm64 via
Docker/OrbStack, 2026-09-14. All AC1–AC24 have passing evidence. Test names below
are executable Go tests; demo assertions inspect the dependency independently.

## Commands and results

- `go test -race ./... -timeout 120s`: pass; opt-in Docker test skipped here.
- `FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestContainerRuntime$' -v -count=1 -timeout 360s`: pass against the delivery Dockerfile, including payment variants.
- `go test -race ./tests/integration -run '^Test(PaymentDemoBinary|ManyHeldFlowsShutdown)$' -v -count=1`: pass after extending binary CLI coverage and held-flow graceful shutdown.
- `go vet ./...`, native CLI build and Linux arm64 CGO=0 cross-build: pass.
- `go build ./...` from a temporary source-only export (no `.git`, build outputs or local caches in its tree): pass. Toolchain/module caches remain external.
- Binary help and all five example root configurations: pass validation.
- Bounded benchmark, three samples at one/eight workers: pass; see [conditions and measurements](benchmark-results.md).

Workspace commands use `rtk proxy`; this environment additionally sets
`GOCACHE=/private/tmp/faultline-go-build`. Network and Docker checks ran with
permission for local sockets/daemon access. No registry image or release was published.

## AC mapping

Tests are under [integration](../../tests/integration/), except where a package
is explicitly named. Existing coverage is reused instead of duplicating it.

| AC | Passing evidence | Observed contract |
| --- | --- | --- |
| AC1 | `TestForwardingAndDisabledSelection`, engine `TestMatching` | Method/escaped path/query/body and response preserved; disabled/non-match does not select. |
| AC2 | `TestCloseProbabilityAndListenerIsolation` | A second listener remains unaffected. |
| AC3 | `TestCloseProbabilityAndListenerIsolation` | Probability 0/1 boundaries, pre-upstream close and upstream attempt counts. |
| AC4 | engine `TestProbabilityReplayAndDistribution`, control `TestSeedReplayAcrossRevisionAndRun` | Sequential replay and predeclared probability tolerance. |
| AC5 | engine `TestSequenceSelectorsAndDisabledTraffic` | nth/every count eligible requests only. |
| AC6 | engine `TestFirstRuleOwnsRequest` | No fallthrough after the first matching rule. |
| AC7 | `TestDelay500Milliseconds`, `TestTimedFaultCancellation` | 500ms delay with 3s upper timing tolerance; cancellation frees the flow. |
| AC8 | `TestFaultTLSMatrix`, `TestRespondFramingAndKeepAlive` | Configured response without upstream call, correct HTTP framing. |
| AC9 | `TestPaymentDemoBinary`, `TestContainerRuntime` | Two timeouts; stored payments 2 without idempotency, 1 with it; events show held upstream 201. |
| AC10 | `TestFinalHeadersAndSnapshot`, CLI `TestProcessRuntimeReloadAndEvents` | Old snapshot retained; new revision on the next keep-alive request. |
| AC11 | admin `TestReloadIncludesAndAtomicRejection`, control `TestInvalidReloadAndRestartFieldsAreAtomic` | Invalid/restart-only changes rejected without partial apply. |
| AC12 | control `TestApplyNoopChangedRevisionAndOldOwnership`, admin `TestReloadIncludesAndAtomicRejection` | Effective no-op preserves counters; real revision resets them. |
| AC13 | `TestBuiltinNotReached`, `TestEventOutcomesAndRedaction` | Selected after-headers fault cannot apply before headers exist. |
| AC14 | `TestResourceRecovery`, `TestManyHeldFlowsShutdown`, `TestGracefulShutdownDrainAndDeadline`, `TestTimedFaultCancellation` | Deadline/cancel/drain cleanup; bounded live heap/goroutines after repeated waves. |
| AC15 | `TestOverloadIsNotSelected`, `TestSlowRecorderDoesNotBlockProxyOrAdmin`, recorder `TestFullQueuePreservesCounters` | Overload/dropped events remain distinct from selection/application. |
| AC16 | `TestEventOutcomesAndRedaction`, `TestPaymentDemoBinary` | Flow/rule/revision/phase/decision/outcome present; sensitive payload excluded. |
| AC17 | CLI `TestProcessRuntimeReloadAndEvents`, `TestPaymentDemoBinary` | Disabled startup and application readiness before enable. |
| AC18 | `TestDisableAndReloadPreserveActiveFault` | New state affects new requests; active old faults remain observable. |
| AC19 | CLI `TestProcessRuntimeReloadAndEvents`, control `TestStartupAndToggleSemantics` | Toggle does not reset; reload preserves enabled state; restart creates a disabled run. |
| AC20 | CLI `TestProcessRuntimeReloadAndEvents` | Startup traffic does not consume nth:1. |
| AC21 | `TestTimedFaultCancellation`, `TestHoldRequestUploadLifecycle` | Hold request prevents upstream work and releases canceled uploads. |
| AC22 | `TestTLSMatrixHTTP1`, `TestFaultTLSMatrix` | Pass-through and all actions across four HTTP/HTTPS combinations. |
| AC23 | `TestUpstreamTLSFailuresAreNatural`, `TestChangedListenerKeyFailsBeforeServing`, `TestContainerRuntime` | Natural TLS failure classification; invalid listener cert prevents startup. |
| AC24 | `TestPaymentDemoBinary`, `TestContainerRuntime` | Binary/Docker validate, serve, admin commands, equivalent demo, clean shutdown. |

Multi-file MC1–MC4 are covered by config `TestLoad*`, control
`TestReloadFileAtomicityAndNoop` and admin reload tests. Container MC5 now uses
the delivery image: single/multi-file equivalence, mounted TLS, changed fragment,
missing fragment/source path, duplicate ID/source path, preserved state and revision.

## Review and limits

- CLI drains accepted requests for up to 5s, then cancels them; recorder flush is
  bounded at 2s. Forced-kill/crash has no automatic bypass. Builtins honor cancel;
  a custom blocking observer/executor violates the adapter's bounded callback contract.
- Inflight limits handler flows, not all accepted TCP connections. Header/idle
  timeouts bound stalled connections; this dev proxy is not a hostile-load firewall.
- Resource measurements cover finite local workloads and live Go heap, not an
  RSS/throughput SLA or proof of absence of long-term leaks. JSON-to-discard
  benchmarks do not represent a slow production sink.
- HTTP/1.1 only; no mTLS, HTTP/2, TCP database/broker adapters, packet loss,
  truncate/throttle, automatic retry or remote UI/API. Ordinary HTTPS verifies trust.
- Store inspection proves this demo's payments. The proxy does not infer business
  commit, logical retries or general application PASS/FAIL from traffic.
- Seed replay depends on eligible input order. Reload is atomic for runtime
  snapshots, not a transaction over files edited concurrently; VM bind mounts
  can briefly expose incomplete updates. Validation errors leave state unchanged.
- Docker runs non-root with readable mounted config/certs; testing generated
  disposable TLS fixtures does not authorize committing real private keys.
