# P12 — JSON events và counters có giới hạn

- Trạng thái: **Planned**
- Phụ thuộc: [P10](../04-runtime-control/01-local-admin.md), [P11](../04-runtime-control/02-atomic-reload.md)
- Nguồn: [specific.md](../../specific.md), mục 6.3, 10, 11.
- Nghiệm thu MVP liên quan: AC13, AC15, AC16, AC18
- Vùng thay đổi dự kiến: `internal/recorder/, internal/control/, internal/proxy/http/`

## DEFINE

Ghi bằng chứng từ decision đến outcome, phân biệt lỗi inject với lỗi proxy/upstream.

## PLAN → BUILD

1. Xuất JSON stdout gồm run/flow/proxy/protocol/revision/state/control sequence, thời gian, rule/eligible sequence/selector, phase/action/outcome.
2. Counters riêng total/eligible/selected/applied/active/dropped; counter chính xác không phụ thuộc event còn trong queue.
3. Queue bounded, đầy thì bỏ event/tăng dropped thay vì block vô hạn; status/control events và shutdown có cách kết thúc bounded.
4. Giới hạn retention revision và cardinality, không dùng flow ID/header làm metric label; mặc định không log body/Authorization/toàn bộ headers.

## VERIFY

- Dựng selected→not_reached và selected→applied; TLS/upstream/cancel/timeout/overload không bị gán thành injected fault.
- Làm đầy queue bằng sink chậm, xác nhận dropped tăng mà proxy vẫn tiến triển; kiểm tra field bắt buộc và dữ liệu nhạy cảm.
- Flow cũ sau reload ghi đúng revision, status còn active faults sau disable; chạy -race.

## REVIEW

Không suy luận retry, business commit, circuit breaker hoặc PASS/FAIL chỉ từ traffic.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
