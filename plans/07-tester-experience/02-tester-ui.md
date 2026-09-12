# P20 — UI chỉnh config và quan sát

- Trạng thái: **Deferred**
- Phụ thuộc: [P19](../07-tester-experience/01-admin-api.md)
- Nguồn: [specific.md](../../specific.md), mục 2, 8.1, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `đường dẫn frontend xác định khi DEFINE`

## DEFINE

Tester chọn proxy/rule, thay tỷ lệ và kiểm tra cấu hình thực sự đang chạy mà không sửa file trên server.

## PLAN → BUILD

1. Chốt UI stack theo nhu cầu triển khai; xây form từ schema/capability, hiển thị draft/active revision và diff trước apply.
2. Hiển thị lỗi field, conflict và apply result; enable/disable/status thể hiện active faults còn lại.
3. Trình bày selected/applied/outcome, dropped events và giới hạn kết luận bằng ngôn ngữ người dùng hiểu.

## VERIFY

- E2E tester sửa probability→validate→apply→thấy revision mới; config lỗi/conflict không mất bản nháp.
- Kiểm tra quyền thao tác và disable khi còn hold; UI không báo proxy ready thành app ready.

## REVIEW

Không sao chép validation semantics thành engine độc lập ở frontend.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
