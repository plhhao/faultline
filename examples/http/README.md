# HTTP configuration

For a complete scenario, use the [payment lost-response demo](paymentdemo/README.md).
[faults.yaml](faults.yaml) shows all five actions and probability/nth/every
selectors on separate paths; enable injection after application readiness.

[faultline.yaml](faultline.yaml) is a complete, validated configuration example.
Use `faultline validate --config examples/http/faultline.yaml` to validate it,
or `faultline serve --config examples/http/faultline.yaml` to forward traffic
from port 8080 to an upstream on port 9000. Injection starts disabled. Add
`--start-enabled` to execute configured faults immediately. All five MVP actions
are implemented; see [action behavior](../../README.md#available-faults).

Multi-file loading is implemented in [P01a](../../plans/01-core/04-multi-file-config.md).
See the runnable loader example [multi-file/faultline.yaml](multi-file/faultline.yaml).
The root supports `include: ["proxies/*.yaml"]`; fragments contain only
`proxies`. Duplicate proxy IDs across files are errors; duplicate rule IDs
are errors within a proxy. Certificates resolve relative to the declaring file.
See [specification section 7.1](../../specific.md) for ordering and reload rules.
`faultline.yaml` remains a working single-file parser example.

Use `config.Load(rootFilename)` for either layout. `config.Parse(data, filename)`
accepts standalone YAML and rejects `include`. In the control service,
`ReloadFile(rootFilename)` rereads the complete file set; `Reload(data, filename)`
remains the standalone API. CLI `reload --config FILE` sends the absolute root
path over the admin socket; the serving process rereads the entire tree and
returns the actual applied revision. See [runtime administration](../../README.md#runtime-administration)
for socket selection, permissions, timeout and counter scopes.

Includes resolve relative to the root; inline proxies come first, followed by
include-list order and lexically sorted glob matches. Missing files, empty glob
matches, directories, nested includes and repeated physical files (including
symlinks/hard links) are errors. Each fragment needs a nonempty `proxies` list.
Diagnostics include source file and field path; duplicate IDs identify both
declarations. Finish editing all files before reloading: snapshot publication
is atomic, filesystem edits across multiple files are not a transaction.

## Defaults and validation

| Field | Default or requirement |
| --- | --- |
| `api_version` | Required: `faultline/v1alpha1` |
| `seed` | `0`; unsigned 64-bit integer |
| `runtime.max_inflight_requests` | `1000`; positive integer |
| `runtime.request_timeout` | `30s`; positive duration |
| `proxies` | At least one; `protocol: http1|http2|grpc`, listener and upstream required |
| Listener host | `:8080` becomes `127.0.0.1:8080`; ports must be 1–65535 |
| `rules` | Empty list when omitted |
| `rule.enabled` | `true`; process injection still starts disabled |
| `match` | Empty matcher when omitted; conditions combine with AND |
| `select` | Exactly one of probability `[0,1]`, positive `nth` or `every` |
| `fault` | Explicit action/phase and action-specific parameters required |
| `respond.body` | Empty string; status must be 200–599 |

`delay` requires a positive `duration`; `hold_request`/`hold_response` require a
positive `max_duration`. The flow's `request_timeout` can end either wait earlier.
`respond` sends no body bytes for HEAD or 204/205/304, even if configured. For HEAD
with a body-permitted status, Content-Length describes the configured body; 205
uses Content-Length 0. `close_connection` takes no timing or response parameters.

These initial resource defaults are configuration values, not measured capacity
or a throughput SLA. The HTTP adapter enforces a process-wide inflight limit
(503 on overflow) and request deadlines, including body I/O and hooks.

IDs use ASCII letters, digits, dot, underscore or hyphen and are unique within
their scope. Header names are case-insensitive; values are exact strings. Quote
numeric header values. For repeated headers, any individual value may match;
values are not implicitly joined. Method matching is case-sensitive and exact
path matching excludes the query string.

The parser accepts one YAML document per file and rejects unknown/duplicate fields,
nulls, aliases, merge keys, implicit string/boolean coercion, unsupported
protocols and invalid action/phase/parameter combinations. Errors identify the
field without echoing its value. Boolean values use `true` or `false`.

## TLS files

Listener TLS requires `tls.cert_file` and `tls.key_file`; upstream custom trust
uses `upstream_tls.ca_file` with an HTTPS origin. Paths resolve relative to the
file declaring that proxy, not the current working directory. Cert/key must parse and
match; the CA file must contain parseable PEM certificates. The TLS adapter
verifies upstream hostname/trust using system CA plus the configured CA, and
negotiates the configured protocol on each side. Optional mTLS uses
`tls.client_ca_file` and/or `upstream_tls.cert_file/key_file`.

Changing TLS settings, file paths or file contents requires restart. Reload
compares the validated file digests even if the file names remain unchanged.
No real private keys are included in this repository; tests generate temporary
certificates.

## Effective configuration

Whitespace, YAML key order, equivalent durations, explicit defaults and proxy
ordering do not create revisions. Headers and upstream origins are normalized;
rule ordering is preserved because it changes precedence. Changing any rule or
seed creates a new revision with fresh rule counters. Listener/protocol/upstream,
TLS and runtime changes require restart and reject the whole reload.

The parser uses [go.yaml.in/yaml/v3](https://pkg.go.dev/go.yaml.in/yaml/v3)
`v3.0.5`, with field-aware decoding to preserve diagnostic paths and avoid
including secret scalar values in errors.

## Phase 6 examples

[http2.yaml](http2.yaml) uses explicit cleartext HTTP/2 with a throttled download.
[body-faults.yaml](body-faults.yaml) exercises truncate/throttle in both directions
on HTTP/1.1; change `protocol` to `http2` for an HTTP/2 upstream.
See the [protocol/capability and mTLS guide](../grpc/README.md) for schema,
byte accounting, edge cases, stream isolation and TLS restart behavior.
