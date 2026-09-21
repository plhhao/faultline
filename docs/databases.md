# PostgreSQL và MySQL

Hai adapter database mô phỏng database đã xác nhận explicit `COMMIT` nhưng client
không nhận ACK. Đây là test retry/idempotency, không phải bằng chứng transaction
đã rollback.

| | PostgreSQL | MySQL |
| --- | --- | --- |
| Protocol | `postgresql` | `mysql` |
| Upstream scheme | `postgresql://`, `postgresqls://` | `mysql://`, `mysqls://` |
| Default port | 5432 | 3306 |
| Phase | `after_commit` | `after_commit` |
| Actions | delay, hold_response, close_connection | delay, hold_response, close_connection |
| Match | `{}` | `{}` |

Ví dụ MySQL:

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

Đổi app DSN sang listener Faultline, nhưng giữ kết nối direct riêng để kiểm tra
dữ liệu sau lỗi client.

- `delay` gửi ACK sau `duration`; client có thể timeout trước đó.
- `hold_response` giữ ACK tới `max_duration`, sau đó đóng connection.
- `close_connection` đóng ngay sau upstream xác nhận COMMIT.
- Disable/reload không giải phóng fault đã bắt đầu; command cycle mới nhận snapshot mới.
- Autocommit, implicit commit, hay COMMIT ngoài grammar hỗ trợ không là điểm inject.

Faultline không retry commit. Nếu application retry cùng operation sau lỗi, bảng
thường có thể có 2 row; unique operation ID/idempotency có thể giữ lại 1 row.

Chạy demo tại [PostgreSQL](../examples/postgresql/README.md) hoặc
[MySQL](../examples/mysql/README.md). MySQL đã kiểm chứng `caching_sha2_password`,
plaintext/plaintext hoặc TLS/TLS; TLS chỉ một chặng bị từ chối. Chi tiết giới hạn:
[MySQL contract](../plans/09-semantic-adapters/mysql-contract.md) và
[PostgreSQL contract](../plans/09-semantic-adapters/contract.md).
