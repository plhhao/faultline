# P01 — Schema và validation YAML

- Trạng thái: **Planned**
- Phụ thuộc: Không; bắt đầu từ scaffold hiện tại.
- Nguồn: [specific.md](../../specific.md), mục 7, 9, 11.
- Nghiệm thu MVP liên quan: AC11, AC23
- Vùng thay đổi dự kiến: `internal/config/, examples/http/`

## DEFINE

Định nghĩa config MVP và báo lỗi có field path; chưa mở listener hoặc apply runtime.

## PLAN → BUILD

1. Chọn thư viện YAML tối thiểu; parse strict, từ chối unknown fields và ID trùng trong scope.
2. Validate origin upstream, listener, đúng một selector, probability [0,1], nth/every nguyên dương, duration dương, respond 200–599 và tổ hợp action/phase theo capability HTTP.
3. Chuẩn hóa config để so sánh nội dung hiệu lực; chốt defaults có tài liệu, không coi các số YAML minh họa là yêu cầu.
4. Resolve đường dẫn theo thư mục config; kiểm tra cert/key khớp, CA parse được, không dùng upstream_tls với upstream HTTP. Thêm config hợp lệ và fixture lỗi.

## VERIFY

- Table-driven tests cho config hợp lệ/sai và thông báo field path; bao gồm unknown fields, selector thiếu/trùng, TLS sai và status ngoài phạm vi.
- Kiểm tra thứ tự rules được giữ nguyên, đường dẫn không phụ thuộc working directory; config mẫu parse được.

## REVIEW

Không đưa truncate, mTLS, HTTP/2 hoặc runtime plugin vào schema MVP; secrets không xuất hiện trong lỗi.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
