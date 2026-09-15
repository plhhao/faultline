# P18 — Truncate và throttle cho request/response

- Trạng thái: **Done**
- Phụ thuộc: [P16](01-second-adapter.md), [P17](02-http2-mtls.md)
- Nguồn: [specific.md](../../specific.md), mục 5.2, 5.4, 6.4; [phạm vi phase 06](README.md).
- Nghiệm thu MVP liên quan: Regression fault/control/resource behavior; thêm tiêu chí riêng bên dưới.
- Vùng thay đổi dự kiến: `internal/fault/`, `internal/config/`, `internal/proxy/http/`, `internal/proxy/grpc/`, recorder khi cần, `tests/integration/`, ví dụ.

## DEFINE

Hiện thực **cả truncate và throttle**, theo thứ tự truncate → throttle. Mỗi action hỗ trợ `request` (Faultline → upstream) hoặc `response` (Faultline → app), trên HTTP/1.1, HTTP/2 và gRPC unary, với TLS/mTLS khi cấu hình. Một flow chỉ nhận một action ở một chiều; không gộp throttle rồi truncate hoặc đồng thời request/response trong đợt này.

| Action | Hành vi và use case |
| --- | --- |
| Truncate | Chuyển tối đa N byte body theo chiều chọn rồi kết thúc bất thường khi body còn dữ liệu; dùng kiểm thử upload/download hoặc unary payload dở dang |
| Throttle | Giới hạn tốc độ body theo byte/giây cho từng flow và chiều chọn; dùng kiểm thử truyền chậm, timeout và cancellation |

Đơn vị đo là byte body tại adapter, không gồm HTTP headers, HTTP/2 frame overhead hoặc TLS overhead. Với gRPC, tính byte body chứa cả message framing và payload ở dạng đang truyền (kể cả compression); có thể cắt giữa message, không phải cắt theo số protobuf message. Đây là giới hạn byte ở proxy, không phải mô phỏng packet loss hoặc tốc độ NIC chính xác.

Truncate HTTP/1.1 phải giữ bằng chứng incomplete body với Content-Length/chunked, không sửa độ dài hoặc thêm kết thúc bình thường. HTTP/2/gRPC kết thúc stream được chọn; không đóng cả connection hoặc báo RPC thành công cho dữ liệu thiếu. Nêu giới hạn nhận biết thiếu dữ liệu của response close-delimited HTTP/1.1.

Throttle tính ngân sách riêng mỗi flow, không phải tổng bandwidth toàn proxy. Có backpressure, buffer hữu hạn và cancellation; không đọc hết body vào RAM để làm chậm đầu ra. Lỗi nguồn hoặc cancellation trước điểm cắt phải được phân biệt với truncate đã áp dụng.

## PLAN → BUILD

1. Ghi schema, phase/capability matrix và thời điểm selected/applied/not_reached cho từng action/adapter trước BUILD. Chốt giới hạn N (gồm N=0), body rỗng, N bằng/vượt chiều dài, nguồn bị ngắt sớm và response không có body; body đã hoàn tất không được báo là truncate thành công.
2. Hiện thực truncate với bộ đếm byte và kết thúc transport/stream đúng framing; chứng minh không chuyển quá N byte. Việc phát hiện body còn dữ liệu dùng buffer giới hạn và tuân theo deadline/cancellation.
3. Thêm tests/ví dụ truncate hai chiều trên HTTP và unary gRPC, rồi hiện thực throttle. Chốt rate dương, kích thước chunk/burst tối đa, công thức thời gian và dung sai đo trước khi chạy test; không dùng dung sai điều chỉnh sau để che sai tốc độ.
4. Tái sử dụng fault/control/recorder, protocol termination do adapter cung cấp; validation từ chối direction/phase/parameter không hợp lệ. Rule fault mới reload được, flow đang chạy giữ quyết định cũ.
5. Thêm ví dụ truyền payload lớn với TLS/mTLS và ghi giới hạn quan sát. Weighted selection, chaining, reset/half-close và fault toàn connection tiếp tục Deferred.

## VERIFY — Tiêu chí nghiệm thu

| ID | Kiểm chứng bắt buộc |
| --- | --- |
| FT-1 | Truncate request/response HTTP/1.1 với Content-Length/chunked: phía nhận phát hiện incomplete body; byte chuyển đúng điểm cắt; kiểm tra N=0, bằng/vượt chiều dài, body rỗng/bodyless, lỗi nguồn; ghi giới hạn close-delimited |
| FT-2 | Truncate HTTP/2/gRPC request/response, gồm cắt giữa message: flow không thành công giả, stream khác trên cùng connection hoàn tất; kiểm tra plain/TLS/mTLS và không retry ngầm |
| FT-3 | Throttle hai chiều trên HTTP/1.1, HTTP/2 và unary gRPC đạt rate/công thức/dung sai định trước, giới hạn burst và ngân sách riêng mỗi flow; xác nhận pass-through không bị throttle khi disabled hoặc probability 0 |
| FT-4 | Cancel/deadline/shutdown trong cả hai fault giải phóng tài nguyên; payload lớn/nhiều flow có bộ nhớ giới hạn; reload snapshot/counters đúng; natural error và fault applied không bị gộp; mTLS bật không đổi semantics action |

Chạy tests/race/vet/build và regression năm fault MVP; tổng hợp matrix protocol × direction × action × TLS mode với check đã chạy/chưa chạy. Binary/Docker smoke kiểm tra cấu hình mới, file mount, admin reload và ít nhất một trường hợp mỗi action/chiều; ghi điều kiện đo throttle và bằng chứng cleanup.

## REVIEW

Review cut boundary, framing và gRPC trailers/status; không báo thiếu body thành kết quả thành công. Kiểm tra timer/buffer, backpressure, stream isolation, retry, counters và cleanup; không cam kết packet loss/reordering ở cấp IP hoặc tác động chính xác lên timing của stream khác dùng chung tài nguyên.


## BUILD / VERIFY — 2026-09-14

Hai action có wrapper body tại `internal/fault/body.go`; schema `direction`, `bytes`, `bytes_per_second` và phase đã ghi trong [contract phase](README.md#build--contract-triển-khai). Truncate dò một byte, throttle chunk hữu hạn/ngân sách riêng mỗi flow, callback applied an toàn khi upload chạy trên goroutine transport. FT-1–FT-4 đã có test pass, xem [lệnh, matrix, tài nguyên và review](acceptance.md).

2026-09-14: **Done** sau full tests/race, vet, build CLI, binary/Docker và review; kết quả/lệnh cụ thể ở [bằng chứng nghiệm thu](acceptance.md).
