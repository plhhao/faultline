# P22 — Correlation, assertions và báo cáo

- Trạng thái: **Deferred**
- Phụ thuộc: [P21](../08-failure-testing/01-scenario-runner.md)
- Nguồn: [specific.md](../../specific.md), mục 10, 12, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `đường dẫn correlation/assertion/report xác định khi DEFINE`

## DEFINE

Đánh giá hành vi phục hồi với nguồn bằng chứng rõ, hỗ trợ PASS/FAIL/inconclusive.

## PLAN → BUILD

1. Chốt operation identity, correlation window và nguồn business evidence; không suy retry từ payload giống nhau.
2. Thiết kế assertions tối thiểu cho demo, kết quả và exit code CI; phân biệt idempotency key với operation ID độc lập.
3. Báo dữ liệu thiếu khi recorder drop hoặc không đủ correlation; báo phạm vi quan sát và lưu artifact.

## VERIFY

- Fixtures PASS/FAIL/inconclusive, missing events, key thay đổi và operation không liên kết được.
- Lost response với/không idempotency có kết quả dựa trên dữ liệu độc lập, không chỉ status/timeline.

## REVIEW

Không gán PASS khi thiếu bằng chứng hoặc tự kết luận circuit breaker mở từ việc không thấy traffic.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
