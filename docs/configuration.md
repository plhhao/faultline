# Cấu hình và rule

Ví dụ HTTP tối thiểu:

```yaml
api_version: faultline/v1alpha1
runtime:
  request_timeout: 30s
  max_inflight_requests: 100
proxies:
  - id: payment
    protocol: http1
    listen: 127.0.0.1:8080
    upstream: http://127.0.0.1:9000
    rules:
      - id: slow-payment
        enabled: true
        match: {method: POST, path: /payments}
        select: {probability: 0.25}
        fault: {action: delay, phase: before_upstream_request, duration: 2s}
```

Mỗi flow chỉ dùng rule phù hợp đầu tiên. `enabled: false` bỏ qua rule đó để xét
rule tiếp theo. Injection toàn cục vẫn phải được bật qua CLI hoặc UI.

## Match và selector

Điều kiện match kết hợp bằng AND. Để trống một điều kiện nghĩa là match mọi giá
trị của điều kiện đó.

| Trường | Ý nghĩa |
| --- | --- |
| `method` | HTTP method chính xác; method gốc vẫn được chuyển tiếp. |
| `path` | Path chính xác, không gồm query string. |
| `path_pattern` | Absolute path; `:name` match đúng một segment không rỗng. |
| `headers` | Object header/metadata, value là string chính xác. |
| `service` | Service gRPC chính xác. |

`path_pattern: /payment/:id` match `/payment/42` và `/payment/history`, nhưng
không match `/payment/42/items`. Đặt exact path `/payment/history` trước pattern
nếu history là ngoại lệ.

Chỉ dùng đúng một selector:

```yaml
select: {probability: 0.2} # 20% eligible flows
select: {nth: 3}           # eligible flow thứ ba
select: {every: 5}         # mỗi eligible flow thứ năm
```

Probability `0` pass-through nhưng vẫn là rule đã match; traffic không rơi xuống
rule tiếp theo. Selector chỉ đếm khi injection toàn cục bật.

## Fault HTTP, HTTP/2 và gRPC unary

| Action | Phase | Tham số |
| --- | --- | --- |
| `delay` | trước request hoặc sau upstream headers | `duration` |
| `respond` | `before_upstream_request` | `status`, `body` |
| `close_connection` | HTTP/1, trước request hoặc sau headers | không có |
| `hold_request` | trước request | `max_duration` |
| `hold_response` | sau upstream headers | `max_duration` |
| `truncate` | request hoặc response | `direction`, `bytes` |
| `throttle` | request hoặc response | `direction`, `bytes_per_second` |

`truncate` và `throttle` chọn phase theo direction: request dùng
`before_upstream_request`, response dùng `after_upstream_headers`. gRPC unary
không hỗ trợ `respond` hoặc `close_connection`. Chi tiết HTTP/2, gRPC và mTLS ở
[examples/grpc/README.md](../examples/grpc/README.md).

PostgreSQL/MySQL chỉ nhận matcher `{}`, phase `after_commit`, và ba action
`delay`, `hold_response`, `close_connection`.

## Reload hay restart

`reload` chỉ thay rule và seed. Listener, protocol, upstream, TLS và runtime cần
restart. Flow đang chạy giữ snapshot cũ; rule mới được publish atomically.

```bash
rtk proxy ./bin/faultline reload --config /absolute/path/config.yaml \
  --admin-socket /tmp/faultline-http/admin.sock
```

Multi-file config dùng `include` ở root; xem
[examples/http/multi-file/faultline.yaml](../examples/http/multi-file/faultline.yaml).
