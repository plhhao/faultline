# Phase 09 — Semantic adapters: PostgreSQL và MySQL

Phạm vi: **Sau MVP**. Trạng thái: **In progress — PostgreSQL chờ UI regression; MySQL Done**.

## Phạm vi đã chốt

Ưu tiên Phase 9 trước Phase 8; Phase 8 tiếp tục Deferred. Bổ sung adapter hiểu giao thức PostgreSQL, tái sử dụng engine/control/recorder hiện có. Không chỉ gây lỗi trên TCP rồi suy ra kết quả transaction.

- Fault: delay, hold có giới hạn và ngắt kết nối. Loại fault độc lập với điểm áp dụng.
- Có điểm áp dụng sau khi PostgreSQL xác nhận COMMIT thành công, trước khi chuyển xác nhận về client. Mất xác nhận commit là tình huống timeout/ngắt kết nối tại điểm này, không phải action riêng.
- Kết nối trực tiếp tới PostgreSQL; fixture Go + PostgreSQL Docker. Không thêm fixture Node.js/TypeORM.
- Kiểm chứng SCRAM-SHA-256, kết nối thường và TLS. Chưa hỗ trợ mTLS, PgBouncer, replication/failover cluster, COPY, pipeline, sửa kết quả hoặc match SQL tùy ý.
- Transaction tường minh là phạm vi nghiệm thu; không kiểm thử tắt transaction/auto-commit trong bản đầu. Không hứa hỗ trợ mọi ORM/driver.
- Đã pin PostgreSQL 17.11 và pgx v5.7.6 trong [contract](contract.md). Chưa có thông tin phiên bản hệ thống thật nên không cam kết tương thích ngoài matrix được kiểm chứng.

## Các plan

| Plan | Tính năng | Phụ thuộc | Trạng thái |
| --- | --- | --- | --- |
| [P23](01-semantic-contract.md) | Contract PostgreSQL, capability và điểm áp dụng fault | P15; rà soát nền TLS/control hiện có | Done |
| [P24](02-semantic-implementation.md) | Adapter, fixture và nghiệm thu PostgreSQL | P23 | In progress; chờ manual UI |
| [P25](03-mysql-adapter.md) | Contract, adapter và nghiệm thu MySQL | Nền config/control/UI hiện có | Done |

## Điều kiện hoàn tất phase

- Đạt P23-AC1–AC4 và P24-AC1–AC8, có bằng chứng thực chạy.
- Có demo lỗi, phục hồi và commit đã thành công nhưng client nhận lỗi; kiểm tra dữ liệu bằng kết nối độc lập.
- Giữ tương thích HTTP/HTTP2/gRPC và thao tác quản trị đã nghiệm thu; ghi rõ giới hạn protocol, driver và TLS.
- Không phụ thuộc runner/assertions Phase 8. Test Go và fixture tự cung cấp bằng chứng.

## Tiến độ

Adapter, CLI/API/UI và fixture đã hiện thực; automated tests đã chạy. Xem [acceptance](acceptance.md) và [hướng dẫn test](../../examples/postgresql/README.md). Chỉ có phase `after_commit` trong bản đầu; không có matcher HTTP/SQL cho PostgreSQL. P23 Done, P24 còn chờ manual UI để đóng toàn phase.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Mở rộng MySQL đã chốt

Người dùng chọn MySQL 8.4 LTS + fixture Go. [P25 — Adapter MySQL](03-mysql-adapter.md)
**Done**: explicit transaction, phase `after_commit`, delay/hold_response/close_connection,
kết nối trực tiếp và kiểm chứng dữ liệu độc lập. Xem [MySQL contract](mysql-contract.md),
[acceptance](mysql-acceptance.md) và [hướng dẫn chạy](../../examples/mysql/README.md);
không mở rộng MariaDB/replication/cluster hoặc fixture ORM. P25 có nghiệm thu riêng,
không dùng bằng chứng PostgreSQL để claim MySQL đã hỗ trợ. Phase 8 vẫn Deferred.
Hoàn tất phase mở rộng cần cả P24 và P25 đạt tiêu chí tương ứng.
