# P06 — Flow lifecycle và adapter capabilities

- Trạng thái: **Planned**
- Phụ thuộc: [P04](../02-http-proxy/01-cli-and-forwarding.md), [P05](../02-http-proxy/02-https.md)
- Nguồn: [specific.md](../../specific.md), mục 5, 9, 10, 11.
- Nghiệm thu MVP liên quan: AC13, AC14
- Vùng thay đổi dự kiến: `internal/proxy/http/, internal/engine/, internal/fault/`

## DEFINE

Chụp snapshot khi nhận headers và cung cấp hai hook để fault executor điều khiển lifecycle an toàn.

## PLAN → BUILD

1. Gắn flow ID, snapshot, sequence và quyết định đúng một lần; request mới trên keep-alive lấy snapshot mới.
2. Đặt before_upstream_request trước khi gọi upstream; after_upstream_headers sau headers cuối cùng và trước khi trả response cuối cùng cho client.
3. Xử lý 1xx như informational, không kích hoạt hook sau headers cuối cùng; không gửi response cuối sớm ngoài kiểm soát action.
4. Adapter sở hữu close/cancel/body I/O; chỉ tạo capability interface nhỏ đủ dùng. Định nghĩa selected/applied/not_reached và outcome cho recorder tương lai.
5. Cleanup body, timer, connection và context trên mọi exit path, kể cả connection bị hijack.

## VERIFY

- Hook order, final headers so với 1xx; selected nhưng upstream lỗi trước hook phải not_reached.
- Cancel/shutdown/early error giải phóng flow; kiểm tra snapshot cố định xuyên suốt một request.

## REVIEW

Nhận headers không đồng nghĩa đọc xong body hay transaction commit; shared fault không import concrete HTTP adapter.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
