# P06 — Flow lifecycle và adapter capabilities

- Trạng thái: **Done**
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

## Kết quả VERIFY / REVIEW — 2026-09-13

- Mỗi request có flow ID, snapshot và một decision. Test giữ revision cũ qua reload rồi lấy revision mới trên cùng client keep-alive connection pass.
- Hook trước upstream và sau final headers được kiểm thử bằng executor fixture; 103 được chuyển tiếp nhưng không gọi hook cuối. Selected gặp lỗi upstream trước hook được report `not_reached`, không applied; executor chưa có trả 501 khi tới phase.
- Capability nhỏ trong `internal/fault` gồm respond, close connection và cancel upstream; HTTP adapter sở hữu I/O. Observer đồng bộ nhận report, chưa có recorder hoặc event persistence.
- Client cancel, request deadline, inflight overflow/release, shutdown trước final headers và tại cả hai hook, hijack-close, slow upload, upstream trả sớm và body truncated đều pass race tests. Shutdown hủy active flows, chờ handler kết thúc; executor/observer phải tuân thủ cancellation và bounded execution.
- Full suite `go test -race -coverpkg=./... ./... -timeout 60s` pass; chạy lại integration race sau assertion keep-alive cũng pass. Review đã sửa cleanup upload và request trailer sharing; graph chưa có source nodes nên review source thủ công.
- AC13 mới có report trong process; recorder thuộc P12. Fault actions và kiểm chứng tải/tài nguyên đầy đủ cho AC14 còn ở phase 3/5.
