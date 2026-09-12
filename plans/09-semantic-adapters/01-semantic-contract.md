# P23 — Chọn công nghệ và semantic contract

- Trạng thái: **Deferred**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 9.1, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `specific.md và tài liệu adapter được chọn`

## DEFINE

Chọn đúng một PostgreSQL/AMQP/Kafka use case; xác định protocol, routing và ý nghĩa các điểm lỗi trước coding.

## PLAN → BUILD

1. Chọn công nghệ theo dependency thực, mô tả tình huống commit/confirm/ack cần test và nguồn evidence.
2. Lập capability matrix với version, authentication/TLS, flow unit, phase/scope; nêu phần wire protocol hỗ trợ.
3. Nếu Kafka, xác định routing địa chỉ broker được quảng bá; nếu database, phân biệt statement response và transaction commit.
4. Xác định phụ thuộc nền cần dùng từ P16/P17 sau khi chọn công nghệ, không yêu cầu mọi adapter trước đó.

## VERIFY

- Review một luồng request/response thực tế và kế hoạch integration fixture; mọi claim semantic có điểm quan sát cụ thể.
- Use case và acceptance criteria đủ rõ trước chuyển P24.

## REVIEW

Generic TCP không được quảng bá thành semantic database/broker adapter.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
