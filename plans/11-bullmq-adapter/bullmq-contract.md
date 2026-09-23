# BullMQ contract — P28

Verified 2026-09-23 on macOS arm64 with Docker/OrbStack Linux arm64.

## Pins and scope

- Node 24.18.0; Docker `node:24.18.0-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd`.
- BullMQ 5.81.5, ioredis 5.8.2; transitive dependencies in
  [package-lock.json](../../examples/bullmq/package-lock.json).
- Redis 7.4.9 standalone, image
  `redis:7.4.9-alpine@sha256:6ab0b6e7381779332f97b8ca76193e45b0756f38d4c0dcda72dbb3c32061ab99`.
- RESP2, default `bull` prefix, DB 0, standard FIFO `Queue.add`, custom or
  generated ID, retained jobs. No Worker is required: independent Redis reads
  establish existence, not processing. No Cluster/Sentinel/Valkey claim.
- Recognize only `addStandardJob-9` SHA1
  `808626431266f2e834f108cac6b35ce556927132`, via EVAL content hash or EVALSHA.
  Exactly 9 keys and 3 arguments: `bull:<queue>:` followed by `wait`, `paused`,
  `meta`, `id`, `completed`, `delayed`, `active`, `events`, `marker`, in that
  order. Queue is 1–128 ASCII letters/digits/underscore/hyphen/dot. Other
  scripts/key layouts pass through without semantic matching. Arguments are
  opaque and never logged; the proxy cannot identify the originating library
  API from an identical wire command. Advanced options using this same script
  are not independently qualified; success never implies a newly created job.

## Observed wire trace

[trace.cjs](../../examples/bullmq/trace.cjs) is a fixture-only wire observer,
not the production parser. It prints names/counts/types/hash, never argument
values, job IDs, credentials, or payloads.

| Case | Observed commands/replies |
| --- | --- |
| Startup | CLIENT SETINFO twice; INFO twice; HSET queue metadata |
| First add on cold client | EVAL, 15 arguments, 9 keys → bulk string |
| Warm add | EVALSHA, same shape/hash → bulk string |
| Existing custom ID | Same EVALSHA → same bulk string ID; waiting list unchanged |
| Cache flushed after warm add | EVALSHA → NOSCRIPT; client EVAL fallback → bulk string |
| Two concurrent adds | Two EVALSHA requests/replies, FIFO; no MULTI/EXEC |
| Missing parent (negative probe only) | Same script → integer -5; Queue.add rejects |
| Duplicate connection | Independent CLIENT/INFO/PING/QUIT stream |
| Blocking connection | BLPOP with 100ms block → null array |
| Explicit reconnect | New connection repeats handshake and PING successfully |
| ACL/TLS | AUTH before startup; both TLS legs verified with generated CA |

## Outcomes, selector and event lifecycle

`after_job_add` means a recognized script returned a nonempty bulk string job
ID. It covers new and duplicate IDs because the reply cannot distinguish them.
It does not prove business success, job processing, or retry idempotency.
Negative integers, null/empty/unrecognized reply, Redis ERR and NOSCRIPT never
reach the phase. Do not log the reply or classify arbitrary Redis success as add.

One complete recognized script command is one **wire attempt**, with a snapshot
and selector decision captured before forwarding it. Each attempt gets one
flow_started, decision and terminal flow_finished event. NOSCRIPT terminates
not_reached; fallback EVAL is a new attempt and increments selectors again.
Reconnect, client resend and application retry also create new attempts. This
deliberately means nth/every can differ after cache eviction. No cross-connection
application-call correlation is claimed. Reload/enable/disable affect attempts
started afterward; pending attempts retain the captured decision/revision.

Queue name is the only matcher, `match.queue`; blank means any recognized queue.
Operation is `job_add`. Events retain the safe queue identifier and fixed
outcome labels, never job ID, key, script args, options, passwords or ACL user.

## Transport and limits

`protocol: bullmq`, `redis://host:port` or `rediss://host:port`, default port
6379. No URL credentials, database path or query. Only plaintext/plaintext or
TLS/TLS is qualified; reject mixed legs and mTLS configuration. TLS 1.2 minimum,
upstream CA and hostname verification mandatory, no downgrade. AUTH and ACL
AUTH are transparently forwarded; authentication errors are opaque pass-through.

Support bounded RESP2 arrays, bulk/simple strings, integers and errors.
Maximum frame 1 MiB (including encoded framing), depth 8, 4096 children per
array, 8192 nodes per frame, 128 pending commands per connection. Runtime
max_inflight_requests also bounds total sessions and global pending commands.
Overflow/malformed/unsupported framing closes both legs without forwarding the
invalid frame. No unbounded payload buffering. request_timeout bounds handshake,
each complete frame read/write, idle wait, pending reply lifetime and faults.
Blocking commands are only transport pass-through and must complete within that
deadline; they are not Worker semantics.

Each connection has FIFO command/reply correlation. Delay/hold pauses reply
delivery for that connection; later requests may already execute at Redis, but
later replies cannot overtake it. Pending capacity is bounded; exhaustion closes
the connection. Close/hold expiry affects every outstanding operation on that
connection. Separate connections remain independent. Client disconnect, Redis
close and shutdown cancel pending work and finish all started events. Proxy
never reconnects or retries.

Reject MULTI/EXEC/DISCARD/WATCH, HELLO, Pub/Sub subscription commands, MONITOR,
CLIENT REPLY/TRACKING, SELECT and replication commands by closing before
forwarding. These can change response cardinality, protocol or transaction
context. Transactions/addBulk are outside the pinned trace. Other ordinary
RESP2 commands are opaque FIFO pass-through without semantic flows; this is not
generic Redis compatibility.

## Retry fixture contract

Caller command timeout 2s, connect timeout 2s, maxRetriesPerRequest 0,
autoResendUnfulfilledCommands false and retryStrategy returning null: ioredis
never resends or reconnects on its own. Fault tests use nth=1, delay 300ms,
hold 300ms or close. The application retry is explicit: one more `Queue.add`
on a new ioredis connection and Queue, same queue and options. Generated-ID
retry must leave two waiting jobs; known-ID retry must leave one job and its
key. Redis observation uses a separate direct connection before cleanup; each
fixture process has a 15s wall-clock bound. Client resend is disabled, so every
attempt in the evidence comes from the application.

## P28 evidence and review

- `rtk proxy npm install --ignore-scripts --no-audit --no-fund --cache /tmp/faultline-npm-cache`
  installed the pinned dependency tree.
- `rtk proxy node examples/bullmq/trace.cjs`: PASS, normal/cold/cache-miss,
  duplicate, concurrent, semantic-error, duplicate/blocking/reconnect paths;
  independent waiting-list count was five after six successful add calls.
- `rtk proxy env TLS_DIR=/tmp/faultline-bullmq-tls REDIS_PORT=16380 REDIS_AUTH=1 node examples/bullmq/trace.cjs`:
  PASS with TLS listener and upstream, generated localhost certificate and an
  isolated ACL user. An initial ACL launch syntax and an immediate reconnect
  race in the fixture were corrected before the passing rerun.
- P28-AC1–AC8: PASS. No transaction was observed; exclusion is explicit.
  Parser limits, selector semantics and TLS rejection behavior are requirements
  for P29, not claims that the proxy already exists. mTLS and mixed TLS legs
  remain excluded. Review found no remaining P28 blocker.

Sources: [BullMQ job IDs](https://docs.bullmq.io/guide/jobs/job-ids),
[Redis scripting](https://redis.io/docs/latest/develop/interact/programmability/eval-intro/),
and installed pinned BullMQ script/client source, corroborated by the wire trace.
