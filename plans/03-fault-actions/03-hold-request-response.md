# P09 — Giữ request và mất response

- Trạng thái: **Planned**
- Phụ thuộc: [P07](../03-fault-actions/01-delay-and-respond.md), [P08](../03-fault-actions/02-close-connection.md)
- Nguồn: [specific.md](../../specific.md), mục 5.1, 11, 12.
- Nghiệm thu MVP liên quan: AC9, AC14, AC21, AC22
- Vùng thay đổi dự kiến: `internal/fault/, internal/proxy/http/, tests/integration/`

## DEFINE

Giữ client đến cancel/max_duration để mô phỏng mất liên lạc và ambiguous outcome.

## PLAN → BUILD

1. hold_request không gọi upstream, không trả response cuối; hết max_duration đóng client.
2. hold_response chỉ bắt đầu sau headers cuối cùng; đóng/cancel upstream ngay rồi giữ client, không buffer body trong lúc chờ.
3. Dùng cleanup/deadline chung nơi phù hợp; deadline toàn flow và shutdown có thể kết thúc hold trước max_duration.

## VERIFY

- Client timeout ngắn hơn max_duration: hold_request không chạm upstream; hold_response có side effect đã ghi trước headers.
- Client timeout dài hơn max_duration thấy connection đóng; cancel và flow deadline giải phóng tài nguyên.
- Lặp trên HTTPS và kiểm tra selected/applied/not_reached theo phase. Demo payment đầy đủ ở P14.

## REVIEW

Không khẳng định upstream dừng mọi business work khi context bị cancel; không tạo hold vô hạn.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
