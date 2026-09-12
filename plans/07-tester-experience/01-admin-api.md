# P19 — Remote API, revision và quyền thao tác

- Trạng thái: **Deferred**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md)
- Nguồn: [specific.md](../../specific.md), mục 8.1, 11, 13.
- Nghiệm thu MVP liên quan: Không thuộc gate MVP.
- Vùng thay đổi dự kiến: `internal/control/, đường dẫn API/storage xác định khi DEFINE`

## DEFINE

Mở control service cho server test chung với ownership cấu hình và quyền thao tác rõ ràng.

## PLAN → BUILD

1. Chốt nguồn config chính/persistence và quan hệ với file/CLI; không cho nhiều nguồn ghi thiếu quy tắc.
2. Thiết kế API validate/draft/apply/status/enable/disable dựa trên control service có sẵn; revision precondition chống lost update.
3. Bổ sung authentication, authorization, audit và deployment transport phù hợp; lưu apply result và active revision.

## VERIFY

- Hai người cùng sửa gây conflict có thể xử lý; draft lỗi không đổi runtime.
- Người không có quyền không apply/toggle; audit đủ actor/revision/outcome, restart phục hồi theo policy đã chốt.

## REVIEW

Không tạo rule engine riêng cho API; mọi thay đổi persistence/injection startup semantics phải được đặc tả lại.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
