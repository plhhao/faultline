# Phase 10 — S3 qua TCP/TLS passthrough

Phạm vi: **Sau MVP**. Trạng thái: **Done**; adapter, Docker route và nghiệm thu endpoint S3 thật đã hoàn tất.

## Phạm vi đã chốt

Ứng dụng Docker Linux giữ nguyên code, image, `S3_ENDPOINT` và credentials. Chỉ container ứng dụng ánh xạ đúng hostname S3 mà SDK gọi tới IP Faultline bằng `extra_hosts`. Faultline chuyển byte TLS đến AWS S3 thật qua DNS độc lập; SDK vẫn kiểm tra chứng chỉ AWS và ký SigV4 như trước. Faultline không nhận credentials, không giải mã TLS và không lưu object.

Một TCP connection là một flow. Bốn primitive trong phạm vi là đóng kết nối theo trigger, giữ một chiều, throttle một chiều và trì hoãn dial upstream; [bảng nghiệm thu P28](03-s3-delivery.md#verify) ánh xạ chúng tới bảy ca lỗi mạng. Proxy không biết operation, bucket/key, HTTP status hoặc S3 đã ghi object thành công; test kiểm tra trạng thái object qua kết nối độc lập. Chỉ hỗ trợ hostname S3 cố định đã xác nhận trong bản đầu; DNS wildcard, TPROXY và adapter ký lại nằm ngoài phạm vi.

## Các plan

| Plan | Tính năng | Phụ thuộc | Trạng thái |
| --- | --- | --- | --- |
| [P26](01-tcp-contract.md) | Contract TCP/TLS, fault, routing và fixture | P15; nền config/control hiện có | Done |
| [P27](02-tcp-adapter.md) | Adapter TCP, CLI/API/UI, lifecycle và regression | P26 | Done |
| [P28](03-s3-delivery.md) | Docker DNS route, Go SDK và S3 thật opt-in | P27 | Done |

## Thứ tự và điều kiện hoàn tất

P26 → P27 → P28 đã hoàn tất. [Contract](contract.md), adapter, fixture TLS, Docker Compose và Go SDK opt-in đã được kiểm tra. [Acceptance](acceptance.md) ghi lệnh, kết quả và giới hạn. P28 chỉ chạy S3 thật khi người dùng điền env và bật test rõ ràng. Không ghi credentials vào repo hoặc log.

Phase đạt P26-AC1–AC4, P27-AC1–AC6 và P28-AC1–AC5, gồm bảy ca qua binary/CLI, Docker runtime và real-S3 opt-in. Go SDK xác minh đường truyền và fault; hành vi retry/upload của Node SDK cần bằng chứng riêng nếu được công bố. Không coi client timeout là bằng chứng S3 đã ghi thành công.

Xem [lộ trình và quy tắc thực hiện](../README.md), [đặc tả](../../specific.md), [Docker `extra_hosts`](https://docs.docker.com/reference/compose-file/services/#extra_hosts) và [AWS virtual-hosted/path-style](https://docs.aws.amazon.com/AmazonS3/latest/userguide/VirtualHosting.html).
