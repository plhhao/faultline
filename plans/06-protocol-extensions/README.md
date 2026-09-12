# Phase 06 — Mở rộng protocol và fault

Phạm vi: **Sau MVP**. Trạng thái: **Deferred.

Chứng minh khả năng mở rộng qua adapter thứ hai; thêm HTTP capabilities theo nhu cầu.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P16](01-second-adapter.md) | Chọn và hiện thực adapter thứ hai | P15 |
| [P17](02-http2-mtls.md) | HTTP/2 và mTLS theo nhu cầu | P15 |
| [P18](03-additional-faults.md) | Truncate, throttle và fault bổ sung | P15 |

## Điều kiện hoàn tất phase

Chỉ kích hoạt khi use case được ưu tiên. Chốt phạm vi từng plan trước BUILD; các capability tùy chọn không mặc nhiên bắt buộc làm hết. Điều chỉnh trạng thái và tiêu chí phase theo phạm vi đã chọn.

Xem [lộ trình và quy tắc thực hiện](../README.md).
