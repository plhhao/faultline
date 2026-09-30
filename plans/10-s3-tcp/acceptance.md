# Phase 10 acceptance record

Status: **Done**. P26, P27 và P28 đạt toàn bộ acceptance criteria với fixture TLS local, adapter TCP, CLI/UI regression, Docker DNS route và endpoint S3 thật dùng AWS SDK for Go v2.

Fixture versions: Go 1.26.4, AWS SDK for Go v2 `service/s3` v1.113.4 và `config` v1.33.6. Docker runtime: OrbStack/Docker 29.4.0. Provider, region, bucket, endpoint và credentials không được ghi vào log nghiệm thu.

## Kết quả theo plan

| Plan | Kết quả |
| --- | --- |
| P26 | Contract topology/schema/event, TLS/SNI/certificate, trigger byte và bảy fault local pass. SDK thật xác nhận virtual-hosted hostname, TLS/SigV4 và DNS route; không có redirect trên endpoint cố định đã test. |
| P27 | Config, adapter, four primitives, recorder/counters, CLI/API/UI pass. Test TCP riêng xác nhận snapshot qua reload, no-op, disable, một decision trên connection reuse, inflight, timeout, reset, đồng thời và shutdown; selector probability/nth/every/first-match được kiểm tra ở engine dùng chung. |
| P28 | Direct baseline và proxied baseline pass; bảy fault pass qua binary/CLI/Docker với event, byte counters, timing SDK và kiểm tra object độc lập. Cleanup không báo lỗi và project/volume Compose được hạ. |

## Nghiệm thu S3 thật

Lệnh: `PYTHONDONTWRITEBYTECODE=1 python3 examples/s3/run.py` với `FAULTLINE_S3_TEST=1`. Harness dùng key ngẫu nhiên dưới prefix test, một SDK attempt cho mỗi operation, container `sdk` có DNS override và container `verifier` đi trực tiếp.

| Ca | Kết quả SDK | Byte client → upstream / upstream → client | Object qua verifier |
| --- | --- | --- | --- |
| Direct baseline | PUT/GET và SHA-256 pass | Không qua Faultline | Nội dung đúng |
| Proxied baseline | PUT/GET, TLS/SigV4 và SHA-256 pass | Counter flow tăng | Nội dung đúng |
| Disconnect | Lỗi sau 19 ms | 0 / 0 | Không tồn tại |
| Hold response | Timeout sau 3003 ms | 68136 / 5591 | Không tồn tại |
| Hold upload | Timeout sau 3006 ms | 4096 / 6291 | Không tồn tại |
| Slow upload | Thành công sau 2243 ms | 68145 / 6891 | Tồn tại |
| Slow response | Thành công sau 2473 ms | 2692 / 72392 | Tồn tại, nội dung đúng |
| Cut after bytes | Lỗi sau 24 ms | 4096 / 5591 | Không tồn tại |
| Delay connect | Thành công sau 2128 ms | 68147 / 6856 | Tồn tại |

CLI thật `validate`, `serve`, `enable`, `status`, `reload` và `disable` pass. Mỗi fault có `selected/reached/applied`, revision đúng và counter applied tăng. Trạng thái object được lấy bằng kết nối verifier độc lập; kết quả SDK timeout không được dùng để suy ra S3 đã ghi hay chưa.

Harness chuẩn hóa `S3_TEST_HOST` từ hostname hoặc URL HTTPS, suy ra virtual-hosted hostname khi giá trị bằng `S3_ENDPOINT`, và đưa hostname đó vào Compose mà không sửa `.env`. Fixture đặt request/response checksum thành `when_required`; mặc định `when_supported` của SDK đã bị endpoint test từ chối bằng `XAmzContentSHA256Mismatch` trước khi cấu hình tương thích này được áp dụng.

## Verification gates

| Check | Result |
| --- | --- |
| `go test ./internal/proxy/tcp -count=3` | PASS sau khi thêm snapshot/reload và lifecycle coverage. |
| `go test -race ./internal/proxy/tcp -count=1` | PASS với toàn bộ test TCP cuối cùng. |
| `go test ./...` | PASS; integration package 31.778 s. |
| `go test -race ./...` | PASS; full protocol regression, integration package 63.542 s. |
| `go vet ./...` | PASS. |
| `node --test internal/control/remote/app_test.cjs internal/control/remote/diff_test.cjs` | PASS, 13/13. |
| Python compile, hostname/checksum diagnostic helpers, Compose config và `git diff --check` | PASS. |
| Docker images và P28 harness | PASS; Faultline scratch image, Go SDK fixture, non-root bind 443 và full real-S3 route. |

## Giới hạn đã nghiệm thu

- TCP connection là một flow; byte counters đo TLS ciphertext, không đo object hoặc HTTP body.
- SDK fixture đặt `maxAttempts=1` và mỗi operation chạy trong process/container riêng. Retry, pooled connection và hành vi upload của Node SDK cần test riêng nếu muốn claim tương đương ở mức client.
- DNS route hỗ trợ một hostname cố định. Redirect sang hostname khác, acceleration, multi-region và wildcard DNS nằm ngoài phạm vi.
- Harness gọi `DeleteObject` cho mọi key đã tạo. Bucket có versioning có thể giữ version cũ hoặc delete marker; cần kiểm tra prefix test nếu yêu cầu xóa vật lý mọi version.
- Faultline không giải mã TLS, không giữ credentials và không biết S3 operation/status. Object tồn tại sau timeout chỉ được xác nhận bằng verifier độc lập.
