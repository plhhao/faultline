# Phase 02 — HTTP/HTTPS proxy

Phạm vi: **MVP**. Trạng thái: **Planned.

CLI validate/serve và proxy nhiều listener chuyển tiếp HTTP/1.1, TLS, lifecycle hooks.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P04](01-cli-and-forwarding.md) | CLI validate/serve và HTTP forwarding | P01, P02, P03 |
| [P05](02-https.md) | TLS hai phía và HTTP/1.1 | P04 |
| [P06](03-flow-lifecycle.md) | Flow lifecycle và adapter capabilities | P04, P05 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).
