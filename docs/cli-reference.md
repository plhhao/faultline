# CLI reference

The root command is `faultline`. Every argument is a flag; positional arguments
are not accepted. Run `faultline --help` or `faultline <command> --help` for the
exact usage of the binary in use.

```text
faultline <validate|serve|reload|enable|disable|status|user|configure> [flags]
```

`--help` and `-h` exit successfully. Syntax errors, invalid flags, and missing
required flags exit with status 1. Examples use `./bin/faultline`; replace it
with an installed binary when appropriate.

## Shared flags

| Flag | Default | Used by | Meaning |
| --- | --- | --- | --- |
| `--config FILE` | none | `validate`, `serve`, `reload`, `configure` | Required root YAML file. It may use `include`; `reload` sends an absolute path to the serving process. |
| `--admin-socket PATH` | `/tmp/faultline-<uid>/admin.sock` | `serve`, `status`, `enable`, `disable`, `reload` | File-mode instance socket. Use the same value for `serve` and runtime commands. Parent is private `0700`; socket is `0600`; an existing socket is never overwritten. |
| `--timeout DURATION` | `5s` | runtime commands | Wait time for a local admin command. Must be positive. A timed-out mutation might have succeeded, so use `status` before retrying. |

## Commands

### `validate`

```sh
./bin/faultline validate --config FILE
```

Validates the complete YAML/include tree and referenced TLS files. It does not
open listeners, contact an upstream, or create an admin socket. A valid config
prints `Configuration valid` to stdout.

### `serve`

```sh
./bin/faultline serve --config FILE [flags]
```

Loads configuration, opens proxy listeners and the Unix admin socket, and writes
NDJSON events to stdout. Diagnostics go to stderr. Injection starts disabled.
Ctrl+C/SIGTERM stops new flows, drains HTTP for up to five seconds, then cancels
remaining flows; PostgreSQL/MySQL sessions close immediately.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--start-enabled` | `false` | Enables injection after listeners are ready. |
| `--event-buffer N` | `1024` | Maximum queued JSON events, excluding one being written. Must be positive; a full queue drops new events and increments `dropped_events`. |
| `--data-dir DIR` | empty | Enables managed mode and persistent config, accounts, and audit data in a private `0700` directory. |
| `--api-listen HOST:PORT` | `127.0.0.1:8443` | Managed UI/API HTTPS listener; used only with `--data-dir`. |
| `--api-cert PEM` | empty | Required managed UI/API certificate. |
| `--api-key PEM` | empty | Required managed UI/API private key. |

Managed mode uses the file only to bootstrap the first revision. Later starts
restore `--data-dir`; the Unix socket permits only `status`, while the HTTPS
UI/API controls rules and injection.

### `status`

```sh
./bin/faultline status [--admin-socket PATH] [--timeout DURATION]
```

Prints readiness, revision, injection state, rule counters, recorder counters,
and active fault flows as JSON. It does not change state.

### `enable` and `disable`

```sh
./bin/faultline enable [--admin-socket PATH] [--timeout DURATION]
./bin/faultline disable [--admin-socket PATH] [--timeout DURATION]
```

Toggle global injection for new flows and print JSON results. Both are
idempotent. `disable` does not end existing delay or hold faults. They are
rejected via the Unix socket in managed mode.

### `reload`

```sh
./bin/faultline reload --config FILE [--admin-socket PATH] [--timeout DURATION]
```

The serving process reads the root config and includes from its own filesystem.
Failure preserves the old snapshot; success publishes atomically. Only rules and
seed may change. Listener, protocol, upstream, TLS, and runtime require restart.
Managed mode rejects reload; use its UI/API or stopped-instance `configure`.

### `user`

```sh
printf '%s' "$faultline_password" | ./bin/faultline user \
  --data-dir DIR --name NAME [--role viewer|editor]
./bin/faultline user --data-dir DIR --name NAME --delete
```

Creates or updates a managed account. Password input must be piped; direct
terminal stdin is rejected. Changes and deletion revoke current sessions.
`--name` accepts 1–64 letters, digits, `.`, `_`, or `-`; passwords are 12–1024
bytes. `viewer` reads state; `editor` can edit/apply rules and control injection.

### `configure`

```sh
./bin/faultline configure --data-dir DIR --config FILE
```

Replaces the complete managed configuration for listener, upstream, TLS, or
runtime changes. The instance must be stopped because its data lock rejects a
concurrent update. Equivalent config is a no-op; changed config increments the
revision and the next `serve` starts with injection disabled.
