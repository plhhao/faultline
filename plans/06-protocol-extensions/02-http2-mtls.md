# P17 — HTTP/2 và mTLS theo nhu cầu

- Trạng thái: **Deferred**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 3.1, 9.1, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `internal/proxy/http/, internal/config/, tests/integration/`

## DEFINE

Mở rộng HTTP khi use case yêu cầu; HTTP/2 và mTLS là hai capability độc lập, không bắt buộc giao cùng lúc.

## PLAN → BUILD

1. Chốt phía nào cần mTLS và policy client certificate; chốt HTTP/2 stream-vs-connection fault scope trước implement.
2. Thêm schema/negotiation/validation riêng cho capability được chọn, giữ hành vi HTTP/1.1 cũ.
3. Nếu làm nền cho gRPC ở P16, cung cấp contract stream cancellation và status rõ ràng.

## VERIFY

- mTLS: cert hợp lệ, thiếu/sai cert ở đúng phía; HTTP/2: nhiều stream cùng connection, fault không lan ngoài scope.
- Regression HTTP/HTTPS MVP và reload field cần restart; kiểm chứng độc lập từng capability được chọn.

## REVIEW

Không diễn giải reset stream như close toàn connection.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
