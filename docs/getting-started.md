# Bắt đầu nhanh với HTTP

Ví dụ này dùng upstream HTTP tại `127.0.0.1:9000` và Faultline listener tại
`127.0.0.1:8080`.

## Build và kiểm tra cấu hình

Tại thư mục gốc repository:

```bash
rtk proxy go build -o bin/faultline ./cmd/faultline
rtk proxy ./bin/faultline validate --config examples/http/faultline.yaml
```

`validate` chỉ kiểm tra YAML và file TLS tham chiếu; nó không mở port hay gọi
upstream.

## Khởi động

Terminal 1 chạy demo upstream:

```bash
rtk proxy go run ./examples/http/paymentdemo/cmd serve --listen 127.0.0.1:9000
```

Terminal 2 chạy proxy:

```bash
rtk proxy ./bin/faultline serve \
  --config examples/http/faultline.yaml \
  --admin-socket /tmp/faultline-http/admin.sock
```

Faultline nhận traffic ngay, nhưng injection mặc định tắt. Request đến port 8080
được chuyển tiếp bình thường.

## Bật rule và quan sát

Terminal 3:

```bash
rtk proxy ./bin/faultline enable --admin-socket /tmp/faultline-http/admin.sock
rtk proxy curl -i http://127.0.0.1:8080/healthz
rtk proxy ./bin/faultline status --admin-socket /tmp/faultline-http/admin.sock
rtk proxy ./bin/faultline disable --admin-socket /tmp/faultline-http/admin.sock
```

`serve` ghi JSON event theo từng dòng ra stdout. Lưu event để phân tích bằng cách
redirect stdout sang file `events.ndjson`; readiness và lỗi vẫn đi ra stderr.

Đọc tiếp [Cấu hình](configuration.md) để thay rule và [Vận hành CLI](operations.md)
để reload an toàn.
