# P21 — Scenario timeline và CI runner

- Trạng thái: **Deferred**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 2, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `đường dẫn scenario/runner xác định khi DEFINE`

## DEFINE

Điều phối một kịch bản lỗi có thời gian và cleanup; dùng control service hiện có.

## PLAN → BUILD

1. Chốt scenario format, scope một process trước, điều kiện bắt đầu/kết thúc và policy với flow đang chạy.
2. Thực thi enable/reload/disable và readiness do test driver cung cấp; lưu run/config/seed/timeline.
3. Timeout/cancel/failure phải cleanup theo policy đã định; chưa mặc định thêm coordinator nhiều proxy.

## VERIFY

- Kịch bản thành công, bước lỗi, hủy giữa hold và timeout đều kết thúc có giới hạn, trạng thái cuối có bằng chứng.
- Chạy tuần tự cùng input để tái lập quyết định; tài liệu hóa giới hạn concurrency.

## REVIEW

Runner thành công chỉ xác nhận điều phối; business PASS/FAIL cần P22.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
