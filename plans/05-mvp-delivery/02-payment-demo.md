# P14 — Demo lost response và nghiệm thu hành vi

- Trạng thái: **Done (2026-09-14)**
- Phụ thuộc: [P13](../05-mvp-delivery/01-resource-limits.md)
- Nguồn: [specific.md](../../specific.md), mục 12.
- Nghiệm thu MVP liên quan: AC1, AC2, AC3, AC4, AC5, AC6, AC7, AC8, AC9, AC10, AC11, AC12, AC13, AC14, AC15, AC16, AC17, AC18, AC19, AC20, AC21, AC22, AC23
- Vùng thay đổi dự kiến: `examples/http/, tests/integration/`

## DEFINE

Cung cấp demo dễ lặp lại và bằng chứng cho toàn bộ AC hành vi; packaging AC24 ở P15.

## PLAN → BUILD

1. Tạo payment dependency test ghi dữ liệu trước khi trả 201; test driver có retry, hai biến thể có/không idempotency.
2. Chờ app ready bằng cơ chế của demo, enable hold_response, quan sát timeout/retry rồi kiểm tra dữ liệu độc lập.
3. Config mẫu cho từng action/selector, hướng dẫn enable/reload/disable và đọc events; không đòi sửa code app thực.
4. Ánh xạ AC1–AC23 sang test cụ thể và kết quả; tái sử dụng tests đã có, bổ sung chỗ thiếu thay vì viết lại cùng coverage.

## VERIFY

- Cùng lost-response fault: biến thể không idempotency tạo trùng, biến thể có idempotency giữ một payment; assertion nằm trong test demo.
- Chạy test suite và demo từ hướng dẫn trên môi trường sạch; kiểm tra evidence theo AC, bao gồm fault qua bốn tổ hợp TLS.

## REVIEW

Không coi 201 là bằng chứng commit chung cho mọi upstream; demo không biến thành assertion DSL trong core.

## Kết quả VERIFY/REVIEW

- [Payment demo](../../examples/http/paymentdemo/README.md) gồm dependency in-memory,
  retry driver, config binary/Docker và hướng dẫn ready→enable→run→disable.
- `TestPaymentDemoBinary` chạy binary thật, CLI admin và demo driver; Docker dùng
  cùng driver contract. Cả hai biến thể đều timeout hai lần: 2 payment khi không
  có idempotency, 1 payment khi có; events ghi selected/reached/applied/upstream 201.
- [faults.yaml](../../examples/http/faults.yaml) minh họa cả năm action và ba selector;
  [bảng AC](acceptance.md) ánh xạ toàn bộ hành vi sang tests đã pass, gồm bốn tổ hợp TLS.
- Review: store chỉ là fixture, không bền qua restart; assertion ở demo/test,
  không đưa business assertions hoặc retry correlation vào core proxy.
