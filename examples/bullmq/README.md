# BullMQ job-add reply fault

This example targets BullMQ **5.81.5** with ioredis **5.8.2** on standalone
Redis **7.4.9** (RESP2, DB 0, default `bull` prefix). Faultline forwards a
persistent Redis connection and can delay, hold, or close it after Redis has
returned a job ID for `Queue.add` but before the caller receives it.

Run a disposable Redis:

```sh
docker run --rm --name faultline-redis -p 127.0.0.1:6379:6379 \
  redis:7.4.9-alpine@sha256:6ab0b6e7381779332f97b8ca76193e45b0756f38d4c0dcda72dbb3c32061ab99
```

Build and start Faultline in another terminal:

```sh
go build -o bin/faultline ./cmd/faultline
./bin/faultline validate --config examples/bullmq/config.yaml
./bin/faultline serve --config examples/bullmq/config.yaml \
  --admin-socket /tmp/faultline-bullmq/admin.sock
./bin/faultline enable --admin-socket /tmp/faultline-bullmq/admin.sock
```

Point the BullMQ `Queue` connection at `127.0.0.1:16379` and add a job to queue
`orders`. With the default rule the first confirmed add loses its reply: the
caller gets a connection error, yet the job is already in Redis. Retry and
idempotency are owned by the application, never by Faultline. With a generated
job ID a retry creates a second job; with a fixed `jobId` BullMQ returns the
existing job.

Use either fault to change the observation:

```yaml
fault: {action: delay, phase: after_job_add, duration: 500ms}
fault: {action: hold_response, phase: after_job_add, max_duration: 2s}
```

## What is recognized

Only the pinned `addStandardJob` script (SHA1
`808626431266f2e834f108cac6b35ce556927132`, via `EVAL` or `EVALSHA`) with its
nine `bull:<queue>:*` keys is a semantic flow. Other commands pass through in
FIFO order without events. Delayed, prioritized, parent/flow jobs, `addBulk`
and other BullMQ versions use different scripts and are not matched.

- A job ID reply reaches `after_job_add`, including an existing custom job ID.
  Negative codes, `NOSCRIPT`, errors and null replies do not.
- Each script command is one selector attempt. After a script-cache miss the
  client's `EVAL` fallback is a new attempt, so `nth`/`every` counts it again.
- Delay or hold pauses all later replies on that connection; close affects every
  outstanding command on it. Separate connections are independent.
- Connections idle longer than `runtime.request_timeout` are closed, as with the
  database adapters; ioredis reconnects. Blocking commands must finish within
  the same timeout.
- `MULTI`/`EXEC`, `HELLO` (RESP3), Pub/Sub, `MONITOR`, `SELECT`, `CLIENT
  REPLY`/`TRACKING` and replication commands close the connection.

Events carry the queue name and fixed outcomes only; job IDs, script arguments,
payloads and credentials are never recorded. `AUTH` is forwarded unchanged.

TLS requires both legs: listener `tls.cert_file`/`tls.key_file` with a
`rediss://` upstream, verified against the system pool or
`upstream_tls.ca_file`. Mixed plaintext/TLS legs and mTLS are rejected.
Redis Cluster/Sentinel, Valkey, Workers/QueueEvents and payload matching are
out of scope.

## Fixtures

Install the pinned client once, then run the opt-in Docker fixtures:

```sh
(cd examples/bullmq && npm ci --ignore-scripts)
FAULTLINE_BULLMQ_TEST=1 go test -race ./tests/integration \
  -run '^TestBullMQ' -count=1 -timeout 240s
```

[`trace.cjs`](trace.cjs) re-records the wire trace used by the contract;
[`fixture.cjs`](fixture.cjs) drives one add, an optional retry on a new
connection, and a direct Redis observation.
