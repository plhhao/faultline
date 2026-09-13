# P04 — CLI validate/serve và HTTP forwarding

- Trạng thái: **Planned**
- Phụ thuộc: [P01a](../01-core/04-multi-file-config.md), [P02](../01-core/02-runtime-snapshots.md), [P03](../01-core/03-rule-engine.md)
- Nguồn: [specific.md](../../specific.md), mục 3, 7, 8.1, 11.
- Nghiệm thu MVP liên quan: AC1, AC2, AC17
- Vùng thay đổi dự kiến: `cmd/faultline/, internal/proxy/http/, tests/integration/`

## DEFINE

Chạy HTTP/1.1 reverse proxy nhiều listener/upstream cố định; khởi động pass-through theo mặc định.

## PLAN → BUILD

1. Wiring config/control/engine trong CLI validate và serve, gồm --config trỏ file gốc và --start-enabled. Cả hai lệnh dùng loader toàn tập include của P01a; một file lỗi không mở listener. Kiểm chứng MC5 phía CLI; không hiển thị lệnh chưa có implementation.
2. Chuẩn bị config và listener trước khi báo ready; lỗi startup dọn các tài nguyên đã mở.
3. Stream request/response với bộ nhớ giới hạn, giữ method/path/query/body và HTTP proxy semantics; xử lý hop-by-hop headers.
4. Kiểm soát đường gửi upstream để không tự retry, kể cả cơ chế retry của transport được chọn. Áp dụng deadline/inflight cơ bản ngay, không chờ hardening.
5. Định nghĩa lỗi rõ cho CONNECT/upgrade và các chế độ ngoài MVP; thêm fixture client/upstream bằng Go testing.

## VERIFY

- So sánh baseline với proxy, hai listener độc lập, body lớn không buffer toàn bộ; startup sai không phục vụ traffic.
- Đếm attempt thực đến upstream ở tình huống connection lỗi/reuse; xác nhận proxy không phát lại request. Serve mặc định không tiêu thụ eligible.

## REVIEW

Chưa gọi upstream ready hay app ready chỉ vì listener đã bind; không thêm rewrite/load balancing.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
