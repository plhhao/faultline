# P28 — Docker route và nghiệm thu AWS S3 thật

- Trạng thái: **Done**; direct/proxied baseline và bảy ca fault đã pass với endpoint S3 thật qua Docker/Go SDK.
- Phụ thuộc: [P27](02-tcp-adapter.md) hoàn tất; cần bucket/prefix test và credentials do người dùng điền để chạy AWS opt-in.
- Nguồn: [phạm vi Phase 10](README.md), [đặc tả](../../specific.md).
- Vùng thay đổi dự kiến: `examples/s3/`, `tests/integration/`, Docker fixture, hướng dẫn và `acceptance.md`.

## DEFINE

Chứng minh app/SDK không sửa code hoặc image vẫn gọi HTTPS/SigV4 tới AWS S3 thật qua Faultline khi chỉ đổi DNS trong Docker. Kiểm tra kết quả object bằng kết nối trực tiếp sau lỗi SDK, không suy ra trạng thái S3 từ client timeout. Go SDK v2 là fixture tự động; không claim hành vi retry/upload của Node SDK nếu chưa test Node riêng.

## PLAN → BUILD

1. Dùng `examples/s3/.env.example` và `.env` đã tạo ở bước lập plan; người dùng điền secret vào `.env` bị Git ignore. Giữ `S3_REGION`, `S3_ENDPOINT`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `S3_BUCKET_NAME`, `S3_PUBLIC_URL`; thêm biến test cho hostname/IP/prefix nếu cần. Test AWS chỉ chạy với `FAULTLINE_S3_TEST=1` và env đầy đủ: flag tắt thì SKIP, flag bật nhưng thiếu biến bắt buộc thì FAIL trước khi gọi AWS. Không in env, signed URL hay credential.
2. Compose app/Go fixture và Faultline trên test network có IP Faultline cố định. Chỉ app/fixture có `extra_hosts` ánh xạ **hostname SDK thực sự gọi** tới IP đó; Faultline không có override, dial AWS:443 bằng DNS thật. Listener 443 chỉ nằm trong test network, bind bằng quyền tối thiểu cho non-root. Kiểm tra DNS hai container, đường đi, certificate và baseline pass-through khi injection tắt.
3. Fixture AWS SDK for Go v2 pin version khi triển khai, `PutObject` nhỏ với key ngẫu nhiên dưới prefix test. Cùng code fixture chạy trực tiếp và qua Docker DNS route; chỉ môi trường mạng khác. Dùng `maxAttempts=1` cho case một attempt, tách case retry. Container verifier riêng không có DNS override gọi `HeadObject`/`GetObject`, so nội dung/kích thước/checksum thay vì chỉ ETag.
4. Chạy đủ bảy ca trong bảng VERIFY bằng binary/CLI thật và Go SDK qua AWS thật. Lưu lỗi/timing SDK, events/counters và trạng thái object độc lập. Không yêu cầu object phải tồn tại ở mọi lần ngắt; nếu cần ca “object tồn tại nhưng SDK lỗi”, chọn trigger/fixture và chỉ ghi PASS khi kiểm tra độc lập xác nhận.
5. Chỉ tạo/xóa key do test sở hữu, không đụng key ứng dụng. Nếu bucket bật versioning, kiểm tra/dọn version hoặc ghi rõ phần còn lại; không tuyên bố `DeleteObject` xóa mọi version. Hướng dẫn dừng Compose và khôi phục DNS. `S3_PUBLIC_URL` chỉ ảnh hưởng URL app tạo nếu được dùng, không phải route upload SDK.
6. Test harness build binary rồi gọi CLI thật: `validate`, `serve`, `enable`, `status`, `reload`, `disable` khi thực hiện từng fault; kiểm tra counters và revision qua CLI, không chỉ gọi Go adapter in-process. Chạy Go tests/race/vet, Docker build/runtime và regression hiện có. Ghi lệnh, PASS/FAIL/SKIP, phiên bản SDK/region/bucket loại test và giới hạn tại `acceptance.md`. Smoke Node/app thực tế là gate riêng nếu muốn claim Node SDK tương đương; không sửa source app để thêm test.

## VERIFY

Ma trận nghiệm thu dự kiến:

| Ca | Cách Faultline tác động | Bằng chứng PASS qua CLI/binary và Go SDK |
| --- | --- | --- |
| Baseline | Direct và qua Faultline khi injection tắt | Cùng hostname/SigV4, TLS hợp lệ, PUT/GET và nội dung đúng; event proxy chỉ ở đường được route. |
| Ngắt kết nối | `close_connection` sau thời gian hoặc N byte | CLI báo fault applied, SDK thấy lỗi kết nối; verifier trực tiếp ghi object có/không tồn tại. |
| Chặn chiều trả về | `hold` chiều upstream → client sau trigger | SDK hết hạn đọc/timeout, event applied; không khẳng định phản hồi HTTP đã bắt đầu. |
| Chặn chiều upload | `hold` chiều client → upstream sau trigger | Upload không hoàn tất trong deadline; SDK báo timeout/hủy, slot/socket được giải phóng. |
| Upload chậm | `throttle` chiều client → upstream | Thời gian truyền payload lớn hơn baseline theo bound P26; SDK có thể thành công hoặc timeout tùy deadline. |
| Download/phản hồi chậm | `throttle` chiều upstream → client | `GetObject`/body về chậm theo bound; dữ liệu đúng nếu hoàn tất. |
| Cắt luồng giữa chừng | `close_connection` sau N byte đã chuyển tiếp | SDK thấy EOF/reset/lỗi TLS, event và byte counter khớp trigger; không gọi đây là cắt HTTP body chính xác. |
| Trễ kết nối | `delay_connect` trước khi dial AWS | TLS handshake/connect bị trễ theo bound; với deadline ngắn SDK timeout. |
| Retry và cleanup | Chạy riêng sau các fault | Ghi số attempt/connection riêng, key test duy nhất và trạng thái cleanup/versioning. |
| Hồi quy và packaging | Fixture TLS local, toàn repo, Docker | Go tests/race/vet, CLI build, Docker build/runtime và protocol cũ pass. |

Lệnh thực tế cho AWS opt-in là `python3 examples/s3/run.py`; harness gọi CLI trong Compose để `validate/serve/enable/status/reload/disable`. Lệnh và kết quả local đã ghi tại [acceptance.md](acceptance.md). Không chạy AWS trong suite mặc định.

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P28-AC1 | Compose ánh xạ duy nhất hostname S3 ở app/fixture, Faultline resolve AWS thật; app code/image và S3 endpoint giữ nguyên, TLS/SigV4 baseline qua proxy pass. |
| P28-AC2 | Go SDK AWS opt-in: baseline PUT/GET pass và ít nhất một fault gây lỗi/timeout SDK; kiểm tra độc lập xác nhận trạng thái object, không suy diễn success. Flag tắt ghi SKIP; flag bật nhưng thiếu env bắt buộc ghi FAIL trước khi gọi AWS. |
| P28-AC3 | Cả bảy ca trong ma trận có bằng chứng CLI/event, lỗi hoặc timing SDK và byte counters; case retry và connection reuse được ghi rõ giới hạn. |
| P28-AC4 | Test chỉ dùng key/prefix riêng, cleanup và versioning được báo cáo; `.env` bị ignore, ví dụ không chứa secret và không public listener ra Internet. |
| P28-AC5 | CLI binary validate/serve/enable/status/reload/disable, Docker runtime, full Go tests/race/vet và hồi quy protocol cũ pass; tài liệu nêu rõ scope connection, hostname cố định và khác biệt Go/Node. |

**Đối chiếu hoàn tất:** P28-AC1–AC5 pass. Harness xác nhận direct baseline và proxied baseline PUT/GET đúng nội dung, cả bảy fault có CLI/event/counter/timing và trạng thái object độc lập, rồi xóa các key test và hạ project Compose. `.env` không được log hoặc đưa vào build context. Xem số liệu tại [acceptance.md](acceptance.md).

## REVIEW

Đã review route app → Faultline → S3, DNS độc lập, virtual-hosted hostname, chứng chỉ, cleanup object và secrets. Harness chấp nhận hostname hoặc URL HTTPS, dùng checksum `when_required` để tương thích endpoint S3, và vẫn giữ một attempt để kết quả fault xác định. Retry/connection pooling của Node SDK nằm ngoài claim tự động này.
