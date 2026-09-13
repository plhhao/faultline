# P01 — Schema và validation YAML

- Trạng thái: **Done**
- Phạm vi hoàn tất: schema một file. Bổ sung include đã hoàn tất tại [P01a](04-multi-file-config.md).
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

## Kết quả — 2026-09-12

- **Done trong phạm vi core của plan.** Table-driven validation cho config hợp lệ/sai, lỗi có field path, selector/duration/action, TLS key mismatch/CA/file paths và config mẫu. Kiểm tra normalization, thứ tự rule, proxy reorder, deep copies và thay đổi nội dung TLS.
- VERIFY: `go test ./...`, `go test -race -cover ./...`, `go vet ./...` và `go build ./...` đã pass trên Go 1.26.4, darwin/arm64. Commands dùng prefix `rtk proxy` và `GOCACHE=/private/tmp/faultline-go-build` do sandbox chặn cache mặc định. Coverage package: **93,1%**.
- REVIEW: Dùng YAML v3.0.5 với decoder theo field path; defaults và quy tắc input được ghi trong examples/http/README.md. Không có protocol/action ngoài MVP hoặc secret scalar trong lỗi.
- Các AC liên quan mới được kiểm chứng ở tầng core; HTTP I/O, TLS handshake, CLI và fault execution tiếp tục trong các phase sau. Không có blocker còn lại cho phạm vi plan này.
