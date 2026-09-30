# P27 — Hiện thực TCP/TLS passthrough adapter

- Trạng thái: **Done**; adapter, selector/snapshot/reload, lifecycle, bảy ca local và regression đã pass.
- Phụ thuộc: [P26](01-tcp-contract.md) đã chốt contract.
- Nguồn: [phạm vi Phase 10](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi dự kiến: `internal/proxy/tcp/`, config/engine/control/recorder, CLI/API/UI capabilities và tests.

## DEFINE

Hiện thực chuyển tiếp byte TLS opaque qua một upstream cố định, không sửa [HTTP handler](../../internal/proxy/http/handler.go) để chuyển ciphertext. Hỗ trợ bốn primitive đã chốt ở P26 theo connection, giới hạn tài nguyên và giữ hành vi HTTP/HTTP2/gRPC/PostgreSQL/MySQL hiện có.

## PLAN → BUILD

1. Thêm config parse/validate/encode và capability cho `protocol: tcp`; từ chối HTTP matcher, phase/direction/action/trigger sai, upstream và listener không hợp lệ. Upstream/listener đổi cần restart; rule đổi được reload theo contract.
2. Thêm adapter TCP với bind/dial timeout, giới hạn inflight, copy hai chiều, EOF/half-close/close, cancellation và shutdown. Không retry upstream tự động hoặc giữ buffer không giới hạn. Mỗi connection lấy snapshot lúc accept, quyết định fault một lần.
3. Gắn `close_connection`, `hold` từng chiều, `throttle` từng chiều và `delay_connect` vào điểm trigger P26. Phân biệt close thường và TCP reset nếu thực sự hỗ trợ; không gọi mọi EOF là reset. Hold phải có `max_duration`, kết thúc hai leg và giải phóng slot; throttle giữ rate theo hướng cấu hình và không gây burst vượt bound. Dial delay không giữ socket/slot sau timeout hoặc client cancel.
4. Tích hợp engine selector, enable/disable, no-op/reload, CLI status và recorder. Event ghi connection flow, rule/revision, selected/reached/applied/not_reached, trigger và byte hai chiều; không ghi payload hoặc credential.
5. Bổ sung managed API validation/capabilities và UI editor/diff/apply. UI chỉ hiện field hợp lệ cho TCP, không hiện method/path/header HTTP; kiểm thử chuyển qua các protocol khác giữ draft và rule cũ.
6. Unit/integration dùng fixture TLS kín của P26: pass-through, chứng chỉ sai, xác suất/nth/every, đủ bảy ca fault theo hai chiều, connection reuse, cancellation, đồng thời, upstream reset, idle/shutdown và tài nguyên. Đo lỗi/timing thực tế ở client; không gắn nhãn S3 success.

## VERIFY

Test tự động dự kiến trong `internal/proxy/tcp/*_test.go` và `tests/integration/tcp_test.go`: baseline TLS hai chiều/cert sai; close sớm và sau N byte; hold từng chiều; throttle từng chiều; dial delay; selector, reload và connection reuse; timeout, hủy và shutdown. Mỗi case fault phải kiểm tra cả lỗi/thời gian client **và** event `reached/applied`; nếu trigger không tới thì kiểm tra `not_reached`, không ghi nhầm PASS. Chạy targeted TCP tests rồi full Go tests/race/vet và binary build. Tên test và lệnh chính xác được ghi vào `acceptance.md` khi code tồn tại.

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P27-AC1 | Config TCP hợp lệ parse/encode/reload; tổ hợp action/trigger/direction, matcher, upstream sai bị từ chối; config cũ giữ nguyên hành vi. |
| P27-AC2 | TLS fixture qua proxy giữ hostname/SNI và certificate verification end-to-end; không giải mã payload, không có đường client → upstream bỏ qua proxy. |
| P27-AC3 | Bốn primitive bao phủ đủ bảy ca lỗi ở fixture TLS, tạo lỗi/timing đúng hướng; ngưỡng byte đo ciphertext, trigger không tới được ghi not_reached và hold kết thúc có bound. |
| P27-AC4 | Probability 0/1, nth/every, first-match, disabled rule, enable/disable, reload/no-op và snapshot trên connection dài hạn đúng P26. |
| P27-AC5 | Đồng thời, EOF, timeout, client cancel, upstream reset và shutdown không rò goroutine/socket/buffer; race test và giới hạn inflight pass. |
| P27-AC6 | CLI/API/UI/counters/events đúng capability TCP, không lộ secret; Go tests/race/vet, binary build và hồi quy protocol cũ pass hoặc ghi rõ blocker. |

**Đối chiếu hoàn tất:** AC1–AC6 pass. Selector probability/nth/every/first-match và no-op/reload được kiểm tra ở engine/control dùng chung. Test TCP riêng chứng minh một decision trên connection tái sử dụng, snapshot cũ giữ fault qua reload, disable áp dụng cho connection mới, giới hạn inflight, timeout thu hồi slot, upstream reset, đồng thời và shutdown. Targeted TCP chạy 3 lần và toàn package chạy với race detector.

## REVIEW

Đã đối chiếu copy hai chiều, timeout, cleanup và trigger/forward với contract P26. Counter byte chỉ tăng sau write thành công; test chờ publication của counter thay vì giả định thứ tự scheduler. Không hứa thu hồi byte đã đến SDK.
