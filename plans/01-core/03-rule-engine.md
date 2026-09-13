# P03 — Matcher, selector và sequence

- Trạng thái: **Done**
- Phụ thuộc: [P01](../01-core/01-config-schema.md), [P02](../01-core/02-runtime-snapshots.md)
- Nguồn: [specific.md](../../specific.md), mục 6, 7, 9.
- Nghiệm thu MVP liên quan: AC3, AC4, AC5, AC6, AC20
- Vùng thay đổi dự kiến: `internal/engine/`

## DEFINE

Quyết định tối đa một action cho một attempt từ metadata và snapshot; engine không sở hữu HTTP I/O.

## PLAN → BUILD

1. Match method, exact path bỏ query, header name không phân biệt hoa thường/value chính xác; AND các điều kiện, matcher rỗng match tất cả.
2. Rule enabled đầu tiên match sở hữu request; selector không chọn thì pass-through, không fallback rule sau.
3. Cấp sequence và chọn fault đồng bộ theo run/revision/proxy/rule; disabled không chạy selector hoặc tăng eligible.
4. Hiện thực probability có biên 0/1 chính xác, nth/every; random xác định theo seed và scope/sequence, không dựa revision ID ngẫu nhiên.

## VERIFY

- Table-driven tests matcher, disabled rule, thứ tự rule, nth/every xen traffic không match, probability 0/1.
- Cùng input tuần tự/seed cho cùng quyết định; statistical test có cỡ mẫu/dung sai định trước. Concurrent test không trùng sequence, chạy -race.

## REVIEW

Không hứa tái hiện cùng logical operation dưới concurrency; không tạo common/utils nếu chưa có hành vi thực sự dùng chung.

## Kết quả — 2026-09-12

- **Done trong phạm vi core của plan.** Matcher AND/case/path/query/header nhiều giá trị, first-match không fallback, disabled, probability 0/1, nth/every, 10.000 mẫu xác suất, replay seed và 1.000 quyết định đồng thời.
- VERIFY: `go test ./...`, `go test -race -cover ./...`, `go vet ./...` và `go build ./...` đã pass trên Go 1.26.4, darwin/arm64. Commands dùng prefix `rtk proxy` và `GOCACHE=/private/tmp/faultline-go-build` do sandbox chặn cache mặc định. Coverage package: **100%**.
- REVIEW: Lock theo rule bảo vệ sequence/random/counters; proxy/rule có scope độc lập. Engine không import HTTP adapter hoặc thực thi action; không coi selected là applied.
- Các AC liên quan mới được kiểm chứng ở tầng core; HTTP I/O, TLS handshake, CLI và fault execution tiếp tục trong các phase sau. Không có blocker còn lại cho phạm vi plan này.
