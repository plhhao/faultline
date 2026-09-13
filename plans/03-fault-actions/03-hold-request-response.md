# P09 — Giữ request và mất response

- Trạng thái: **Done** (2026-09-13)
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

Quyết định: hold dùng cùng timer hủy được; luôn kết thúc bằng đóng connection,
không tạo final response. hold_request đọc bỏ upload trong goroutine có cleanup
để phát hiện client disconnect mà không gửi upstream hay buffer body. hold_response
cancel/close upstream trước khi chờ. Deadline toàn flow vẫn giới hạn cả hai action.

## VERIFY

- Client timeout ngắn hơn max_duration: hold_request không chạm upstream; hold_response có side effect đã ghi trước headers.
- Client timeout dài hơn max_duration thấy connection đóng; cancel và flow deadline giải phóng tài nguyên.
- Lặp trên HTTPS và kiểm tra selected/applied/not_reached theo phase. Demo payment đầy đủ ở P14.

## REVIEW

Không khẳng định upstream dừng mọi business work khi context bị cancel; không tạo hold vô hạn.

## Kết quả VERIFY/REVIEW

- Full race suite, vet, build và CLI smoke pass; lệnh/bằng chứng chung tại [phase README](README.md#kết-quả).
- Cả bốn tổ hợp HTTP/HTTPS: max_duration 150 ms kết thúc bằng đóng connection, không final response. Client timeout 250 ms ngắn hơn hold 10 s nhận lỗi timeout trên HTTP/HTTPS.
- hold_response cancel upstream ngay trong khi client còn chờ; fixture ghi side effect trước 201 và giá trị vẫn tồn tại sau client timeout. hold_request không chạm upstream.
- Client cancel, flow deadline và shutdown trả report canceled, giữ selected/applied riêng; slot max_inflight=1 được tái sử dụng. 24 hold đồng thời kết thúc cùng shutdown.
- Upload chưa hoàn tất được đọc bỏ với bộ nhớ giới hạn; regression client disconnect/max_duration/deadline/shutdown pass, gồm ba lần chạy race cho cleanup upload.
- Review sửa phát hiện client disconnect khi body chưa được đọc, tránh Hijack tranh chấp reader và chờ reader kết thúc. Không suy luận business work đã dừng từ context cancel; demo payment và benchmark đầy đủ vẫn thuộc phase 5.
