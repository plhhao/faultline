# P15 — Binary, Docker và bàn giao MVP

- Trạng thái: **Planned**
- Phụ thuộc: [P14](../05-mvp-delivery/02-payment-demo.md)
- Nguồn: [specific.md](../../specific.md), mục 7, 11, 12.
- Nghiệm thu MVP liên quan: AC24
- Vùng thay đổi dự kiến: `deploy/docker/, cmd/faultline/, README.md, examples/http/`

## DEFINE

Bàn giao cách build/run tái lập cho binary và Docker, hoàn tất gate MVP AC1–AC24.

## PLAN → BUILD

1. Build binary và Dockerfile; chọn OS/architecture thực tế, tài liệu hóa commands, flags và phiên bản toolchain/dependencies.
2. Mount toàn bộ cây config (file gốc và proxies fragments) cùng certs với đường dẫn nhất quán; CLI reload trong container gửi đường dẫn root trong container. Cấu hình bind container rõ ràng, không expose remote admin mặc định.
3. Cập nhật README từ scaffold sang khả năng thật, kèm troubleshooting và giới hạn protocol/fault.
4. Tổng hợp kết quả AC1–AC24 và limitations; chỉ ghi hoàn tất MVP khi mọi kiểm chứng bắt buộc đã có bằng chứng.

## VERIFY

- Smoke test validate/serve/status/enable/reload/disable qua binary và Docker; chạy demo cùng config tương đương.
- Kiểm tra invalid config/cert, mount paths, signal shutdown và build từ checkout sạch; build/test/race/vet đúng môi trường hỗ trợ.
- MC5: chạy cùng cấu hình một file và nhiều file; đổi fragment/reload và kiểm tra source path khi fragment bị thiếu hoặc ID trùng.

## REVIEW

Không công bố image hoặc tạo release từ việc viết plan; khi thực thi phải theo phạm vi phát hành được giao.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
