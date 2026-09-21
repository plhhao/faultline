# Configuration and rules

Minimal HTTP configuration:

```yaml
api_version: faultline/v1alpha1
runtime:
  request_timeout: 30s
  max_inflight_requests: 100
proxies:
  - id: payment
    protocol: http1
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:9000
    rules:
      - id: slow-payment
        enabled: true
        match: {method: POST, path: /payments}
        select: {probability: 0.25}
        fault: {action: delay, phase: before_upstream_request, duration: 2s}
```

Each flow uses the first matching rule. `enabled: false` skips that rule and
continues evaluation. Global injection must also be enabled through the CLI or
the managed UI.

## Matchers and selectors

Matchers combine with AND. An omitted matcher matches every value for that
field.

| Field | Meaning |
| --- | --- |
| `method` | Exact HTTP method; the original method is still forwarded. |
| `path` | Exact path without its query string. |
| `path_pattern` | Absolute path; `:name` matches one non-empty segment. |
| `headers` | Header/metadata object with exact string values. |
| `service` | Exact gRPC service. |

`path_pattern: /payment/:id` matches `/payment/42` and `/payment/history`, but
not `/payment/42/items`. Put an exact `/payment/history` rule before the pattern
when it is an exception.

Use exactly one selector:

```yaml
select: {probability: 0.2} # 20% of eligible flows
select: {nth: 3}           # third eligible flow
select: {every: 5}         # every fifth eligible flow
```

Probability `0` passes through but still owns the matching flow; it does not
fall through to the next rule. Selectors count only while global injection is
enabled.

## HTTP, HTTP/2, and unary gRPC faults

| Action | Phase | Parameters |
| --- | --- | --- |
| `delay` | before request or after upstream headers | `duration` |
| `respond` | `before_upstream_request` | `status`, `body` |
| `close_connection` | HTTP/1 before request or after headers | none |
| `hold_request` | before request | `max_duration` |
| `hold_response` | after upstream headers | `max_duration` |
| `truncate` | request or response | `direction`, `bytes` |
| `throttle` | request or response | `direction`, `bytes_per_second` |

`truncate` and `throttle` use `before_upstream_request` for request direction
and `after_upstream_headers` for response direction. Unary gRPC does not support
`respond` or `close_connection`. See [the gRPC example](../examples/grpc/README.md)
for HTTP/2, gRPC, and mTLS details.

PostgreSQL and MySQL accept only matcher `{}`, phase `after_commit`, and
`delay`, `hold_response`, or `close_connection` actions.

## Reload or restart

`reload` changes only rules and seed. Listener, protocol, upstream, TLS, and
runtime settings require restart. In-flight flows retain their old snapshot;
the new rule snapshot is published atomically.

```bash
./bin/faultline reload --config /absolute/path/config.yaml \
  --admin-socket /tmp/faultline-http/admin.sock
```

Multi-file configuration uses root-level `include`; see the
[multi-file example](../examples/http/multi-file/faultline.yaml).
