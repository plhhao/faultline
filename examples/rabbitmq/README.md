# RabbitMQ publisher-confirm fault

This example targets RabbitMQ **4.2.9** and AMQP 0-9-1. Faultline forwards a
persistent client connection and can delay, hold, or close it after RabbitMQ has
sent a successful publisher confirm but before the publisher receives it.

Run a disposable broker:

```sh
docker run --rm --name faultline-rabbitmq -p 127.0.0.1:5672:5672 \
  rabbitmq:4.2.9-management@sha256:e002010fddc214d38f7c8acf058a2fb62350daa10316431cd2cf959f42761e34
```

Build and start Faultline in another terminal:

```sh
go build -o bin/faultline ./cmd/faultline
./bin/faultline validate --config examples/rabbitmq/config.yaml
./bin/faultline serve --config examples/rabbitmq/config.yaml \
  --admin-socket /tmp/faultline-rabbitmq/admin.sock
```

Enable injection only after the publisher and broker are ready:

```sh
./bin/faultline enable --admin-socket /tmp/faultline-rabbitmq/admin.sock
```

Point an AMQP 0-9-1 publisher at `amqp://guest:guest@127.0.0.1:15672/`, enable
publisher confirms, and publish to routing key `payment.created`. With the
default rule the proxy closes the connection after RabbitMQ has confirmed the
publish. The publisher must reconnect; retry/idempotency is owned by the
application, never by Faultline.

Use either fault to change the observation:

```yaml
fault: {action: delay, phase: after_publish_confirm, duration: 500ms}
fault: {action: hold_response, phase: after_publish_confirm, max_duration: 2s}
```

`routing_key` and `exchange` are exact matchers; leave either blank to match any
value. A `basic.ack` from a consumer is not a publisher confirm and cannot be
used as this phase. When RabbitMQ sends one `basic.ack` with `multiple=true`,
Faultline gates that whole confirm frame; the fault can therefore affect other
confirms on the same connection.

TLS is independently configurable per leg; the fixture verifies both legs TLS
together with `amqps://` upstream, listener `tls.cert_file`/`tls.key_file`, and
`upstream_tls.ca_file`. Client and upstream certificates for mTLS are
deliberately rejected in this release. AMQP 1.0,
Streams, cluster/federation, payload matching/editing, and automatic proxy retry
are out of scope.

Run the opt-in Docker fixture:

```sh
FAULTLINE_RABBITMQ_TEST=1 go test -race ./tests/integration \
  -run '^TestRabbitMQ' -count=1 -timeout 240s
```

The retry fixture `TestRabbitMQPersistentChannelsAndRetry` publishes through
Faultline, loses its confirm, disables injection, reconnects and republishes.
An independent broker connection observes **two messages**: the original and
the application's retry. AMQP publisher confirms do not deduplicate a retry;
applications must supply their own idempotency policy. The fixture also checks
heartbeats, two channels, channel reuse, consumer nack/requeue and shutdown.

To include the non-root Faultline Docker image (build, CLI enable, lost confirm,
independent message observation and clean stop), run:

```sh
FAULTLINE_DOCKER_TEST=1 FAULTLINE_RABBITMQ_TEST=1 go test -race ./tests/integration \
  -run '^TestRabbitMQContainerRuntime$' -count=1 -timeout 240s
```

Confirm delay gates subsequent server-to-client frames, including other
channels and heartbeats. A delay longer than the client's heartbeat tolerance
can therefore disconnect it. Exact frame-order tests use a controlled AMQP
peer; broker tests verify the client-visible behavior. See the
[acceptance evidence](../../plans/10-rabbitmq-adapter/rabbitmq-acceptance.md).
