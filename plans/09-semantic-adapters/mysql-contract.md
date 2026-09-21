# MySQL adapter contract — P25

## Baseline pinned

- MySQL 8.4.8, image `mysql@sha256:2952e3be7807f06fc18de50b3ea1a632d5c70d63482ff7d7376fe3aa8999babf` (multi-platform manifest).
- Go driver `github.com/go-sql-driver/mysql v1.10.1`; InnoDB, direct backend.
- Contract evidence 2026-09-17: temporary Docker, loopback dynamic port; driver direct trace records only packet type/sequence/length, never auth bytes or SQL.
- Cold auth: greeting seq0 → auth-more 04 seq2 → RSA key seq4 → OK seq6. Warm auth: auth-more 03 seq2 → OK seq3.
- Prepared SELECT: prepare OK length12, parameter/column definitions; execute column count, definition, binary row, final FE OK. Driver negotiates DEPRECATE_EOF.

## Authentication / transport

`protocol: mysql`, `mysql://host:3306` or `mysqls://host:3306`. Client supplies credentials. URL credentials forbidden.

- Supported matrix: plaintext/plaintext with caching_sha2_password RSA exchange, or TLS/TLS with full/fast caching_sha2_password. Upstream CA and hostname verified, TLS >=1.2.
- Mixed TLS/plaintext legs rejected by config in this slice: passthrough authentication cannot safely translate cleartext TLS password and RSA password responses between legs. No downgrade, no native-password fallback.
- Configured listener TLS required, not opportunistic. No mTLS.
- AuthSwitch only to caching_sha2_password. Unsupported plugins/capability combinations are rejected by closing the connection. No credential logging.

## Command and commit boundary

Each command cycle pins control snapshot. BEGIN / BEGIN WORK / START TRANSACTION and COMMIT / COMMIT WORK are recognized case-insensitively with ASCII whitespace and one optional trailing semicolon. No comments or multi-statement grammar. Prepared versions use bounded statement-ID metadata, never SQL retained.

Inject only after successful COMMIT OK for a tracked explicit transaction, with upstream IN_TRANS cleared. No injection for autocommit, implicit commits, rollback, result-set terminators, or unrecognized COMMIT syntax. A server ERR conservatively clears tracking (including deadlock rollback); later successful statements do not restore it without a fresh explicit BEGIN. This avoids assuming ERR preserves a transaction. COMMIT AND CHAIN/RELEASE is forwarded but not injected.

COM_QUERY, INIT_DB, PING, STMT_PREPARE/EXECUTE/CLOSE/RESET and RESET_CONNECTION are supported. Prepared binary rows and text rows forwarded packet-by-packet; both legacy EOF and DEPRECATE_EOF boundaries supported. No multi-results, query attributes, compression, LOCAL INFILE, cursor fetch, long-data chunks, change-user, replication or arbitrary commands. Unsupported packets close with a generic error. CALL/XA/SQL PREPARE/EXECUTE are outside scope. No SQL/result editing.

A selected commit response is withheld in full. Delay sends ACK after duration; hold waits then closes; close_connection closes immediately. Client failure does not mean rollback. No proxy retries. Disable/reload only affects new cycles, including existing pooled sessions.

## Bounds and lifecycle

- Packet payload <=1 MiB; reject oversized and continuation packets before allocation. TCP fragmentation is reassembled with io.ReadFull; sequence IDs checked modulo256.
- <=128 live prepared statements/session, <=4096 columns/parameters in a response.
- Session bound uses runtime.max_inflight_requests; bounded reader channels, no accumulated result sets.
- Handshake, idle session and command cycle bounded by runtime.request_timeout. Client disconnect/shutdown cancels active hold and closes both legs. No PostgreSQL-style cancel key or backend query cancellation claim.
- Events contain protocol, revision, selection and commit_confirmed, never SQL, parameter or auth values.

## Sources

- [MySQL caching SHA-2 authentication](https://dev.mysql.com/doc/refman/8.4/en/caching-sha2-pluggable-authentication.html)
- [OK packet](https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basic_ok_packet.html)
- [Prepared statement response](https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_com_stmt_prepare.html)
- [Pinned Go driver](https://github.com/go-sql-driver/mysql/tree/v1.10.1)

Implementation acceptance is tracked separately; direct trace is not proof that the adapter passes.
