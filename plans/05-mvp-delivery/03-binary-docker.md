# P15 — Binary, Docker và bàn giao MVP

- Trạng thái: **Done (2026-09-14)**
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

## Kết quả VERIFY/REVIEW

- Build native macOS arm64, cross-build Linux arm64 và build source-only export pass.
  Dockerfile Go 1.26.4 build source, runtime scratch/non-root 65532 có CA hệ thống.
- `TestContainerRuntime` pass: validate/serve/status/enable/reload/disable, HTTPS
  traffic, invalid cert startup, config một/nhiều file tương đương, missing fragment
  và duplicate ID báo source/giữ snapshot, stop exit 0; payment demo hai biến thể pass.
- Native binary/admin/demo driver pass; full race suite, vet, binary help và năm
  example roots pass. Hướng dẫn tại [Docker delivery](../../deploy/docker/README.md).
- Review: mount path/quyền non-root rõ ràng, không mở admin TCP, không commit key thật.
  Version tag base image có thể đổi contents; không cam kết byte-identical rebuild.
  Chưa publish image/release; kiến trúc khác cần runtime verification riêng.
- [AC1–AC24](acceptance.md) đủ bằng chứng; MVP Done trong phạm vi đã chốt.
