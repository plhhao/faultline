# Faultline

[![CI](https://github.com/plhhao/faultline/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/plhhao/faultline/actions/workflows/ci.yml)

Faultline is a Go proxy for deterministic network-failure testing. Place it
between an application and a dependency, inject controlled faults, and record
what happened.

Faultline is a development and resilience-testing tool. It is not a production
gateway, load balancer, or general-purpose TCP/database proxy.

## Supported scope

| Area | Supported behavior |
| --- | --- |
| HTTP | HTTP/1.1 and HTTP/2, optional TLS/mTLS, and delay/response/close/hold/truncate/throttle faults. |
| gRPC | Unary gRPC over HTTP/2 with service, method, and metadata matching. Streaming is out of scope. |
| PostgreSQL | Explicit-transaction COMMIT acknowledgement delay, bounded hold, and disconnect faults. |
| MySQL | The same explicit-COMMIT faults for MySQL 8.4.8. |
| Control | File configuration, local Unix-socket control, managed HTTPS UI/API, NDJSON events, and bounded counters. |

Database faults model a COMMIT that the upstream has confirmed but whose
acknowledgement does not reach the client. They are for retry/idempotency tests,
not proof that a transaction was rolled back.

## Quick start

Faultline requires Go 1.26.4.

```sh
git clone https://github.com/plhhao/faultline.git
cd faultline
go build -o bin/faultline ./cmd/faultline
./bin/faultline validate --config examples/http/faultline.yaml
```

Run the upstream demo in one terminal:

```sh
go run ./examples/http/paymentdemo/cmd serve --listen 127.0.0.1:9000
```

Run Faultline in another:

```sh
./bin/faultline serve \
  --config examples/http/faultline.yaml \
  --admin-socket /tmp/faultline-http/admin.sock
```

Injection starts disabled. Enable it only when the application and upstream are
ready:

```sh
./bin/faultline enable --admin-socket /tmp/faultline-http/admin.sock
curl -i http://127.0.0.1:8080/healthz
./bin/faultline status --admin-socket /tmp/faultline-http/admin.sock
```

See the complete [HTTP example](examples/http/README.md), including the
lost-response payment demonstration.

## Documentation

- [HTTP quick start](docs/getting-started.md)
- [Configuration and rules](docs/configuration.md)
- [CLI reference](docs/cli-reference.md)
- [Runtime operations](docs/operations.md)
- [Tester UI](docs/tester-ui.md)
- [PostgreSQL and MySQL](docs/databases.md)
- [Linux/systemd and Docker deployment](docs/deployment.md)

## Operational boundaries

- Injection is disabled at startup unless `serve --start-enabled` is used.
- `reload` atomically changes rules and seed only; listeners, upstreams, TLS,
  protocol, and runtime settings require restart.
- `serve` writes NDJSON events to stdout and diagnostics to stderr. A full event
  queue drops new events and increments `dropped_events`.
- File mode uses a private Unix socket. Managed mode uses an authenticated HTTPS
  UI/API; its Unix socket exposes read-only `status`.
- PostgreSQL/MySQL support only the documented explicit-COMMIT grammar and
  actions. MySQL supports plaintext/plaintext or TLS/TLS legs, not mixed TLS.

## Development

```sh
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

Integration tests bind localhost TCP and Unix sockets. The optional Docker
smoke test requires a running Docker daemon:

```sh
FAULTLINE_DOCKER_TEST=1 go test ./tests/integration \
  -run '^TestContainerRuntime$' -timeout 360s
```

## Contributing and security

Faultline is licensed under the [Apache License 2.0](LICENSE). Read
[CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request and
[SECURITY.md](SECURITY.md) for private vulnerability reporting.

Detailed implementation plans and verification evidence are available in
[plans](plans/README.md); the current product scope is in [specific.md](specific.md).
