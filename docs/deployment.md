# Triển khai trên server

Faultline là proxy để kiểm thử failure handling, phù hợp nhất cho development,
staging và môi trường test được kiểm soát. Không dùng nó làm Internet edge proxy
hay load balancer production tổng quát. Hướng dẫn này dùng Linux có `systemd` và
cài binary tại `/usr/local/bin/faultline`.

Chọn một trong hai mô hình:

| Mô hình | Dùng khi | Kênh quản trị |
| --- | --- | --- |
| File mode | Operator quản lý rule qua CLI trên server/SSH | Unix socket cục bộ |
| Managed mode | Tester cần UI/API HTTPS và account riêng | HTTPS UI/API; socket chỉ đọc `status` |

Xem [tham chiếu CLI](cli-reference.md) để biết đầy đủ lệnh và flag; xem
[cấu hình](configuration.md) trước khi triển khai.

## Chuẩn bị

Tạo service account không có login, cùng thư mục config, state và runtime. Các
đường dẫn dưới đây là ví dụ; giữ private key, managed state và config có test
header/body ở nơi chỉ operator/service account đọc được.

```sh
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin faultline
sudo install -d -o root -g faultline -m 0750 /etc/faultline
sudo install -d -o faultline -g faultline -m 0700 /var/lib/faultline
sudo install -m 0755 ./bin/faultline /usr/local/bin/faultline
sudo install -o root -g faultline -m 0640 ./faultline.yaml /etc/faultline/config.yaml
```

Nếu config tham chiếu certificate/key/CA hoặc file `include`, cài toàn bộ cây
file với đường dẫn có thể đọc/traverse bởi user `faultline`. Private key không
nên world-readable. Validate với đúng service account trước khi tạo service:

```sh
sudo -u faultline /usr/local/bin/faultline validate \
  --config /etc/faultline/config.yaml
```

Ví dụ dưới dùng `/run/faultline/admin.sock`. `RuntimeDirectory=faultline` do
systemd tạo `/run/faultline` mỗi lần service start với ownership của service;
socket là kênh quản trị cục bộ, không chứa config hay traffic log.

## File mode với systemd

Tạo `/etc/systemd/system/faultline.service`:

```ini
[Unit]
Description=Faultline failure-testing proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=faultline
Group=faultline
RuntimeDirectory=faultline
RuntimeDirectoryMode=0700
ExecStart=/usr/local/bin/faultline serve \
  --config /etc/faultline/config.yaml \
  --admin-socket /run/faultline/admin.sock
Restart=on-failure
RestartSec=2s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

Nạp unit và khởi động:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now faultline
sudo systemctl status faultline
```

Injection mặc định tắt sau mỗi start. Chỉ bật sau khi upstream và ứng dụng test
đã sẵn sàng:

```sh
sudo -u faultline /usr/local/bin/faultline status \
  --admin-socket /run/faultline/admin.sock
sudo -u faultline /usr/local/bin/faultline enable \
  --admin-socket /run/faultline/admin.sock
```

Các lệnh `status`, `enable`, `disable` và `reload` phải chạy dưới user
`faultline` (hoặc quyền tương đương) vì socket có mode `0600`. Từ máy khác, SSH
vào server để chạy các lệnh này; không expose Unix socket qua TCP.

```sh
ssh operator@faultline-host \
  'sudo -u faultline /usr/local/bin/faultline status --admin-socket /run/faultline/admin.sock'
```

### Cập nhật rule trong file mode

Sửa toàn bộ config/include, validate rồi reload. Chỉ rule và seed có thể reload;
đổi listener, protocol, upstream, TLS hoặc runtime cần restart service.

```sh
sudo -u faultline /usr/local/bin/faultline validate \
  --config /etc/faultline/config.yaml
sudo -u faultline /usr/local/bin/faultline reload \
  --config /etc/faultline/config.yaml \
  --admin-socket /run/faultline/admin.sock
```

Không retry mù nếu `reload` timeout: đọc `status` trước vì mutation có thể đã áp
dụng. Khi cần restart, dùng `sudo systemctl restart faultline`; HTTP flow được
drain tối đa 5 giây, còn PostgreSQL/MySQL session đang mở sẽ đóng ngay.

## Managed mode với UI/API HTTPS

Managed mode cung cấp UI/API cho tester. Tạo certificate và key hợp lệ với
hostname mà tester sử dụng; mở read permission cho group `faultline` nhưng giữ
private key không world-readable. Ví dụ bind UI vào loopback để một reverse
proxy/VPN ở cùng host kiểm soát truy cập:

```ini
[Unit]
Description=Faultline managed failure-testing proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=faultline
Group=faultline
RuntimeDirectory=faultline
RuntimeDirectoryMode=0700
StateDirectory=faultline
StateDirectoryMode=0700
ExecStart=/usr/local/bin/faultline serve \
  --config /etc/faultline/config.yaml \
  --data-dir /var/lib/faultline \
  --admin-socket /run/faultline/admin.sock \
  --api-listen 127.0.0.1:8443 \
  --api-cert /etc/faultline/admin.crt \
  --api-key /etc/faultline/admin.key
Restart=on-failure
RestartSec=2s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

Lưu unit này thay cho unit file-mode, rồi chạy `daemon-reload` và `enable --now`
như phần trước. Trong ví dụ, tester không truy cập thẳng IP/port 8443; reverse
proxy hoặc VPN là lớp kiểm soát network phía trước. Nếu bind trực tiếp private
IP hay `0.0.0.0`, chỉ cho phép mạng tester qua firewall/VPN và dùng certificate
có hostname đúng. Không đặt UI/API Internet-public.

Tạo account trước hoặc sau khi service chạy. Password đi qua stdin, không đặt
trong command line/history:

```sh
read -r -s -p 'Password: ' faultline_password; printf '\n' >&2
printf '%s' "$faultline_password" | sudo -u faultline /usr/local/bin/faultline user \
  --data-dir /var/lib/faultline --name tester-a --role editor
unset faultline_password
```

Tester mở `https://<hostname>:8443`, đăng nhập bằng account `viewer` hoặc
`editor`. Editor chỉnh/apply rule và bật/tắt injection qua UI/API. Trong managed
mode, Unix socket chỉ cho `status`; `enable`, `disable` và `reload` qua socket
bị từ chối để không có hai kênh mutation cạnh tranh nhau.

UI chỉ sửa rule của proxy đã tồn tại. Để thêm/xóa proxy hoặc đổi hạ tầng, dừng
service, lấy config active làm bản gốc, thay đổi complete configuration, chạy
`configure`, rồi start lại. Các lệnh dưới dùng `jq` để đọc trường config từ
managed state:

```sh
sudo systemctl stop faultline
sudo -u faultline sh -c \
  'jq -r ".config" /var/lib/faultline/state.json > /tmp/faultline-next.yaml'
# Sửa /tmp/faultline-next.yaml: thêm/xóa proxy hoặc đổi listener/upstream/TLS/runtime.
sudo -u faultline /usr/local/bin/faultline validate \
  --config /tmp/faultline-next.yaml
sudo -u faultline /usr/local/bin/faultline configure \
  --data-dir /var/lib/faultline --config /tmp/faultline-next.yaml
sudo systemctl start faultline
```

Không dùng file bootstrap cũ làm replacement config vì nó có thể bỏ các rule
tester đã apply. `configure` thay toàn bộ config, tăng revision khi có thay đổi
và start lại với injection tắt. Xem [Vận hành CLI](operations.md) để biết giới
hạn mutation trong managed mode.

## Network và TLS

- Chỉ mở port data-plane (`listen` trong YAML) cho đúng mạng cần test.
- Admin Unix socket không cần và không được publish qua network.
- UI/API managed mode cần HTTPS trực tiếp tới Faultline; nếu có reverse proxy,
  proxy phải kết nối HTTPS tới Faultline và giữ Host/Origin phù hợp.
- TLS proxy data-plane và TLS UI/API là các certificate/leg riêng. Thay TLS
  data-plane cần restart; không có certificate hot reload.
- Listener ready chỉ có nghĩa port đã bind, không chứng minh upstream hoặc ứng
  dụng đã ready. Kiểm tra health/readiness thực tế trước khi enable injection.

## Log, quan sát và retention

`serve` ghi event NDJSON ra stdout và diagnostics ra stderr. Trong unit ở trên,
cả hai được journald thu nhận; systemd/journald tự quản lý journal file và
rotation, không dùng `nohup` hoặc `copytruncate` cho event stream quan trọng.

```sh
sudo journalctl -u faultline -f
sudo journalctl -u faultline --since '1 hour ago'
sudo systemctl status faultline
```

Chọn retention/capacity cho journald theo chính sách máy chủ, ví dụ trong
`/etc/systemd/journald.conf`:

```ini
[Journal]
Storage=persistent
SystemMaxUse=5G
SystemMaxFileSize=256M
MaxRetentionSec=30day
```

Sau khi thay đổi journald config, reload/restart dịch vụ journald theo quy trình
vận hành của hệ thống. Theo dõi `dropped_events`, `write_errors` và
`pending_events` trong `status`: giá trị khác 0 nghĩa event artifact có thể
không đầy đủ. Journal không thay thế backup nếu event là bằng chứng test cần
lưu trữ lâu dài.

## Backup và khôi phục managed state

Chỉ managed mode dùng `/var/lib/faultline`. Nó chứa `state.json` (config,
revision và audit) và `users.json` (hash password và role). Backup thư mục này
như dữ liệu nhạy cảm, với quyền hạn chế; không coi nó là traffic/event history.

Trước thay đổi hạ tầng, backup khi service đã dừng để có snapshot nhất quán:

```sh
sudo systemctl stop faultline
sudo tar -C /var/lib -czf /var/backups/faultline-$(date +%F).tgz faultline
```

Không xóa `--data-dir` để xử lý socket stale. Sau crash, chỉ khi đã xác nhận
process cũ không còn chạy mới xóa riêng `/run/faultline/admin.sock`; thư mục
`/run` thường là tmpfs và được tạo mới sau reboot.

## Docker

Để chạy container, mount read-only toàn bộ config/TLS tree, publish riêng port
data-plane cần thiết và chạy admin command bằng `docker exec`; không publish
admin TCP port vì không có. Image mặc định chạy non-root và `/tmp` writable chỉ
phục vụ runtime socket. Xem [hướng dẫn Docker](../deploy/docker/README.md) cho
mount, UID, shutdown, reload và troubleshooting.

## Checklist trước khi đưa tester vào dùng

- [ ] `faultline validate` chạy thành công dưới service account.
- [ ] Port listener và upstream đã kiểm tra từ đúng network namespace/host.
- [ ] Injection đang tắt sau start; chỉ bật sau readiness check.
- [ ] Admin socket nằm trong private runtime directory, không expose TCP.
- [ ] UI/API (nếu có) dùng certificate hợp lệ và chỉ truy cập qua mạng tester
      được giới hạn.
- [ ] `journalctl` nhận event/diagnostic, retention đã đặt theo dung lượng disk.
- [ ] Có alert/quy trình kiểm tra counters `dropped_events`, `write_errors`,
      `pending_events`.
- [ ] Managed state đã có backup và quyền filesystem phù hợp.
