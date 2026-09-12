# P24 — Hiện thực adapter đã chọn

- Trạng thái: **Deferred**
- Phụ thuộc: [P23](../09-semantic-adapters/01-semantic-contract.md)
- Nguồn: [specific.md](../../specific.md), mục 9, 9.1, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `internal/proxy/<technology>/, examples/, tests/integration/`

## DEFINE

Hiện thực đúng capability và protocol version đã chốt ở P23, có demo với dependency thật.

## PLAN → BUILD

1. Bổ sung adapter/config validation trong source, reuse engine/control/recorder theo đơn vị flow đã công bố.
2. Hiện thực routing và fault tại semantic phase được hỗ trợ; quản lý sessions/connections/cancel.
3. Thêm demo ambiguous outcome; tích hợp P22 chỉ nếu cần tự đánh giá business outcome.

## VERIFY

- Integration với database/broker thật: baseline, selected fault, recovery, TLS/auth nếu trong capability và cleanup.
- Regression HTTP MVP; unsupported version/operation có kết quả rõ, không giả vờ hiểu semantics.

## REVIEW

Không tự mở rộng sang các công nghệ còn lại hoặc hứa commit semantics vượt bằng chứng.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
