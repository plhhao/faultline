# Tham chiếu CLI

Lệnh gốc là `faultline`. Mọi tham số đều phải là flag; CLI không nhận positional
argument. Dùng `faultline --help` để xem danh sách lệnh, hoặc
`faultline <lệnh> --help` để xem flag của một lệnh trên binary đang chạy.

```text
faultline <validate|serve|reload|enable|disable|status|user|configure> [flags]
```

`--help` hoặc `-h` in trợ giúp và thoát thành công. Lỗi cú pháp, flag không hợp
lệ hoặc thiếu flag bắt buộc thoát với mã 1. Các ví dụ dưới đây dùng binary đã
build tại `./bin/faultline`; thay đường dẫn này bằng binary đã cài đặt nếu cần.

## Flag dùng chung

| Flag | Kiểu | Mặc định | Dùng bởi | Chức năng và ràng buộc |
| --- | --- | --- | --- | --- |
| `--config FILE` | đường dẫn | không có | `validate`, `serve`, `reload`, `configure` | Bắt buộc. Là file YAML gốc; có thể dùng `include`. `reload` chuyển nó thành đường dẫn tuyệt đối trước khi gửi tới process đang phục vụ. |
| `--admin-socket PATH` | đường dẫn Unix socket | `/tmp/faultline-<uid>/admin.sock` | `serve`, `status`, `enable`, `disable`, `reload` | Xác định instance file-mode. Dùng cùng giá trị cho `serve` và các runtime command. Thư mục cha phải private `0700`; socket tạo với mode `0600` và socket đã tồn tại không bị ghi đè. |
| `--timeout DURATION` | Go duration, ví dụ `500ms`, `5s`, `1m` | `5s` | `status`, `enable`, `disable`, `reload` | Thời gian chờ admin command; phải lớn hơn 0. Timeout của mutation không khẳng định mutation chưa xảy ra: chạy `status` trước khi retry. |

## `validate`

```sh
rtk proxy ./bin/faultline validate --config FILE
```

Đọc và kiểm tra toàn bộ YAML/config include và các file TLS được tham chiếu. Nó
không mở listener, không gọi upstream và không tạo admin socket. Khi hợp lệ,
in `Configuration valid` ra stdout.

| Flag | Bắt buộc | Chức năng |
| --- | --- | --- |
| `--config FILE` | Có | Root configuration cần validate. |

## `serve`

```sh
rtk proxy ./bin/faultline serve --config FILE [flags]
```

Nạp config, mở listener proxy, mở Unix admin socket và ghi JSON event theo từng
dòng ra stdout. Readiness, lỗi và chẩn đoán đi ra stderr. Injection mặc định
tắt; Ctrl+C hoặc SIGTERM dừng nhận flow mới, drain HTTP tối đa 5 giây rồi hủy
flow còn lại. PostgreSQL/MySQL session đóng ngay khi shutdown.

| Flag | Kiểu | Mặc định | Chức năng và ràng buộc |
| --- | --- | --- | --- |
| `--config FILE` | đường dẫn | không có | Bắt buộc; root YAML configuration. |
| `--admin-socket PATH` | đường dẫn Unix socket | `/tmp/faultline-<uid>/admin.sock` | Socket private cho runtime command. |
| `--start-enabled` | boolean | `false` | Bật injection ngay khi listener sẵn sàng. Nếu bỏ qua, traffic ban đầu pass-through. |
| `--event-buffer N` | số nguyên | `1024` | Số event JSON chờ tối đa, chưa kể event đang được writer xử lý. Phải lớn hơn 0; đầy queue sẽ drop event mới và tăng `dropped_events`. |
| `--data-dir DIR` | đường dẫn | rỗng | Bật managed mode và lưu config/account/audit bền vững trong thư mục private `0700`. Bỏ flag này để dùng file mode. |
| `--api-listen HOST:PORT` | địa chỉ | `127.0.0.1:8443` | Listener HTTPS của managed UI/API. Chỉ dùng khi có `--data-dir`; phải không rỗng trong managed mode. |
| `--api-cert PEM` | đường dẫn | rỗng | Certificate PEM của HTTPS UI/API. Bắt buộc khi dùng `--data-dir`. |
| `--api-key PEM` | đường dẫn | rỗng | Private-key PEM của HTTPS UI/API. Bắt buộc khi dùng `--data-dir`. |

Trong managed mode, config file chỉ bootstrap lần đầu. Các lần khởi động sau nạp
revision đã lưu trong `--data-dir`; Unix socket chỉ cho phép `status`, còn sửa
rule và bật/tắt injection thực hiện qua HTTPS UI/API. `--api-listen`,
`--api-cert` và `--api-key` phải được cung cấp/resolve hợp lệ cùng `--data-dir`.

## `status`

```sh
rtk proxy ./bin/faultline status [--admin-socket PATH] [--timeout DURATION]
```

Đọc trạng thái instance và in JSON: readiness listener, revision, injection
state, counters rule của revision hiện hành, recorder counters và active fault
flows. Không thay đổi trạng thái.

| Flag | Mặc định | Chức năng |
| --- | --- | --- |
| `--admin-socket PATH` | socket mặc định | Instance cần đọc trạng thái. |
| `--timeout DURATION` | `5s` | Thời gian chờ request admin, phải dương. |

## `enable` và `disable`

```sh
rtk proxy ./bin/faultline enable [--admin-socket PATH] [--timeout DURATION]
rtk proxy ./bin/faultline disable [--admin-socket PATH] [--timeout DURATION]
```

`enable` bật injection toàn cục cho flow mới; `disable` tắt nó cho flow mới.
Cả hai in JSON result. Chúng idempotent: lặp lại trạng thái đang có là no-op.
`disable` không kết thúc delay/hold đã bắt đầu. Trong managed mode, hai lệnh bị
từ chối trên Unix socket; editor phải dùng HTTPS UI/API.

| Flag | Mặc định | Chức năng |
| --- | --- | --- |
| `--admin-socket PATH` | socket mặc định | Instance cần thay đổi. |
| `--timeout DURATION` | `5s` | Thời gian chờ admin mutation, phải dương. |

## `reload`

```sh
rtk proxy ./bin/faultline reload --config FILE \
  [--admin-socket PATH] [--timeout DURATION]
```

Yêu cầu process đang phục vụ đọc lại root config và mọi file include từ chính
filesystem của nó. Root path được CLI chuyển thành tuyệt đối. Kiểm tra thất bại
giữ nguyên snapshot cũ; reload thành công publish snapshot mới atomically.
Chỉ rule và seed được phép đổi; listener, protocol, upstream, TLS và runtime
cần restart. Trong managed mode, Unix socket chỉ đọc trạng thái nên `reload`
bị từ chối; dùng UI/API cho thay đổi rule hoặc `configure` khi instance đã dừng.

| Flag | Mặc định | Chức năng |
| --- | --- | --- |
| `--config FILE` | không có | Bắt buộc; root config cần reload. |
| `--admin-socket PATH` | socket mặc định | Instance cần reload. |
| `--timeout DURATION` | `5s` | Thời gian chờ admin mutation, phải dương. |

## `user`

```sh
printf '%s' "$faultline_password" | rtk proxy ./bin/faultline user \
  --data-dir DIR --name NAME [--role viewer|editor]

rtk proxy ./bin/faultline user --data-dir DIR --name NAME --delete
```

Tạo hoặc cập nhật account managed mode. Lệnh đọc password từ stdin khi không có
`--delete`; terminal stdin trực tiếp bị từ chối để tránh nhập password không an
toàn. Mọi session hiện có của account được thu hồi sau khi account được đổi hoặc
xóa.

| Flag | Kiểu | Mặc định | Chức năng và ràng buộc |
| --- | --- | --- | --- |
| `--data-dir DIR` | đường dẫn | không có | Bắt buộc; managed data directory private. |
| `--name NAME` | string | không có | Bắt buộc; 1–64 ký tự chữ, số, `.`, `_` hoặc `-`. |
| `--role viewer\|editor` | string | `viewer` | Quyền account. `viewer` chỉ xem; `editor` có thể sửa/apply rule và bật/tắt injection. Chỉ kiểm tra khi tạo/cập nhật. |
| `--delete` | boolean | `false` | Xóa account và không đọc stdin. `--role` bị bỏ qua. |

Password tạo/cập nhật phải dài 12–1024 byte; trailing newline từ pipe được bỏ.

## `configure`

```sh
rtk proxy ./bin/faultline configure --data-dir DIR --config FILE
```

Thay toàn bộ managed configuration, dùng để đổi hạ tầng như listener, upstream,
TLS hoặc runtime. Instance sở hữu `--data-dir` phải dừng trước; khi nó đang chạy,
lock dữ liệu khiến lệnh bị từ chối. Config tương đương là no-op; config mới tăng
revision và lần `serve` kế tiếp khởi động với injection tắt.

| Flag | Kiểu | Mặc định | Chức năng |
| --- | --- | --- | --- |
| `--data-dir DIR` | đường dẫn | không có | Bắt buộc; managed data directory private. |
| `--config FILE` | đường dẫn | không có | Bắt buộc; complete replacement configuration. |

## Chọn lệnh

| Nhu cầu | Lệnh |
| --- | --- |
| Kiểm YAML trước khi chạy | `validate` |
| Khởi động proxy file mode hoặc managed mode | `serve` |
| Xem state/counters | `status` |
| Bật hoặc tắt fault cho flow mới ở file mode | `enable` / `disable` |
| Thay rule/seed ở file mode khi process đang chạy | `reload` |
| Tạo, đổi quyền/password hoặc xóa account managed mode | `user` |
| Thay hạ tầng managed mode khi instance đã dừng | `configure` |
