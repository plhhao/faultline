# Server deployment

Faultline is a failure-testing proxy for development, staging, and controlled
test environments. Do not use it as an Internet edge proxy or general-purpose
production load balancer. This guide assumes Linux with `systemd` and installs
the binary at `/usr/local/bin/faultline`.

| Mode | Use it when | Administration channel |
| --- | --- | --- |
| File mode | An operator manages rules through server CLI/SSH | Local Unix socket |
| Managed mode | Testers need an HTTPS UI/API and separate accounts | HTTPS UI/API; socket exposes only `status` |

Read the [CLI reference](cli-reference.md) and [configuration guide](configuration.md)
before deploying.

## Prepare the host

Create a non-login service account and private configuration, state, and runtime
directories. Treat configuration with test headers/bodies, managed state, and
private keys as sensitive.

```sh
sudo useradd --system --home /nonexistent --shell /usr/sbin/nologin faultline
sudo install -d -o root -g faultline -m 0750 /etc/faultline
sudo install -d -o faultline -g faultline -m 0700 /var/lib/faultline
sudo install -m 0755 ./bin/faultline /usr/local/bin/faultline
sudo install -o root -g faultline -m 0640 ./faultline.yaml /etc/faultline/config.yaml
sudo -u faultline /usr/local/bin/faultline validate --config /etc/faultline/config.yaml
```

Install the complete included-file and TLS tree with paths traversable by the
`faultline` user. Do not make private keys world-readable.

## File mode with systemd

Create `/etc/systemd/system/faultline.service`:

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
ExecStart=/usr/local/bin/faultline serve --config /etc/faultline/config.yaml --admin-socket /run/faultline/admin.sock
Restart=on-failure
RestartSec=2s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

`RuntimeDirectory=faultline` creates `/run/faultline` on every start. Its socket
is local administration only; it contains neither configuration nor traffic logs.

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now faultline
sudo systemctl status faultline
sudo -u faultline /usr/local/bin/faultline status --admin-socket /run/faultline/admin.sock
sudo -u faultline /usr/local/bin/faultline enable --admin-socket /run/faultline/admin.sock
```

Injection is disabled after every start. Enable it only after the application and
upstream are ready. `status`, `enable`, `disable`, and `reload` must run as the
`faultline` user (or equivalent) because the socket is `0600`. From another
machine, SSH to the host; never expose the Unix socket as TCP.

### Updating file-mode rules

Edit the complete config/include tree, validate it, then reload. Only rules and
seed reload; listener, protocol, upstream, TLS, and runtime changes require a
service restart.

```sh
sudo -u faultline /usr/local/bin/faultline validate --config /etc/faultline/config.yaml
sudo -u faultline /usr/local/bin/faultline reload --config /etc/faultline/config.yaml --admin-socket /run/faultline/admin.sock
```

Do not blindly retry a timed-out reload: read `status` first. Use
`sudo systemctl restart faultline` for infrastructure changes. HTTP drains for
up to five seconds; open PostgreSQL/MySQL sessions close immediately.

## Managed mode with HTTPS UI/API

Use a certificate valid for the tester hostname. Keep the private key
non-world-readable. Bind the UI to loopback when a same-host reverse proxy or
VPN controls access:

```ini
[Service]
User=faultline
Group=faultline
RuntimeDirectory=faultline
RuntimeDirectoryMode=0700
StateDirectory=faultline
StateDirectoryMode=0700
ExecStart=/usr/local/bin/faultline serve --config /etc/faultline/config.yaml --data-dir /var/lib/faultline --admin-socket /run/faultline/admin.sock --api-listen 127.0.0.1:8443 --api-cert /etc/faultline/admin.crt --api-key /etc/faultline/admin.key
Restart=on-failure
StandardOutput=journal
StandardError=journal
```

Use this service definition instead of the file-mode `ExecStart`. Do not expose
the UI/API to the public Internet. If binding a private address or `0.0.0.0`,
restrict access to the tester network through firewall/VPN and use a certificate
with the correct hostname.

Create accounts without putting passwords in command history:

```sh
read -r -s -p 'Password: ' faultline_password; printf '\n' >&2
printf '%s' "$faultline_password" | sudo -u faultline /usr/local/bin/faultline user --data-dir /var/lib/faultline --name tester-a --role editor
unset faultline_password
```

Editors change/apply rules and control injection through the UI/API. In managed
mode the Unix socket accepts only `status`.

To add/remove a proxy or change infrastructure, stop the service, start from the
active configuration, edit the complete replacement, validate it, run
`configure`, then start the service. Do not reuse the initial bootstrap file if
testers have applied later rules: `configure` replaces everything and starts the
next service run with injection disabled.

## Network and TLS

- Open each data-plane `listen` port only to the intended test network.
- Do not publish the admin Unix socket.
- TLS for the data plane and UI/API uses separate certificates and legs.
- Data-plane TLS changes require restart; there is no certificate hot reload.
- Listener readiness means only that a port is bound. Check real application and upstream readiness before enabling injection.

## Logs and retention

`serve` writes NDJSON events to stdout and diagnostics to stderr. With the units
above, journald collects both streams and manages journal-file rotation; avoid
`nohup` or `copytruncate` for an important event stream.

```sh
sudo journalctl -u faultline -f
sudo journalctl -u faultline --since '1 hour ago'
```

Set journald retention according to host policy, for example:

```ini
[Journal]
Storage=persistent
SystemMaxUse=5G
SystemMaxFileSize=256M
MaxRetentionSec=30day
```

Monitor `dropped_events`, `write_errors`, and `pending_events` in `status`.
Nonzero values mean the event artifact may be incomplete. Journald is not a
replacement for a backup when long-term evidence is required.

## Managed-state backup and Docker

Only managed mode uses `/var/lib/faultline`. It stores `state.json` (config,
revision, audit) and `users.json` (password hashes and roles), not traffic/event
history. Back it up as sensitive data while the service is stopped:

```sh
sudo systemctl stop faultline
sudo tar -C /var/lib -czf /var/backups/faultline-$(date +%F).tgz faultline
```

Do not delete `--data-dir` to solve a stale socket. After a crash, remove only
`/run/faultline/admin.sock` after confirming that the old process is gone.

For Docker, mount the full configuration/TLS tree read-only, publish only needed
data-plane ports, and run local admin commands through `docker exec`. The image
runs non-root. See the [Docker guide](../deploy/docker/README.md).

## Pre-tester checklist

- [ ] `faultline validate` passes as the service account.
- [ ] Listener and upstream work from the intended host/network namespace.
- [ ] Injection is disabled after start and enabled only after readiness checks.
- [ ] The admin socket is private and not exposed over TCP.
- [ ] The optional UI/API has valid TLS and restricted tester-network access.
- [ ] Journald retention fits available disk and counters are monitored.
- [ ] Managed state is backed up with appropriate filesystem permissions.
