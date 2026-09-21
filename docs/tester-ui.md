# Tester UI

Tester UI cho editor tạo draft, validate, xem diff và apply qua HTTPS. Login độc
lập, không có SSO.

## Khởi tạo local

```bash
rtk proxy python3 examples/tester/setup.py
```

Script tạo binary, certificate localhost, config HTTP/gRPC và account editor/
viewer. Khởi động theo [hướng dẫn tester](../examples/tester/README.md), sau đó
mở `https://localhost:8443`.

## Luồng chỉnh rule

1. Chọn proxy và chỉnh rule trong draft.
2. Chọn **Validate draft & review diff**.
3. Đọc hai cột Active/Draft để xem field, rule hoặc thứ tự đã đổi.
4. Chọn **Apply reviewed draft**. Revision tăng nhưng injection không tự bật.
5. Bấm **Enable injection** để rule tác động traffic.

**Compare with latest active** chỉ cập nhật cột Active để so sánh, không sửa
draft. **Use latest revision as draft base** đổi base revision để resolve conflict;
draft hiện tại có thể ghi đè thay đổi editor khác. **Discard draft and load active**
bỏ toàn bộ draft và nạp rule active.

Draft giữ trong bộ nhớ tab. Reload hoặc đóng tab làm mất draft. Khi session hết
hạn, draft đã sửa được giữ để người dùng tự quyết định; draft chưa sửa sẽ nạp lại
từ active config.

Badge `Injection ON/OFF` là trạng thái server đã xác nhận. Status poll mỗi 5 giây;
dòng Updated thay đổi không có nghĩa một rule vừa apply. `Observed outcomes` là
quan sát của proxy, không chứng minh upstream đã rollback/commit. `Rule counters`
là eligible/selected của revision active.
