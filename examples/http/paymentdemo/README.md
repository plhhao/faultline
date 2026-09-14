# Payment lost-response demo

Run commands from the repository root with Go 1.26.4. The demo dependency stores
a payment **before** returning 201. Faultline holds that response; the client
times out at 200ms and retries once. The driver queries the dependency directly
to assert **two stored payments without idempotency**, **one with idempotency**.

The store is in memory and resets when its process stops. This is a test fixture,
not a durable payment implementation. Every driver run generates a new operation
ID. Idempotency-Key is used by the demo application; Faultline does not require it.

## Binary

Build both programs:

```sh
rtk proxy go build -trimpath -o bin/faultline ./cmd/faultline
rtk proxy go build -trimpath -o bin/paymentdemo ./examples/http/paymentdemo/cmd
```

Terminal 1, start the dependency:

```sh
rtk proxy ./bin/paymentdemo serve
```

Terminal 2, start Faultline disabled and save its event stream:

```sh
rtk proxy ./bin/faultline serve --config examples/http/paymentdemo/faultline.yaml > /tmp/faultline-payment-events.ndjson
```

Terminal 3, verify application readiness **through the proxy**, then enable and run:

```sh
rtk proxy curl --fail http://127.0.0.1:8080/healthz
rtk proxy ./bin/faultline status
rtk proxy ./bin/faultline enable
rtk proxy ./bin/paymentdemo run
rtk proxy ./bin/paymentdemo run --idempotent
rtk proxy ./bin/faultline disable
```

Both drivers exit 0 when their assertions pass. JSON includes `attempts: 2`,
`timeouts: 2`, and `payments: 2` or `payments: 1`. Inspect the payment directly
using its printed `operation` value:

```sh
rtk proxy curl 'http://127.0.0.1:9000/payments?operation=REPLACE_WITH_OPERATION'
```

Edit `select.probability` in the config, then apply it with
`rtk proxy ./bin/faultline reload --config examples/http/paymentdemo/faultline.yaml`.
Use probability 1 for this driver's timeout assertions. A reload with unchanged
effective config is a no-op; enable/disable does not reset sequence counters.

Stop the two server processes with Ctrl+C after the run. Event lines for
`lost-response` show `selected`, `reached`, `applied`, `upstream_status: 201`,
`phase: after_upstream_headers` and the client cancellation outcome. These prove
what the proxy observed. The direct store query proves this demo's side effect;
201 alone cannot prove a business commit for an arbitrary dependency.

## Docker / OrbStack

Stop the binary servers before reusing their ports. Start the dependency with
`rtk proxy ./bin/paymentdemo serve --listen 0.0.0.0:9000` so the Docker VM can
reach it. Use this test listener only while running the demo.

```sh
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:local .
rtk proxy docker run -d --name faultline-payment --add-host host.docker.internal:host-gateway -p 127.0.0.1:8080:8080 -v "$PWD/examples/http/paymentdemo/docker:/config:ro" faultline:local serve --config /config/faultline.yaml
rtk proxy curl --fail http://127.0.0.1:8080/healthz
rtk proxy docker exec faultline-payment /faultline status
rtk proxy docker exec faultline-payment /faultline enable
rtk proxy ./bin/paymentdemo run
rtk proxy ./bin/paymentdemo run --idempotent
rtk proxy docker exec faultline-payment /faultline reload --config /config/faultline.yaml
rtk proxy docker exec faultline-payment /faultline disable
rtk proxy docker stop --time 10 faultline-payment
rtk proxy docker logs faultline-payment
rtk proxy docker rm faultline-payment
```

This uses the equivalent multi-file config; edit `docker/proxies/payment.yaml`
to change the rule, then reload its root path **inside the container**. Reload
requires all files to be visible and readable there. On VM bind mounts, run
`docker exec faultline-payment /faultline validate --config /config/faultline.yaml`
after edits if the filesystem has not yet exposed a complete update.

## Automated verification

```sh
rtk proxy go test ./tests/integration -run '^TestPaymentDemoBinary$' -v -count=1
rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestContainerRuntime$' -v -count=1 -timeout 360s
```

The tests allocate temporary ports/configs and assert both payment variants and
event evidence. The Docker test builds the delivery image, validates TLS/mounts,
exercises admin commands and removes its own containers/image.
