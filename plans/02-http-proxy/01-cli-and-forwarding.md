# P04 — CLI validate/serve và HTTP forwarding

- Trạng thái: **Done**
- Phụ thuộc: [P01a](../01-core/04-multi-file-config.md), [P02](../01-core/02-runtime-snapshots.md), [P03](../01-core/03-rule-engine.md)
- Nguồn: [specific.md](../../specific.md), mục 3, 7, 8.1, 11.
- Nghiệm thu MVP liên quan: AC1, AC2, AC17
- Vùng thay đổi dự kiến: `cmd/faultline/, internal/proxy/http/, tests/integration/`

## DEFINE

Chạy HTTP/1.1 reverse proxy nhiều listener/upstream cố định; khởi động pass-through theo mặc định.

## PLAN → BUILD

Quyết định hiện thực: dùng `httputil.ReverseProxy`, transport tắt upstream keep-alive để không có retry trên connection reuse; client keep-alive vẫn hoạt động. Deadline và inflight áp dụng toàn process. CLI chỉ có validate/serve; action chưa có executor trả 501 khi tới phase, không ghi applied. Shutdown hủy flow đang chạy và đóng connection.

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

## Kết quả VERIFY / REVIEW — 2026-09-13

- CLI validate/serve dùng loader include; test single/multi-file, input sai, startup disabled/start-enabled và cancellation đã pass. Binary build, `--help` và validate multi-file đã chạy thành công.
- Integration test pass: method/escaped path/raw query/body, repeated headers, hop-by-hop headers, request/response trailers, streaming upload 8 MiB và response trước EOF, hai listener độc lập, rollback khi bind listener sau thất bại.
- Kiểm tra hai request trên cùng client connection: upstream dùng hai connection mới, upstream đóng ở attempt thứ hai trả 502, không có attempt thứ ba. Disabled không tăng eligible.
- `go test -race -coverpkg=./... ./... -timeout 60s`, `go vet ./...` và build CLI pass (qua RTK, cache tạm). HTTP tests cần quyền bind localhost ngoài sandbox.
- Review giữ nguyên raw query và trailer cập nhật sau EOF; đóng upload chưa hoàn tất khi upstream trả sớm. No-retry đổi lấy chi phí TCP/TLS mỗi request; chưa có benchmark/SLA. MC5 phần CLI hoàn tất, reload/Docker vẫn ở P11/P15.
