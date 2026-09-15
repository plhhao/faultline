# gRPC unary, HTTP/2 and optional mTLS

`protocol: grpc` accepts native unary gRPC on HTTP/2. The proxy forwards opaque
message bytes; application protobuf definitions or generated code are not needed.
Client/server/bidirectional streaming and gRPC-Web are outside the verified scope.
The included `faultline.demo.Echo/Call` fixture uses `google.protobuf.BytesValue`.

## Run the unary example

Start each long-running command in its own terminal from the repository root:

```sh
rtk proxy go run ./examples/grpc/unary/cmd -mode server -address 127.0.0.1:9000
rtk proxy go run ./cmd/faultline serve --config examples/grpc/faultline.yaml
rtk proxy go run ./examples/grpc/unary/cmd -mode client -address 127.0.0.1:8080 -size 262144
rtk proxy go run ./cmd/faultline enable
```

Repeat the client command: every second eligible RPC has its response cut after
1024 wire body bytes. Startup is disabled and does not consume the selector.
The client checks the full payload and prints headers, trailers, elapsed time and
RPC errors. Use `-timeout 500ms` to observe a deadline on slow/held traffic.

To test upload instead, change the fault to:

```yaml
fault: {action: truncate, direction: request, phase: before_upstream_request, bytes: 1024}
```

For a slow download or upload, use `action: throttle`, replace `bytes` with
`bytes_per_second: 65536`, and set direction/phase as above. Reload rules with:

```sh
rtk proxy go run ./cmd/faultline reload --config examples/grpc/faultline.yaml
```

## Protocol and capability contract

| Setting | Behavior |
| --- | --- |
| `protocol: http1` | HTTP/1.1 listener; existing configs retain their behavior |
| `protocol: http2` | HTTP/2 listener; without TLS, use prior knowledge, not HTTP/1 Upgrade |
| `protocol: grpc` | Native gRPC, HTTP/2 on both legs |
| `upstream_protocol: http1\|http2` | Defaults to listener HTTP version; gRPC requires http2 |
| `match.service`, `match.method` on gRPC | Exact fully qualified service and method from `/service/method` |
| `match.path`, `match.headers` | Exact path and metadata/header matching; conditions combine with AND |

TLS advertises only the configured protocol. An HTTP/2 upstream which does not
negotiate h2 fails; the proxy does not silently send HTTP/1.1. Each flow owns a
fresh upstream connection and makes one attempt, including REFUSED_STREAM errors.
This adds handshake cost. Client connections support concurrent HTTP/2 streams;
cancellation, deadline and faults end the chosen stream. Resource contention can
still affect timing across streams. Retries performed by the application are new
flows and remain visible to selectors.

| Action | HTTP/1.1 | HTTP/2 | gRPC unary |
| --- | --- | --- | --- |
| delay | Both phases | Both phases | Both phases |
| respond | Before request | Before request | Rejected |
| close_connection | Both phases | Rejected | Rejected |
| hold_request / hold_response | Close client connection after hold | End chosen stream after hold | End chosen RPC stream after hold |
| truncate / throttle | Request or response body | Request or response body | Request or response wire body |

Hold cancels upstream immediately. A gRPC flow preserves upstream status/trailers;
natural RPC failures are recorded as `upstream_rpc` with `grpc_status`. Transport
failure before headers returns gRPC UNAVAILABLE, overload RESOURCE_EXHAUSTED;
incomplete streamed responses abort the stream. Delay consumes the RPC deadline,
and the forwarded `grpc-timeout` is reduced by time already spent in the proxy.

Truncate forwards at most N bytes (`bytes >= 0`) and probes one additional byte.
Only a confirmed cut is applied; empty/short/exact-length bodies are not marked
truncated. Existing Content-Length is retained, and incomplete chunked bodies do
not get a terminating chunk. HTTP/1 close-delimited framing alone cannot always
prove truncation to a client. With gRPC, N includes the 5-byte message prefix and
compressed payload as transmitted; cutting mid-message produces an RPC error.

Throttle uses a positive `bytes_per_second` per flow and direction. Each read is
at most `min(16384, max(1, rate/10))` bytes and waits `bytes/rate` before returning
those bytes; unused credit is not accumulated. Burst is bounded by one such
chunk. Headers and transport overhead are excluded. Slow sources/backpressure
can reduce actual throughput. One flow selects one action and one direction.

## TLS and mTLS

Both legs are independent. `tls.cert_file/key_file` enables listener TLS.
Adding `tls.client_ca_file` requires and verifies client certificates. Without
it, clients need no certificate. `upstream_tls.ca_file` extends system server
trust; `upstream_tls.cert_file/key_file` supplies the proxy's client identity.
That pair may be used with system roots alone. Upstream hostname verification
is always enabled. Upstream authenticates Faultline, not the original app.

For a local demo only, generate a short-lived self-signed certificate shared by
all participants (production identities should use separate keys):

```sh
rtk proxy mkdir -p examples/grpc/tls
rtk proxy openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost -addext 'subjectAltName=DNS:localhost,DNS:host.docker.internal,IP:127.0.0.1' -addext 'extendedKeyUsage=serverAuth,clientAuth' -keyout examples/grpc/tls/key.pem -out examples/grpc/tls/cert.pem
rtk proxy go run ./examples/grpc/unary/cmd -mode server -address 0.0.0.0:9000 -cert examples/grpc/tls/cert.pem -key examples/grpc/tls/key.pem -client-ca examples/grpc/tls/cert.pem
rtk proxy go run ./cmd/faultline serve --config examples/grpc/mtls.yaml
rtk proxy go run ./examples/grpc/unary/cmd -mode client -address 127.0.0.1:8080 -ca examples/grpc/tls/cert.pem -cert examples/grpc/tls/cert.pem -key examples/grpc/tls/key.pem
```

The generated `tls/` directory is ignored by Git. Removing listener
`client_ca_file` gives ordinary listener TLS; removing upstream cert/key gives
ordinary upstream TLS when its server does not require mTLS. TLS paths resolve
relative to the declaring YAML file, including includes. Protocol, TLS fields,
CA/cert/key paths and contents require restart. Rule reload does not rotate
certificates. Missing, invalid or mismatched files fail validation; rejected
reload preserves the active snapshot. Listener handshake failures go to stderr
without creating fault flows. No certificate identity policy or hot reload is
implemented.

## Docker

Keep the demo upstream running on the host. Build the existing image and mount
the config/certificate directory read-only. UID 65532 must have permission to
read/traverse it; restrict real private keys using owner/group permissions.

```sh
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:local .
rtk proxy docker run --rm --name faultline-grpc --add-host host.docker.internal:host-gateway -p 127.0.0.1:8080:8080 -v "$PWD/examples/grpc:/config:ro" faultline:local serve --config /config/docker.yaml
rtk proxy docker exec faultline-grpc /faultline enable
rtk proxy docker exec faultline-grpc /faultline reload --config /config/docker.yaml
```

Run the same TLS client against port 8080. Check application readiness with a
successful RPC before enabling injection; admin readiness alone does not prove
upstream or Docker port readiness. Tests generate temporary certificates and
exercise binary/Docker, admin reload, and both actions in both directions:

```sh
rtk proxy go test ./tests/integration -run '^TestExtensionBinary$' -v -count=1
rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestExtensionDocker$' -v -count=1 -timeout 360s
```
