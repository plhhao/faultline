# Vận hành bằng CLI

Tất cả runtime command phải dùng cùng Unix socket với `serve`:

```bash
rtk proxy ./bin/faultline status --admin-socket /tmp/faultline/admin.sock
rtk proxy ./bin/faultline enable --admin-socket /tmp/faultline/admin.sock
rtk proxy ./bin/faultline disable --admin-socket /tmp/faultline/admin.sock
rtk proxy ./bin/faultline reload --config /absolute/path/config.yaml \
  --admin-socket /tmp/faultline/admin.sock
```

Socket mặc định là `/tmp/faultline-<uid>/admin.sock`. Sau crash, chỉ xóa socket
cũ khi chắc chắn process cũ đã dừng; không xóa data directory để xử lý socket.

## Đọc `status`

| Field | Ý nghĩa |
| --- | --- |
| `ready` | Listener proxy đã mở, không chứng minh upstream/app sẵn sàng. |
| `config_revision` | Revision cấu hình active. |
| `injection_enabled` | Injection toàn cục. |
| `revision_rule_counters` | Eligible/selected của rule ở revision active. |
| `run_counters` | Tổng lifetime process: total, selected, applied, active… |

`active_fault_flows` có thể vẫn lớn hơn 0 sau `disable`: disable chỉ ảnh hưởng
flow mới, không thả `hold_response` đã bắt đầu. Rule counters reset khi revision
mới apply; run counters chỉ reset khi process restart.

Runtime command timeout mặc định 5 giây. Nếu timeout, mutation có thể đã xảy ra;
gọi `status` trước khi retry. Nonzero `dropped_events`, `write_errors` hoặc
`pending_events` cho biết event artifact có thể không đầy đủ.

## Managed mode

Với `--data-dir`, Unix admin socket chỉ đọc `status`; thay rule và enable/disable
qua HTTPS UI/API có login.

### Tài khoản UI

Tạo tài khoản mới hoặc cập nhật password/quyền của tài khoản hiện có bằng lệnh
`user`. Password phải được pipe vào stdin; không ghi password vào command
history:

```bash
read -r -s -p "Password: " faultline_password; printf "\n" >&2
printf "%s" "$faultline_password" | rtk proxy ./bin/faultline user \
  --data-dir /path/to/data --name alice --role editor
unset faultline_password
```

Role `viewer` chỉ xem; `editor` có thể chỉnh rule, apply và enable/disable.
Chạy lại lệnh với cùng `--name` để đổi password hoặc quyền. Xóa tài khoản bằng
`rtk proxy ./bin/faultline user --data-dir /path/to/data --name alice --delete`.
Mọi session hiện có của tài khoản đó sẽ bị thu hồi.

### Thay đổi hạ tầng

Muốn đổi listener, upstream, TLS hoặc runtime, dừng instance trước rồi dùng:

```bash
rtk proxy ./bin/faultline configure \
  --data-dir /path/to/data --config /path/to/replacement.yaml
```

`configure` thay toàn bộ managed config và bị từ chối khi instance đang chạy.
