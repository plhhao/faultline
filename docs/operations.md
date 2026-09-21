# CLI operations

All runtime commands must use the same Unix socket as `serve`:

```bash
./bin/faultline status --admin-socket /tmp/faultline/admin.sock
./bin/faultline enable --admin-socket /tmp/faultline/admin.sock
./bin/faultline disable --admin-socket /tmp/faultline/admin.sock
./bin/faultline reload --config /absolute/path/config.yaml \
  --admin-socket /tmp/faultline/admin.sock
```

The default socket is `/tmp/faultline-<uid>/admin.sock`. After a crash, remove
a stale socket only after confirming that its process has stopped. Do not remove
the managed data directory to solve a socket problem.

## Reading `status`

| Field | Meaning |
| --- | --- |
| `ready` | Proxy listeners are open; it does not prove the upstream or application is ready. |
| `config_revision` | Active configuration revision. |
| `injection_enabled` | Global injection state. |
| `revision_rule_counters` | Eligible and selected counts for rules in the active revision. |
| `run_counters` | Process-lifetime totals: total, selected, applied, active, and more. |

`active_fault_flows` can remain nonzero after `disable`: disable affects only
new flows and does not release an existing `hold_response`. Rule counters reset
for a new revision; run counters reset only when the process restarts.

Runtime commands default to a five-second timeout. A timed-out mutation may
already have succeeded, so read `status` before retrying. Nonzero
`dropped_events`, `write_errors`, or `pending_events` means the event artifact
may be incomplete.

## Managed mode

With `--data-dir`, the Unix socket exposes read-only `status`; rule changes and
injection controls are available through the authenticated HTTPS UI/API.

### UI accounts

Create an account or update its password/role with `user`. Pipe the password to
stdin so it is not stored in shell history:

```bash
read -r -s -p "Password: " faultline_password; printf "\n" >&2
printf "%s" "$faultline_password" | ./bin/faultline user \
  --data-dir /path/to/data --name alice --role editor
unset faultline_password
```

`viewer` can only inspect state; `editor` can change rules, apply drafts, and
enable or disable injection. Re-run with the same `--name` to update an account.
Delete an account with:

```bash
./bin/faultline user --data-dir /path/to/data --name alice --delete
```

Deleting or changing an account revokes its existing sessions.

### Infrastructure changes

To change a listener, upstream, TLS, or runtime setting, stop the instance and
then run:

```bash
./bin/faultline configure \
  --data-dir /path/to/data --config /path/to/replacement.yaml
```

`configure` replaces the complete managed configuration and rejects requests
while the instance is running.
