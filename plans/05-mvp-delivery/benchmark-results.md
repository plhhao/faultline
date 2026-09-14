# Resource verification and benchmark

Measured on Apple M2, 8 CPU cores, 16 GiB RAM, macOS/arm64, Go 1.26.4.
Benchmark date: 2026-09-14. Local loopback HTTP/1.1; Docker builds/tests had
finished before the recorded benchmark. This is a dev-machine sample, not an SLA.

## Reproduce

```sh
rtk proxy go test ./tests/integration -run '^$' -bench '^BenchmarkHTTP$' -benchmem -benchtime=500x -count=3 -cpu=1,8 -timeout 90s
rtk proxy go test -race ./tests/integration -run '^Test(ResourceRecovery|SlowHeadersAndIdleConnectionsExpire|GracefulShutdownDrainAndDeadline|ManyHeldFlowsShutdown)$' -v -count=1
```

`-cpu` sets GOMAXPROCS and, with this benchmark's default RunParallel settings,
one or eight client workers. Each sample sends exactly 500 POST attempts with
empty bodies, a 2-byte upstream response and no client retry. The client allows
keep-alive in all modes; Faultline opens a fresh upstream connection per request.
Hold closes the downstream connection, so subsequent attempts reconnect.

Proxy limits: inflight 1000, flow/header/read/write/idle timeout 5s. Recorder
queue: 1024, JSON sink: `io.Discard`. Delay and hold-response duration: 5ms,
probability 1. Direct mode has neither proxy nor recorder. Client timeout: 5s.

## Results

Median of three samples; ns/op is wall time divided by completed attempts.
For eight workers it describes aggregate throughput, **not individual latency**.
B/op and allocations include the in-process client, upstream and recorder as
well as the proxy. They do not measure isolated proxy RSS or retained memory.

| Mode | Workers | Median ns/op | Median B/op | Median allocations/op |
| --- | ---: | ---: | ---: | ---: |
| Direct | 1 | 33,774 | 6,884 | 77 |
| Pass-through | 1 | 156,849 | 64,152 | 272 |
| Delay 5ms | 1 | 6,764,214 | 65,676 | 292 |
| Hold response 5ms | 1 | 7,092,793 | 43,409 | 336 |
| Direct | 8 | 11,258 | 7,504 | 78 |
| Pass-through | 8 | 64,532 | 70,042 | 280 |
| Delay 5ms | 8 | 902,079 | 71,125 | 299 |
| Hold response 5ms | 8 | 942,422 | 44,948 | 339 |

All samples passed response/fault-count assertions; dropped events were 0.
Direct single-worker samples ranged from 29,464 to 62,551 ns/op, illustrating
warm-up/scheduling variability. The median pass-through increment is about
123 microseconds per attempt at one worker in this setup. That includes a second
HTTP hop, fresh upstream TCP connection and JSON recording. The added configured
5ms wait, timer scheduling and hold reconnect cost are not forwarding overhead.

An initial time-calibrated fresh-connection benchmark exhausted local ephemeral
ports and failed; those samples are excluded. Fixed request counts keep the
measurement bounded. Very large counts/repeated runs can still encounter OS
TIME_WAIT limits; wait for recovery instead of changing system network settings.

## Resource recovery

`TestResourceRecovery` runs three waves of 32 concurrent flows for each of delay,
hold_request and hold_response: 96 flows per action. Fault duration is 1 minute;
the flow deadline is 400ms, inflight limit 32, recorder queue 256. Each wave must
finish with all flows applied and active counters back at zero.

Observed non-race run: goroutines returned from 3 to 3 for all three actions.
Live heap after GC changed from 280,288 to 1,089,640 bytes (delay), 554,752 to
1,111,880 (hold request), and 579,200 to 1,196,280 (hold response). Test tolerance
is baseline +8 goroutines and +8 MiB live heap within 3s after cleanup, allowing
runtime/HTTP caches and race instrumentation. RSS is not required to shrink;
Go may retain pages. This finite run detects gross retention, not all possible
long-duration leaks.

Other tests cover 8 MiB streamed uploads, incomplete uploads, slow headers/idle
expiry, early client cancellation, shared inflight overload, full recorder queues,
normal drain and cancellation of 24 held flows after shutdown's grace deadline.
See the [acceptance matrix](acceptance.md) for the exact test mapping.
