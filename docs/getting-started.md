# HTTP quick start

This example uses an HTTP upstream at `127.0.0.1:9000` and a Faultline listener
at `127.0.0.1:8080`.

## Build and validate

From the repository root:

```bash
go build -o bin/faultline ./cmd/faultline
./bin/faultline validate --config examples/http/faultline.yaml
```

`validate` checks YAML, included files, and referenced TLS files. It does not
open a listener or contact the upstream.

## Start the example

Start the demonstration upstream in terminal 1:

```bash
go run ./examples/http/paymentdemo/cmd serve --listen 127.0.0.1:9000
```

Start Faultline in terminal 2:

```bash
./bin/faultline serve \
  --config examples/http/faultline.yaml \
  --admin-socket /tmp/faultline-http/admin.sock
```

Faultline accepts traffic immediately, but injection starts disabled. Requests
to port 8080 initially pass through unchanged.

## Enable a rule and observe it

In terminal 3:

```bash
./bin/faultline enable --admin-socket /tmp/faultline-http/admin.sock
curl -i http://127.0.0.1:8080/healthz
./bin/faultline status --admin-socket /tmp/faultline-http/admin.sock
./bin/faultline disable --admin-socket /tmp/faultline-http/admin.sock
```

`serve` writes newline-delimited JSON events to stdout. Redirect stdout to
`events.ndjson` to keep an artifact; readiness messages and diagnostics remain
on stderr.

Continue with [Configuration](configuration.md) to change rules and
[CLI operations](operations.md) for safe reloads. The complete upstream demo is
documented in [examples/http](../examples/http/README.md).
