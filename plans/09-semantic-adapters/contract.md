# PostgreSQL semantic contract (P23)

## Versions and scope

PostgreSQL **17.11-bookworm**, wire protocol **3.0**, fixture **pgx v5.7.6**.
Pin a PostgreSQL 17 maintenance image for reproducibility; pin pgx for its ordinary
(non-pipeline) simple/extended query and SCRAM behavior.

| Capability | Contract |
| --- | --- |
| Direct connections, client-side pools | Supported; one upstream connection per client |
| Simple query, extended Parse/Bind/Describe/Execute/Sync, prepared statements | Forwarded; sequential command cycles |
| SCRAM-SHA-256 | Relayed unchanged; no password stored in config |
| TLS | PostgreSQL SSLRequest, TLS >=1.2, independently on each leg; configured listener requires TLS; postgresqls:// requires verified upstream TLS, no fallback |
| SCRAM-SHA-256-PLUS | Rejected explicitly, mechanisms never stripped; pgx fixture uses non-binding SCRAM; other clients must explicitly disable channel binding |
| Cancellation | Proxy-issued cancel key only; cancels local waits, closes the session and forwards the original key to the configured upstream; outcome is never assumed rollback |
| COPY, replication, pipeline, GSS, protocol !=3.0 | Rejected/connection closed; COPY may have started upstream, no claim about side effects |
| SQL/parameters/result editing, arbitrary SQL matching, mTLS | Unsupported |

## Flow and faults

A flow is a **command cycle**, first frontend query/extended message through
ReadyForQuery. Snapshot is captured at the first message. Extended parse/describe
cycles without execution are also flows. First eligible rule and one fault per flow.
Only a cycle with backend **CommandComplete("COMMIT")**, after an explicit transaction
has been observed, is eligible. No SQL string search. ROLLBACK and ErrorResponse do
not prove commit. BEGIN and COMMIT should be separate sequential cycles; batches,
multiple Execute per cycle, and multiple transaction boundaries in one simple Query
are outside semantic guarantees. No auto-commit acceptance claim.

Only phase **after_commit** is exposed. Actions: **delay** (duration then deliver
acknowledgment), **hold_response** (max_duration then close session),
**close_connection** (close both legs immediately). The COMMIT CommandComplete frame
is withheld before any byte of it is sent. Earlier notices/results may already have
been forwarded. Database confirmation and client acknowledgment are separate facts.
A selected fault does not imply application rollback or durable replication guarantees.

Rule match must be empty; select probability/nth/every count eligible commit cycles.
Disabled injection and probability zero forward normally. Reload affects next cycle
on an existing connection; current cycle retains revision, selection and enabled state.
Disable does not release an active hold. No-op reload preserves counters.

## Bounds and lifecycle

Startup <=10 KiB, individual frame <=1 MiB; at most two retained frames per direction plus
the current frame and write copy (approximately 6 MiB of payload buffers per session); result sets stream rather than accumulate. Session count bounded by
runtime.max_inflight_requests; up to 16 additional connections permit bounded startup/cancellation (independent of HTTP request slots). Startup, idle time
and each command cycle bounded by runtime.request_timeout. Fault waits are capped by
that deadline. Client EOF, valid cancellation and shutdown close both legs and end waits.
No SQL, parameters, authentication payload, cancel keys or backend error text in events.
Malformed protocol receives a generic error or is closed, never echoed.

## Traces and evidence

```text
BEGIN -> CommandComplete BEGIN -> ReadyForQuery T
INSERT -> CommandComplete INSERT -> ReadyForQuery T
COMMIT -> CommandComplete COMMIT -> [after_commit fault] -> ReadyForQuery I
                                  independent connection: row exists
ROLLBACK -> CommandComplete ROLLBACK -> ReadyForQuery I (no eligibility)
failed transaction -> COMMIT -> CommandComplete ROLLBACK (no eligibility)
deferred constraint failure -> COMMIT -> ErrorResponse -> ReadyForQuery I (no eligibility)
```

Fixture uses explicit transactions and verifies through a separate direct connection.
Retry demo compares two ordinary inserts against a unique operation ID with ON CONFLICT.
Client errors alone never determine database outcome. Tests retain sanitized events,
row counts and elapsed durations; real wire trace assertions use backend tags, not SQL.

## Sources

- [PostgreSQL 17 message flow](https://www.postgresql.org/docs/17/protocol-flow.html)
- [Message formats](https://www.postgresql.org/docs/17/protocol-message-formats.html)
- [SASL authentication](https://www.postgresql.org/docs/17/sasl-authentication.html)
- [Pinned pgx SCRAM implementation](https://github.com/jackc/pgx/blob/v5.7.6/pgconn/auth_scram.go)
