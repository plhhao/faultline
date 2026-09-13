# Phase 02 — HTTP/HTTPS proxy

Phạm vi: **MVP**. Trạng thái: **Done** (2026-09-13).

CLI validate/serve và proxy nhiều listener chuyển tiếp HTTP/1.1, TLS, lifecycle hooks.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P04](01-cli-and-forwarding.md) | CLI validate/serve và HTTP forwarding | P01a, P02, P03 |
| [P05](02-https.md) | TLS hai phía và HTTP/1.1 | P04 |
| [P06](03-flow-lifecycle.md) | Flow lifecycle và adapter capabilities | P04, P05 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Kết quả

P04–P06 hoàn tất: binary validate/serve, multi-listener HTTP/HTTPS, streaming,
no-retry transport, deadline/inflight, snapshot theo request và lifecycle hooks.
Race tests, vet, build CLI và smoke validate/help pass. Các test mạng cần bind
localhost; lần chạy sandbox bị chặn đã được chạy lại thành công với quyền đó.

Upstream connection mới mỗi request để tránh transport retry; downstream vẫn
keep-alive. Shutdown hủy flow đang chạy. Chưa có fault executor production:
`--start-enabled` bật selection nhưng action được chọn trả 501 tại phase, không
ghi applied. Phase 3 hiện thực action, phase 4 bổ sung admin CLI và recorder.
Reset lượt test riêng vẫn là tính năng tương lai, không đổi semantics counter.
