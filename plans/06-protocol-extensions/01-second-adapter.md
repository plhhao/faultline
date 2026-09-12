# P16 — Chọn và hiện thực adapter thứ hai

- Trạng thái: **Deferred**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 9, 9.1, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `internal/proxy/<protocol>/, internal/config/, tests/integration/`

## DEFINE

Chọn TCP hoặc gRPC từ một use case thực; chứng minh thêm protocol không viết lại rule/control engine.

## PLAN → BUILD

1. Ở DEFINE chọn protocol, đơn vị flow/selector và capability matrix; nếu gRPC cần HTTP/2 thì thực hiện phần nền tương ứng của P17 trước adapter.
2. Hiện thực adapter trong source Go, metadata và action được hỗ trợ; config từ chối capability không hợp lệ.
3. Tái sử dụng control/recorder/selector và chỉ điều chỉnh contract nơi có bằng chứng cần thiết.

## VERIFY

- Integration test use case đã chọn; regress HTTP MVP và reload, counters, cancellation.
- Review dependency imports và lượng thay đổi engine; không đồng nhất TCP connection với HTTP attempt.

## REVIEW

Chưa chọn đồng thời mọi protocol hoặc thiết kế plugin runtime.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
