# Phase 05 — Nghiệm thu và bàn giao MVP

Phạm vi: **MVP**. Trạng thái: **Done (2026-09-14)**.

Đạt AC1–AC24, có demo lost response và chạy được bằng binary/Docker.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P13](01-resource-limits.md) | Giới hạn tài nguyên và shutdown | P12 |
| [P14](02-payment-demo.md) | Demo lost response và nghiệm thu hành vi | P13 |
| [P15](03-binary-docker.md) | Binary, Docker và bàn giao MVP | P14 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Kế hoạch hiện thực

1. P13: bổ sung shutdown drain có hạn, kiểm chứng slow headers/idle và recovery
   của goroutine/heap dưới nhiều delay/hold; benchmark direct/proxy/fault cùng tải.
2. P14: payment fixture và retry driver dùng chung cho hướng dẫn và test;
   đối chiếu dữ liệu độc lập trong hai biến thể có/không idempotency.
3. P15: Dockerfile build từ source, chạy non-root có CA hệ thống; kiểm chứng
   binary/container, config một/nhiều file và demo; lập bảng bằng chứng AC1–AC24.

Giữ `request_timeout` làm giới hạn headers/read/write/idle và flow, mặc định 30s;
inflight 1000, recorder queue 1024. CLI shutdown chờ tối đa 5s rồi cancel,
recorder flush tối đa 2s. Chỉ build/test local, chưa publish image hoặc release.

## VERIFY → REVIEW → Done

- P13: drain/cancel, resource recovery, slow headers/idle, tải có giới hạn và
  [benchmark thực tế](benchmark-results.md) đã pass. Full race suite và vet pass.
- P14: binary và Docker đều cho 2 payment khi không có idempotency, 1 payment
  khi có idempotency sau hai timeout/retry; đối chiếu store và JSON events riêng.
- P15: image non-root build từ source, CA hệ thống, config một/nhiều file/TLS,
  validate/admin/reload/rejection/shutdown đã pass. Build source export sạch pass.
- [Bảng nghiệm thu AC1–AC24](acceptance.md) ghi test, kết quả và giới hạn. Review
  scope/cancellation, test cleanup, source paths, redaction và hướng dẫn bàn giao.

MVP đã hoàn tất trong phạm vi HTTP/1.1; chưa publish image/release, chưa mở phase 06.
