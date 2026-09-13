# P02 — Snapshot và trạng thái injection

- Trạng thái: **Done**
- Phụ thuộc: [P01](../01-core/01-config-schema.md)
- Nguồn: [specific.md](../../specific.md), mục 6.3, 8, 8.1.
- Nghiệm thu MVP liên quan: AC10, AC12, AC17, AC18, AC19, AC20
- Vùng thay đổi dự kiến: `internal/control/`

## DEFINE

Tạo control service trong process, snapshot config và injection state nhất quán; chưa có admin transport.

## PLAN → BUILD

1. Tạo run ID, revision, control sequence và snapshot bất biến mà mỗi flow giữ đến khi kết thúc.
2. Serialize apply/enable/disable; mặc định disabled, --start-enabled sẽ được wiring ở P04. Enable/disable lặp là no-op và không reset counters.
3. Apply config hiệu lực giống nhau là no-op; thay đổi tạo revision/counter scope mới, giữ injection state. Từ chối toàn bộ thay đổi cần restart.
4. Xác định ownership/lifetime của revision cũ và counters; không giữ lịch sử revision vô hạn.

## VERIFY

- Kiểm tra snapshot cũ không đổi sau apply; lỗi validation hoặc field cần restart không đổi active revision.
- Kiểm tra enable/disable, reload khi disabled, no-op và restart; chạy race tests với acquire/apply/toggle đồng thời.

## REVIEW

Không tạo snapshot gồm config cũ và state mới do đọc rời rạc; flow cũ phải ghi đúng revision.

## Kết quả — 2026-09-12

- **Done trong phạm vi core của plan.** Startup disabled, nth không bị tiêu thụ, toggle/no-op, reload/restart rejection, reset counters, revision cũ tiếp tục độc lập, seed replay và acquire/apply/toggle đồng thời.
- VERIFY: `go test ./...`, `go test -race -cover ./...`, `go vet ./...` và `go build ./...` đã pass trên Go 1.26.4, darwin/arm64. Commands dùng prefix `rtk proxy` và `GOCACHE=/private/tmp/faultline-go-build` do sandbox chặn cache mặc định. Coverage package: **97,6%**.
- REVIEW: Control sở hữu bản sao Document; đã sửa và thêm regression test cho caller thay Document sau New/Apply. Không lưu map lịch sử revision; snapshot cũ giữ engine/counters đến khi không còn reference.
- Các AC liên quan mới được kiểm chứng ở tầng core; HTTP I/O, TLS handshake, CLI và fault execution tiếp tục trong các phase sau. Không có blocker còn lại cho phạm vi plan này.
