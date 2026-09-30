# S3 qua TCP/TLS passthrough

Faultline chuyển nguyên byte TLS giữa SDK và S3. Ứng dụng giữ nguyên `S3_ENDPOINT`, credentials, chữ ký SigV4 và kiểm tra chứng chỉ. Chỉ DNS trong container chạy SDK được ánh xạ tới Faultline; container Faultline dùng DNS thường để tới S3 thật. Không cần sửa source hoặc image của ứng dụng.

## Chuẩn bị

1. Sao chép `.env.example` thành `.env` và điền thông tin của **bucket test riêng**. `.env` bị Git ignore. Không đưa credentials vào YAML hoặc log.
2. `S3_TEST_HOST` là **hostname chính xác** mà SDK gửi kết nối tới. Có thể điền hostname thuần hoặc URL HTTPS không có path; harness sẽ lấy hostname và chỉ chấp nhận cổng mặc định 443. Nếu điền đúng `S3_ENDPOINT`, harness Go mặc định suy ra virtual-hosted style bằng cách thêm bucket vào hostname; có thể điền hostname cuối cùng để dùng trực tiếp. Với path-style thường là `s3.<region>.amazonaws.com`. Redirect, multi-region, acceleration hoặc nhiều hostname cần route riêng.
3. `S3_TEST_PREFIX` là vùng key dành riêng cho test, ví dụ `faultline-tests/`. Test tạo tên ngẫu nhiên dưới prefix đó. `S3_PUBLIC_URL` chỉ phục vụ URL công khai do app tự tạo, không quyết định đường upload SDK.
4. Đặt `FAULTLINE_S3_TEST=1` để cho phép test AWS thật. Để `0` thì harness SKIP. Nếu bật nhưng thiếu biến bắt buộc, harness FAIL trước khi gọi AWS.

Chạy từ thư mục repository:

```sh
python3 examples/s3/run.py
```

Harness tạo project Compose tạm, build Faultline và fixture Go SDK v2, tạo tám config TCP, kiểm tra `validate`, khởi động `serve`, dùng `enable/status/reload/disable` qua Unix socket riêng, rồi chạy baseline và bảy fault. `sdk` có `extra_hosts` ánh xạ `S3_TEST_HOST` → `172.30.240.10`; `verifier` và `faultline` không có ánh xạ này. Không publish cổng 443 ra host. Container Faultline chạy non-root; network namespace cho phép bind 443 bằng `ip_unprivileged_port_start=0`. Harness dọn project/volume và gọi `DeleteObject` cho các key nó tạo khi kết thúc.

Các ca `disconnect`, `hold_response`, `hold_upload`, `cut` đòi SDK báo lỗi. Các ca `slow_upload`, `slow_response`, `delay_connect` kiểm tra thời gian tối thiểu và nội dung khi thành công. Sau mỗi ca, verifier độc lập gọi `HeadObject` để ghi nhận object có tồn tại hay không. `fault_applied` chỉ xác nhận thao tác TCP tại Faultline, không xác nhận S3 đã ghi object. Fixture cố định một attempt (`maxAttempts=1`); retry của app/Node SDK cần kiểm thử riêng. Bucket bật versioning có thể giữ các version cũ sau `DeleteObject`; kiểm tra prefix test sau chạy.

Các rule `hold_response`/`hold_upload` kích hoạt sau 4096 byte TLS chiều SDK → S3 để handshake có thể hoàn tất; `slow_response` kích hoạt sau 4096 byte chiều về. Mốc đó chỉ là ngưỡng byte mã hóa, không bảo đảm toàn bộ object đã tới S3 hay response HTTP đã bắt đầu.

## Cấu hình TCP

File được tạo bởi [generate-configs.sh](generate-configs.sh) khi Compose chạy. Một rule mẫu:

```yaml
api_version: faultline/v1alpha1
runtime:
  max_inflight_requests: 32
  request_timeout: 30s
proxies:
  - id: s3
    protocol: tcp
    listen: 0.0.0.0:443
    upstream: tcp://bucket.s3.region.amazonaws.com:443
    rules:
      - id: cut
        enabled: true
        match: {}
        select: {probability: 1}
        fault:
          phase: on_transfer
          action: close_connection
          after_bytes: 4096
          trigger_direction: client_to_upstream
```

Đếm byte là byte TLS đã chuyển tiếp trên **một connection**, không phải byte object hoặc số request. `hold` và `throttle` có `direction` riêng; trigger có thể dùng chiều khác. Xem [contract](../../plans/10-s3-tcp/contract.md) và [kết quả nghiệm thu](../../plans/10-s3-tcp/acceptance.md).
