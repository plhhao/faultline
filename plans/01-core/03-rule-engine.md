# P03 — Matcher, selector và sequence

- Trạng thái: **Planned**
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

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
