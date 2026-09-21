# Hướng dẫn sử dụng Faultline

Faultline là proxy đặt giữa ứng dụng và dependency. Chuyển endpoint ứng dụng gọi
sang listener Faultline, sau đó dùng rule để gây lỗi có kiểm soát và quan sát
cách ứng dụng phản ứng.

| Mục đích | Tài liệu |
| --- | --- |
| Chạy HTTP đầu tiên | [Bắt đầu nhanh](getting-started.md) |
| Triển khai trên server | [Deployment](deployment.md) |
| Tra cứu mọi lệnh và flag | [Tham chiếu CLI](cli-reference.md) |
| Viết rule và match traffic | [Cấu hình](configuration.md) |
| Bật/tắt, reload, đọc counters | [Vận hành CLI](operations.md) |
| Dùng giao diện quản trị HTTPS | [Tester UI](tester-ui.md) |
| Test mất ACK COMMIT | [PostgreSQL và MySQL](databases.md) |

Các file dưới `examples/` là demo có thể chạy. Kế hoạch và bằng chứng kiểm thử
chi tiết nằm trong `plans/`.
