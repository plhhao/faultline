# P26 TCP/TLS contract

## Route and identity

The application retains its S3 endpoint, TLS verification and SigV4 signing. Only its Docker container maps the exact hostname used on the wire to Faultline's address. The Faultline container has ordinary DNS and dials `tcp://<original-host>:443`. TCP bytes, including the ClientHello, pass unchanged; Faultline does not parse TLS, HTTP or S3. The upstream certificate is verified by the SDK against its original hostname. Path-style and virtual-hosted requests can use different hostnames, and redirects or alternate S3 endpoints require separate routing checks. A bucket name containing a dot can affect virtual-hosted TLS.

## Configuration

`protocol: tcp` requires `upstream: tcp://host:port`, an empty matcher, exactly one selector and no `tls` or `upstream_tls`. `upstream_protocol` defaults to `tcp`. The proxy cannot point at its own normalized listener address. A connection is one eligible flow and receives one decision; retries and keep-alive requests may have different connection counts. Rule reload/enable affects connections accepted afterward. Listener, upstream and runtime changes require restart.

| Action | Phase | Required parameter | Trigger | Effect |
| --- | --- | --- | --- | --- |
| `close_connection` | `on_transfer` | None | immediate, `after_duration`, or `after_bytes` + `trigger_direction` | Close both TCP legs. Ordinary close/EOF is possible; RST is not promised. |
| `hold` | `on_transfer` | `direction`, `max_duration` | Same trigger options | Stop forwarding the selected direction. Close both legs after the bound. |
| `throttle` | `on_transfer` | `direction`, `bytes_per_second` | Same trigger options | Pace selected direction in chunks no larger than 1024 bytes. |
| `delay_connect` | `on_connect` | `duration` | On accept | Delay dial to upstream. Client bytes read during delay are buffered up to 16 KiB; early client close is detected while reading. |

Directions are `client_to_upstream` and `upstream_to_client`. A byte trigger counts bytes **successfully forwarded** in `trigger_direction`, including TLS handshake and framing. Threshold zero activates immediately. A time trigger starts after upstream dial. At most one trigger field is accepted. The byte count cannot identify S3 body bytes or an HTTP response boundary; bytes already forwarded cannot be recalled. A trigger that is not reached emits `not_reached` at flow end.

`runtime.max_inflight_requests` limits simultaneous TCP connections per TCP server. Extra accepts are closed. `runtime.request_timeout` bounds each connection and upstream dial; long uploads need a larger value. Each pump holds one 4096-byte buffer; dial delay can hold a further 16 KiB of client bytes. Shutdown closes listeners and active sockets, then waits for pumps. The upstream is never retried by Faultline.

Recorder flow events include proxy/rule/revision, selection, reached/applied/not_reached, outcome and forwarded byte counts by direction. They do not include payload, SNI, signed URLs, bucket/key or credentials. `fault_applied` means Faultline performed its TCP action, not that S3 accepted an object.

## Local evidence

`TestTLSPassthroughAndCertificate` connected a TLS client through the TCP adapter to a local TLS server with certificate verification enabled; an untrusted root failed. `TestTCPFaults` exercised immediate disconnect, byte cut, hold in both directions, throttle in both directions, and dial delay against a local TLS fixture. `TestThrottlePacesForwardedBytes` measured the rate with an in-memory connection. The affected tests passed with the race detector after the final delay prefetch change; details are in [acceptance](acceptance.md). AWS/SigV4 behavior remains gated by the opt-in Docker test.
