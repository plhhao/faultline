# Binary and Docker delivery

Build from the repository root with Go 1.26.4:

```sh
rtk proxy go build -trimpath -o bin/faultline ./cmd/faultline
rtk proxy env GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o bin/faultline-linux-arm64 ./cmd/faultline
rtk proxy docker build -f deploy/docker/Dockerfile -t faultline:local .
```

The multi-stage Dockerfile builds from source and pins the Go version. `go.sum`
checks dependencies. Runtime is scratch with the system CA bundle, static binary
and a writable `/tmp` owned by UID/GID 65532. The default user is non-root; there
is no shell or remote admin port. No image or release is published by these commands.
Native macOS arm64 and Docker Linux arm64 are the verification targets; other
architectures need their own runtime checks. Rebuilds may pick up changed base
image contents under the versioned tag; byte-for-byte image reproducibility is
not promised.

## Configuration and administration

Mount the entire configuration tree, including any TLS files, read-only. All
directories and files must be readable/traversable by UID 65532; keep real private
keys restricted to an appropriate owner/group. Cert/key paths resolve relative
to the file declaring the proxy. `listen` must bind `0.0.0.0:8080` in the container;
publish only the needed traffic port. The host and container use different paths.

```sh
rtk proxy docker run --rm -v "$PWD/examples/http/paymentdemo/docker:/config:ro" faultline:local validate --config /config/faultline.yaml
```

The [payment demo](../../examples/http/paymentdemo/README.md#docker--orbstack)
contains a complete run sequence with upstream readiness, `docker exec` admin,
reload, events and cleanup. Its upstream runs on the host; `host.docker.internal`
uses `--add-host host.docker.internal:host-gateway`. A dependency in another
container should instead use that container's DNS name on a shared Docker network.

SIGTERM stops admission, drains accepted flows for up to 5s, cancels the rest,
then flushes recorder events for up to 2s. Allow at least 10s for Docker stop.
Admin socket cleanup is automatic on normal shutdown. The same mounted config
can also be loaded with `validate` before `serve` or `reload`.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Container port unreachable | Listener binds `0.0.0.0`, port is published, container is running. |
| 502 upstream error | Upstream address is reachable from the container; `127.0.0.1` refers to the container itself. |
| TLS validation fails | Mount cert/key/CA tree, grant runtime user read access, verify hostname and trust. No insecure bypass is enabled. |
| Admin socket permission/conflict | Use `docker exec`; custom socket parent must be private. An existing socket is never overwritten. |
| Reload rejected | Read returned source/field error. Missing fragments, duplicate IDs or restart-only changes preserve the active snapshot. |
| Config read immediately after host edit is incomplete | Finish edits, validate from inside the container, then reload; the loader does not provide a cross-file filesystem transaction. |

Run the opt-in delivery test with a working Docker daemon:

```sh
rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestContainerRuntime$' -v -count=1 -timeout 360s
```
