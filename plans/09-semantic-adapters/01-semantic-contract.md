# P23 — Contract PostgreSQL và điểm áp dụng fault

- Trạng thái: **Done**
- Phụ thuộc: [P15](../05-mvp-delivery/03-binary-docker.md); rà soát TLS/control của phase 6–7.
- Nguồn: [specific.md](../../specific.md), mục 9.1, 13; [phạm vi Phase 9](README.md).
- Vùng thay đổi dự kiến: tài liệu contract/capability, schema đề xuất và thiết kế adapter PostgreSQL.

## DEFINE

Chốt cách quan sát giao thức PostgreSQL để delay, hold hoặc ngắt kết nối tại điểm đã công bố. Chỉ khi có bằng chứng xác nhận COMMIT thành công mới gắn nhãn tình huống mất xác nhận commit. Tách kết quả database khỏi lỗi client nhìn thấy.

Phạm vi: kết nối trực tiếp, SCRAM-SHA-256, thường/TLS, transaction tường minh, fixture Go + PostgreSQL Docker. Không thêm Node.js/TypeORM, không nghiệm thu tắt transaction/auto-commit; các giới hạn khác theo README phase.

## PLAN → BUILD

1. Pin PostgreSQL major/image và driver Go/version; dùng tài liệu chính thức và trace fixture để chốt startup, authentication, query/response và transaction lifecycle. Lựa chọn kỹ thuật do người hiện thực quyết định, ghi lại lý do.
2. Lập capability matrix: simple/extended query theo nhu cầu driver fixture, prepared statements, error recovery, pooling phía client, cancellation, TLS từng chặng và SCRAM/channel binding. Chốt rõ phần hỗ trợ/phần từ chối; không im lặng hạ mức bảo mật. TLS cần giải mã ở proxy để quan sát semantics, không coi tunnel mã hóa là semantic adapter.
3. Chốt đơn vị flow (session, command hay transaction), snapshot/revision và selector counters. Quy định reload/disable trên kết nối dài hạn, thứ tự rule, một fault/flow và timeout/cleanup; không tự áp dụng định nghĩa HTTP request sang PostgreSQL.
4. Thiết kế matrix action × phase. Các điểm cần đánh giá: trước chuyển command, trước chuyển response và bắt buộc sau xác nhận COMMIT thành công trước khi client nhận xác nhận. Pin tên config/phase và semantics delay (tiếp tục), hold (chờ có giới hạn rồi kết thúc), disconnect. Chốt đối với partial response, transaction lỗi/rollback và cancellation; không nhận diện bằng tìm chuỗi COMMIT đơn thuần.
5. Chốt parser/state machine và giới hạn frame/buffer; quy định khi gặp version/operation ngoài phạm vi (từ chối có lỗi rõ hoặc pass-through không claim semantics). Chốt match metadata tối thiểu, không thêm parser SQL tổng quát.
6. Thiết kế config/capability và cách CLI/API/UI hiện tại biểu diễn PostgreSQL. Không dùng field HTTP path/method cho PostgreSQL; chỉ thêm field thật sự cần. Chốt TLS trust, startup credentials forwarding và logging không lộ secrets/SQL parameters.
7. Thiết kế fixture: transaction ghi dữ liệu rồi commit; kết nối kiểm tra độc lập xác nhận dữ liệu dù client lỗi. Demo retry có/không mã thao tác duy nhất; không kết luận rollback chỉ từ exception hoặc timeout.

## VERIFY

| ID | Tiêu chí |
| --- | --- |
| P23-AC1 | Pin phiên bản và có capability matrix cho protocol, driver, authentication/TLS; các giới hạn được ghi rõ. |
| P23-AC2 | Có trace/state diagram cho thành công, lỗi/rollback, commit thành công rồi mất phản hồi; xác định chính xác bằng chứng và dữ liệu được giữ lại. |
| P23-AC3 | Contract xác định flow, matcher, action × phase, snapshot/reload/disable, cancellation và resource bounds; config lỗi bị từ chối. |
| P23-AC4 | Fixture và nguồn bằng chứng độc lập đủ kiểm tra P24; không phụ thuộc Phase 8 hoặc TypeORM. |

## REVIEW

Các quyết định được chốt trong [contract](contract.md); đối chiếu trace fixture trong [acceptance](acceptance.md). Không claim commit thành công từ việc thấy client gửi COMMIT; không hứa TLS/SCRAM tương thích trước khi giải quyết handshake và channel binding. Giữ hành vi HTTP/HTTP2/gRPC hiện có.

## Tiến độ

P23-AC1–AC4 hoàn tất: PostgreSQL 17.11, pgx v5.7.6, phase `after_commit`; có contract, state traces và fixture độc lập. TLS/SCRAM thường đã được kiểm chứng; SCRAM-PLUS bị từ chối rõ, không sửa mechanism để hạ bảo mật. Xem [bằng chứng nghiệm thu](acceptance.md).
