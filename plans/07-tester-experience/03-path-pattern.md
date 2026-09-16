# P20a — Path pattern cho rule matcher

- Trạng thái: **Done**
- Phụ thuộc: [P19](01-admin-api.md), phần form rule đã BUILD của [P20](02-tester-ui.md); không cần đợi nghiệm thu UI P20 để bắt đầu.
- Nguồn: yêu cầu bổ sung path pattern ngày **2026-09-15**; [phạm vi phase 07](README.md).
- Vùng thay đổi dự kiến: `internal/config/`, `internal/engine/`, `internal/control/remote/`, kiểm thử adapter/control liên quan, `examples/tester/`, `specific.md`.

## DEFINE

Cho phép một rule khớp các URL có ID động, ví dụ `/payments/:id`, qua file cấu hình và UI/API. Hiện tại `match.path` chỉ khớp chính xác; query params đã được chuyển tiếp tới upstream và không tham gia path matching.

### Contract đã chốt cho bản đầu

Người dùng đã xác nhận semantics và yêu cầu hiện thực. Bản đầu hỗ trợ `:param`; không gồm wildcard.

```yaml
match:
  method: GET
  path_pattern: /payments/:id
```

- Thêm `match.path_pattern` (JSON API: `Match.PathPattern`); giữ semantics exact của `match.path`, kể cả dấu `:` literal. Không tự diễn giải config cũ thành pattern.
- Không cho đặt đồng thời hai trường path khác rỗng. Không đặt cả hai nghĩa là không lọc theo path.
- `:name` chiếm toàn bộ một đoạn không rỗng giữa các dấu `/`. Tên có dạng `[A-Za-z_][A-Za-z0-9_]*`, không trùng trong cùng pattern; có thể có nhiều tham số như `/payments/:id/items/:item_id`.
- So khớp toàn bộ path, phân biệt hoa/thường và dấu `/` cuối; `/payments/:id` khớp `/payments/123`, không khớp `/payments/`, `/payments/123/` hoặc `/payments/123/items`.
- Pattern phải bắt đầu bằng `/`, không chứa query, fragment, CR/LF. Từ chối tham số sai cú pháp, tham số nằm giữa literal như `item-:id`, và wildcard `*`/`**`; lỗi chỉ rõ trường cấu hình.
- Dùng cùng biểu diễn path mà adapter hiện cung cấp cho exact matcher; không thêm URL decoding hoặc chuẩn hóa slash. Adapter dùng `r.URL.Path` đã decode: `%2F` tạo dấu phân đoạn, `%252F` chỉ decode một lần; integration tests kiểm chứng raw URL gửi upstream.
- Các điều kiện method/service/header/path kết hợp AND. Giữ thứ tự first-match: rule disabled bị bỏ qua; rule enabled đầu tiên khớp vẫn sở hữu request kể cả khi selector không chọn fault.
- Pattern chỉ quyết định khớp rule; không trích xuất tham số để nội suy fault, không rewrite URL. Query params tiếp tục được chuyển tiếp với request tới upstream; không thêm query matcher.
- Dùng chung engine cho HTTP/1, HTTP/2 và metadata path của gRPC; giữ matcher service/method hiện tại.

Ngoài phạm vi: regex, wildcard, optional segment, route specificity/ranking, path rewrite và thay đổi cơ chế forwarding query.

## PLAN → BUILD

1. **Schema và validation:** thêm trường, validation cú pháp và kiểm tra exact/pattern loại trừ nhau. Kiểm tra parse/encode/clone/fingerprint để config cũ, no-op và persistence hoạt động đúng; cập nhật contract trong `specific.md` khi hiện thực.
2. **Engine:** thêm so khớp theo đoạn, dùng chung cho mọi entry point; tránh parse lại pattern cho từng request nếu cần chuẩn bị cấu trúc matcher theo snapshot. Không thêm routing framework hay thay đổi ownership/selector.
3. **Control và API:** đưa trường mới qua validate, diff, apply, reload và lưu bền. Thay đổi rule có hiệu lực cho request mới qua CLI reload hoặc API apply, không cần restart; lỗi giữ active config/revision cũ.
4. **UI:** cho chọn không lọc path / exact / pattern, hiển thị ví dụ và lỗi phía server. Đổi loại phải xóa trường đối lập khỏi draft gửi lên; viewer vẫn chỉ xem. Giữ draft khi lỗi/conflict và hiển thị pattern trong diff.
5. **Tài liệu và fixture:** bổ sung rule mẫu, request khớp/không khớp và checklist tương tác vào `examples/tester/README.md`; dùng upstream có thể quan sát path/query để kiểm chứng forwarding. Ghi kết quả vào acceptance của phase.

## VERIFY

| ID | Tiêu chí nghiệm thu |
| --- | --- |
| P20a-AC1 | Config có pattern hợp lệ parse/encode round-trip; exact cũ giữ nguyên. Từ chối exact + pattern, thiếu `/`, query/fragment, tên tham số lỗi/trùng, tham số nhúng và wildcard; lỗi trỏ tới field. |
| P20a-AC2 | Unit tests bao phủ một/nhiều tham số, segment rỗng, thêm segment, trailing slash, hoa/thường, root, percent-encoding và query không tham gia match; method/service/header vẫn AND. |
| P20a-AC3 | Rule disabled cho phép rule sau khớp; hai rule chồng lấn giữ thứ tự khai báo; selector không chọn fault không làm rơi xuống rule tiếp theo. |
| P20a-AC4 | CLI reload và API validate/apply nhận pattern không cần restart; request mới dùng revision mới, request đang chạy giữ snapshot cũ. No-op không tăng revision; lỗi/conflict giữ active; restart managed mode khôi phục pattern đã apply. |
| P20a-AC5 | Integration HTTP/1 và HTTP/2 chứng minh pattern điều khiển fault; request thực sự được forward giữ path và raw query (gồm key lặp, giá trị rỗng, percent-encoding). Có regression gRPC cho path và service/method matcher. |
| P20a-AC6 | Người dùng kiểm thử UI tạo/sửa exact và pattern → validate → diff → apply → thấy traffic đổi; lỗi giữ draft, viewer không sửa được. Có checklist riêng và ghi rõ kết quả pending cho tới khi người dùng xác nhận. |

Agent chạy unit/integration tests phù hợp, Go race/vet và JavaScript syntax check; kiểm tra tài liệu/link. Kiểm thử tương tác UI do người dùng hỗ trợ theo thỏa thuận phase 7. Các lệnh và kết quả chỉ ghi PASS sau khi thực sự chạy; bản plan này chưa có bằng chứng runtime.

## REVIEW

- Không đổi ngầm semantics exact, representation của path, query forwarding, thứ tự rule hoặc selector.
- Validation dùng chung cho file/API; frontend không có matcher riêng.
- Diff, revision, persistence và config cũ tương thích; không thay đổi listener/upstream/TLS.
- Rà soát test request thực sự tới upstream, tránh dùng fault respond để suy luận query đã forward.

## Tiến độ

- 2026-09-15: tạo plan và contract đề xuất; chưa BUILD. Phạm vi bản đầu đề xuất chỉ `:param`, wildcard chưa chốt và ngoài phạm vi.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện). Hoàn tất tài liệu kế hoạch không đồng nghĩa tính năng đã Done.

- 2026-09-15: người dùng chốt semantics `:id` khớp cả `history`, ưu tiên theo thứ tự rule; bắt đầu BUILD. Matcher dùng `r.URL.Path` đã decode như exact hiện tại; `%2F` là dấu phân đoạn, không decode thêm.

## Kết quả BUILD / VERIFY / REVIEW

- Đã thêm schema/validation, matcher dùng `strings.Cut` theo đoạn không tạo slice/regex cho mỗi request; field tự đi qua config clone, encode, fingerprint, API và persistence.
- UI có Any/Exact/Pattern, xóa trường đối lập khi đổi loại, giữ loại đang chọn trong draft và kiểm tra ô trống cả khi chuyển proxy. Server là nguồn validation cú pháp.
- `go test -race ./... -timeout 180s`, `go vet ./...`, CLI build/validate fixture và `node --check internal/control/remote/ui/app.js` PASS. Network tests chạy với quyền mở localhost sau khi sandbox chặn lần đầu. Docker opt-in không chạy lại cho thay đổi này.
- P20a-AC1–AC3: unit config/engine PASS. P20a-AC4: file reload qua control, API validate/apply/conflict/no-op và managed restart PASS; bộ regression snapshot đang chạy PASS. P20a-AC5: HTTP/1–HTTP/2 raw URL/query, percent-encoding và gRPC selector/reload PASS.
- REVIEW: giữ exact, first-match và selector; không rewrite query hay thay đổi hạ tầng. Hướng dẫn mới tại [checklist P20a](../../examples/tester/README.md#p20a--kiểm-thử-path-pattern).
- P20a-AC6 **pending người dùng kiểm thử tương tác UI**; P20a và phase 7 giữ In progress. Không coi syntax/API test là browser E2E.

## Nghiệm thu hoàn tất — 2026-09-16

Người dùng xác nhận mọi case trong [hướng dẫn tester](../../examples/tester/README.md) PASS: U1–U9, vận hành config, Docker, path pattern và highlight diff. Kết hợp bằng chứng tự động đã ghi, P19/P20/P20a và Phase 7 được đánh dấu **Done**. Các ghi chú pending bên trên là lịch sử trước nghiệm thu. Lần cập nhật này chỉ sửa tài liệu, không chạy lại runtime tests.
