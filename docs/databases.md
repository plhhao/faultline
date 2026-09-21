# PostgreSQL and MySQL

The database adapters model a database that has confirmed an explicit `COMMIT`,
but whose acknowledgement does not reach the client. They test retry and
idempotency behavior; they do not prove that a transaction was rolled back.

| | PostgreSQL | MySQL |
| --- | --- | --- |
| Protocol | `postgresql` | `mysql` |
| Upstream scheme | `postgresql://`, `postgresqls://` | `mysql://`, `mysqls://` |
| Default port | 5432 | 3306 |
| Phase | `after_commit` | `after_commit` |
| Actions | delay, hold_response, close_connection | delay, hold_response, close_connection |
| Match | `{}` | `{}` |

MySQL example:

```yaml
- id: database
  protocol: mysql
  listen: 127.0.0.1:13306
  upstream: mysql://127.0.0.1:3306
  rules:
    - id: lost-commit-ack
      match: {}
      select: {probability: 1}
      fault: {action: close_connection, phase: after_commit}
```

Point the application DSN at the Faultline listener. Keep a separate direct
connection for checking persisted data after a client-side error.

- `delay` sends the acknowledgement after `duration`; the client may time out first.
- `hold_response` withholds the acknowledgement until `max_duration`, then closes the connection.
- `close_connection` closes immediately after the upstream confirms COMMIT.
- Disable or reload does not release a fault that has already started; new command cycles use the new snapshot.
- Autocommit, implicit commits, and COMMIT forms outside the supported grammar are not injection points.

Faultline never retries COMMIT. Retrying the same operation after a lost
acknowledgement can create two rows; an operation ID or idempotency constraint
can retain one.

Run the [PostgreSQL](../examples/postgresql/README.md) or
[MySQL](../examples/mysql/README.md) demo. MySQL has been verified with
`caching_sha2_password` and plaintext/plaintext or TLS/TLS connections; mixed
TLS legs are rejected. See the [MySQL contract](../plans/09-semantic-adapters/mysql-contract.md)
and [PostgreSQL contract](../plans/09-semantic-adapters/contract.md) for limits.
