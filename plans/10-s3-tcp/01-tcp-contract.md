# P26 — Contract TCP/TLS và lỗi theo chiều byte

- Trạng thái: **Done**; contract, fixture TLS local và route hostname thực tế của SDK S3 đã được kiểm chứng.
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md), nền config/control/recorder; không phụ thuộc Phase 8/9.
- Nguồn: [phạm vi Phase 10](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi dự kiến: contract và schema/capability đề xuất; fixture TLS kín để đo và kiểm tra giả định.

## DEFINE

Chốt một adapter TCP chuyển byte TLS giữa app và một upstream cố định. TLS/SigV4 end-to-end, không terminate TLS, không parse HTTP/S3 và không claim biết kết quả `PutObject`. Một connection là một flow; selector đếm connection eligible, không đếm request. Phạm vi fault bản đầu: đóng hai leg, giữ một chiều, throttle một chiều và trì hoãn dial upstream. Một action/connection.

Route Docker dùng hostname chính xác mà SDK thực sự gọi (`bucket.s3.region.amazonaws.com` hoặc `s3.region.amazonaws.com`); `extra_hosts` chỉ có trong container app. Container Faultline phải resolve hostname AWS thật và không được dial lại listener. Endpoint động, DNS wildcard, TPROXY, S3 signing lại và matcher operation/key không thuộc contract này.

## PLAN → BUILD

1. Quan sát hostname/port/SNI của một fixture SDK không inject. Ghi điều kiện dùng virtual-hosted hoặc path-style, redirect region, tên bucket có dấu chấm và trường hợp nhiều hostname. Chọn một hostname cố định cho ví dụ; không giả định `S3_ENDPOINT` luôn là hostname cuối cùng trên dây.
2. Chốt schema `protocol: tcp`, `listen`, upstream `host:port`, matcher rỗng và validation. Không tái dùng phase HTTP như `after_upstream_headers`. Chốt tên action, trigger theo thời gian hoặc N byte đã chuyển tiếp, direction `client_to_upstream`/`upstream_to_client`, tham số timeout/rate và trường hợp ngưỡng 0, EOF, trigger không tới. Byte đếm gồm TLS handshake/framing, không phải byte object.
3. Chốt matrix action × trigger × direction. `hold` ngừng đọc/chuyển chiều được chọn với backpressure có hạn rồi đóng; `close_connection` nêu rõ FIN hoặc RST được bảo đảm, dùng trigger thời gian/byte cho cả ngắt sớm và cắt luồng; `throttle` giới hạn tốc độ theo mỗi chiều không có burst vô hạn; `delay_connect` chờ có hạn trước khi dial AWS, kể cả khi client hủy trong lúc chờ. Trigger đến sau khi byte đã gửi không thể thu hồi byte đó; không hứa chặn chính xác response HTTP. Chốt thứ tự kích hoạt khi hai chiều cùng tiến triển.
4. Chốt giới hạn listener, dial, inflight connection, idle/flow timeout, buffer, shutdown và cleanup. Snapshot/revision pin lúc accept; reload/disable chỉ ảnh hưởng connection mới. Một connection tái sử dụng có thể chứa nhiều request, retry không nhất thiết tạo connection mới.
5. Chốt recorder/API/UI capability: selected/reached/applied/not_reached, byte từng chiều, trigger và outcome connection. Không ghi TLS payload, signed URL, access key, SNI đầy đủ chứa bucket hoặc IP client vào event mặc định. Xác định quyền bind cổng 443 tối thiểu cho container non-root, không dùng privileged.
6. Tạo fixture TLS local với chứng chỉ cho hostname thử nghiệm; client map DNS tới proxy nhưng tự kiểm tra chứng chỉ upstream. Có trace pass-through, cert sai, hold quá sớm chặn handshake, byte threshold và connection reuse để kiểm chứng contract trước code adapter.

Quyết định schema và matrix đã ghi tại [contract.md](contract.md). Trace fixture local và các gate chưa chạy được ghi tại [acceptance.md](acceptance.md).

## VERIFY

| ID | Tiêu chí |
| --- | --- |
| P26-AC1 | Contract xác định topology DNS của hai container, hostname cố định, TLS/SigV4 passthrough và các trường hợp nhiều hostname/redirect bị giới hạn rõ. |
| P26-AC2 | Có schema/matrix cho bốn primitive và bảy ca lỗi, action × trigger × direction, nghĩa byte/connection, validation, snapshot và giới hạn tài nguyên; không dùng tên phase HTTP gây hiểu nhầm. |
| P26-AC3 | Fixture/trace TLS kín chứng minh handshake, cert validation, hai chiều byte và các thời điểm trigger; không suy ra HTTP status hoặc upload success từ ciphertext. |
| P26-AC4 | Contract định nghĩa event/capability và secret redaction; fixture và điều kiện môi trường đủ làm đầu vào P27/P28. |

**Đối chiếu hoàn tất:** AC1–AC4 pass. Fixture local kiểm tra TLS/SNI, certificate, hai chiều byte, bảy fault và trigger không tới. Nghiệm thu S3 thật xác nhận SDK dùng virtual-hosted hostname suy ra từ endpoint và bucket, TLS/SigV4 giữ nguyên qua DNS route. Endpoint cố định không redirect trong lần chạy; harness P28 dùng một process SDK cho mỗi operation nên không dùng kết quả đó để claim connection reuse của SDK ứng dụng.

## REVIEW

Đã đối chiếu contract với TLS/SigV4 và AWS SDK Go v2 trên endpoint S3 thật. Faultline vẫn chỉ quan sát ciphertext/connection; không quảng bá fault chiều về là “sau khi S3 thành công”. Không còn quyết định schema chặn P27.
