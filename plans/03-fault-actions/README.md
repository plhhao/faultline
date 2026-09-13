# Phase 03 — Fault actions

Phạm vi: **MVP**. Trạng thái: **Done** (2026-09-13).

Tất cả action MVP hoạt động tại đúng phase, có cancellation và bằng chứng kiểm thử.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P07](01-delay-and-respond.md) | Delay và response giả | P06 |
| [P08](02-close-connection.md) | Đóng connection có chủ đích | P06 |
| [P09](03-hold-request-response.md) | Giữ request và mất response | P07, P08 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Kết quả

P07–P09 hoàn tất: `fault.Builtin` thực thi cả năm action qua capabilities; CLI
`serve --start-enabled` inject thật, mặc định vẫn disabled. HTTP adapter xử lý
bodyless/framing, cancel upstream, đóng client và dọn upload đang giữ.

VERIFY: `go test -race ./... -timeout 90s`, `go vet ./...`, build CLI và binary
help/multi-file validate pass. Regression thêm cho deadline khi delay giữ upload
chưa đọc cũng pass với race detector. Network tests được chạy với quyền bind
localhost sau khi sandbox chặn lần đầu.

REVIEW: graph không trả function/flow để đối chiếu nên đã review source/diff thủ
công. Sửa lỗi response giả thiếu chunk kết thúc bằng Content-Length; thay Hijack
bằng đóng connection trực tiếp để tránh tranh chấp với đọc bỏ upload. Kiểm tra
phân biệt đóng chủ động với cancellation, và chờ goroutine đọc bỏ kết thúc.

Phạm vi kiểm thử: bốn tổ hợp HTTP/HTTPS cho mọi action/phase, delay 500 ms với
dung sai -25 ms/+2 s, response 503 và HEAD/204/205/304 trên keep-alive, close
probability 0/1 và listener độc lập, hold hết hạn/client timeout/shutdown, side
effect trước response headers và 24 flow giữ đồng thời được dọn khi shutdown.

Giới hạn: delay trước upstream giữ backpressure; khi upload chưa được đọc, phát
hiện client disconnect có thể chờ I/O tiếp tục, còn deadline/shutdown vẫn hủy
timer. Không cam kết TCP RST hoặc rollback nghiệp vụ upstream. Recorder/admin ở
phase 4; payment demo đầy đủ, benchmark tài nguyên và nghiệm thu MVP ở phase 5.
