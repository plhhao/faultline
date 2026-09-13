# P01a — File gốc và include cấu hình proxy

- Trạng thái: **Done**
- Phụ thuộc: [P01](01-config-schema.md), [P02](02-runtime-snapshots.md), [P03](03-rule-engine.md).
- Nguồn: [specific.md](../../specific.md), mục 7.1 và 8.
- Nghiệm thu: MC1–MC4; regression AC11, AC12, AC23 ở tầng core.
- Vùng thay đổi: `internal/config/`, `internal/control/`, tests tương ứng và `examples/http/`.

## DEFINE

Nạp một file gốc và các file proxies thành một Document hợp lệ. Từ chối ID trùng, giữ đường dẫn nguồn cho diagnostics và giữ semantics snapshot/selector hiện tại. Phạm vi core đã được hiện thực; CLI/Docker tiếp tục theo các plan phụ thuộc.

## Kết quả hiện thực

- `Load` đọc root và fragments rồi validate cùng nhau; schema include chỉ ở loader. `Parse` standalone báo lỗi khi gặp include; control có `ReloadFile` cho cả tập file và giữ API reload bytes standalone.
- ID trùng báo cả hai vị trí; đường dẫn TLS theo file khai báo. Glob có thứ tự ổn định; kiểm tra danh tính file bằng `os.SameFile`, bao gồm symlink/hard link và self-include.
- Source metadata chỉ tồn tại khi nạp/validate, không đi vào Document hoặc fingerprint. Không thêm engine hay registry mới.
- MC1–MC4: unit tests cho bố cục tương đương, ID/fragment/include lỗi, TLS theo fragment, no-op/reset/old snapshot và reload đồng thời với acquire/toggle. Config một file và engine regression đều pass.
- VERIFY: `rtk proxy env GOCACHE=/private/tmp/faultline-go-build go test -race -cover ./...`, `go vet ./...` và `go build ./...` (cùng prefix/cache) pass. MC5 chưa chạy vì CLI/Docker thuộc P04/P11/P15.
- REVIEW: đã kiểm tra source remapping, scope ID, fingerprint và thứ tự apply; bỏ kiểm tra duplicate ID dư ở validator vì bước ghép đã chịu trách nhiệm. Graph chưa có node; review source trực tiếp. Không còn blocker cho core phase 1.

## PLAN → BUILD

1. Tách đọc tập file, decode và validate config hiệu lực. Root có include tùy chọn; fragment chỉ có proxies. Giữ tương thích config một file; không thêm include vào engine hoặc runtime snapshot.
2. Expand file/glob theo mục 7.1: đường dẫn tương đối theo root, thứ tự ổn định, không đệ quy, lỗi khi thiếu/không match/thư mục hoặc lặp cùng file sau resolve symlink.
3. Giữ source file và field path trước khi ghép. Từ chối proxy ID trùng toàn tập và rule ID trùng trong proxy; diagnostics trỏ cả hai khai báo. Không overwrite/merge theo ID.
4. Resolve TLS theo file chứa proxy, validate toàn bộ một lần. Source/include metadata không làm đổi fingerprint; giữ hash TLS và các field cần restart.
5. Thêm đường nạp lại từ root filename vào control service. Chốt lại vai trò `Load`, `Parse` và `Reload` để caller không truyền bytes gốc rồi vô tình bỏ qua fragments. Parse bytes độc lập không được âm thầm bỏ include.
6. Thêm ví dụ multi-file và fixtures khi loader đã chạy; giữ ví dụ một file hiện tại. Cập nhật API/docs và unit tests theo hợp đồng mới.

## VERIFY

- MC1: inline, include-only và kết hợp inline/include; nhiều glob có thứ tự ổn định; config một file tiếp tục pass. Chia lại file tương đương không đổi fingerprint.
- MC2: ID trùng trong root/fragment và giữa fragments; duplicate rule scope; file thiếu, sai type, unknown field, glob sai/rỗng, include lồng nhau, self-include/symlink lặp. Lỗi có source/field và không leak secrets.
- MC3: cert/key/CA tương đối theo từng fragment, chạy từ working directory khác; di chuyển file làm thay TLS path thực phải cần restart.
- MC4: reload một fragment lỗi giữ snapshot/state/counters cũ; đổi rules tạo revision/reset; request giữ snapshot cũ; no-op giữ counters. Thêm/xóa proxy bị từ chối khi reload.
- Chạy unit/race tests, build và vet; test filesystem dùng thư mục tạm. Kiểm chứng CLI/Docker tiếp tục ở P04/P11/P15.

## REVIEW

Không nhân đôi engine/validation, không lưu source path vào effective-config hash, không hứa atomic filesystem nhiều file. Chỉ Done sau VERIFY/REVIEW có bằng chứng và cập nhật changelog.
