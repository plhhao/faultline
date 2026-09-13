# Phase 04 — Điều khiển runtime và quan sát

Phạm vi: **MVP**. Trạng thái: **Done** — đã kiểm chứng trên host và container (2026-09-13).

Thao tác CLI trên process đang chạy, reload atomically, JSON events và counters.

## Các plan

| Plan | Tính năng | Phụ thuộc |
| --- | --- | --- |
| [P10](01-local-admin.md) | Kênh quản trị local và CLI runtime | P02, P09 |
| [P11](02-atomic-reload.md) | Reload qua CLI và revision nhất quán | P10 |
| [P12](03-events-and-counters.md) | JSON events và counters có giới hạn | P10, P11 |

## Điều kiện hoàn tất phase

Hoàn tất các plan trong bảng và có bằng chứng VERIFY/REVIEW cho phạm vi phase. MVP chỉ hoàn tất sau phase 05 và đủ AC1–AC24.

Xem [lộ trình và quy tắc thực hiện](../README.md).

## Kết quả hiện thực

- P10/P11: CLI `status/enable/disable/reload`, HTTP/JSON qua Unix socket riêng,
  timeout/no-retry, reload file gốc/includes trong process và trả revision thực.
- P12: JSON stdout, lifecycle/control events, counters theo run độc lập queue,
  active fault flows sau disable/reload, bounded queue/flush và redaction.
- `serve` vẫn mặc định disabled. `--admin-socket` chọn instance;
  `--event-buffer` mặc định 1024, CLI `--timeout` mặc định 5s. Hướng dẫn tại
  [README](../../README.md#runtime-administration).

## VERIFY/REVIEW trên host

- `go test -race ./... -timeout 90s` pass; test Docker opt-in được skip trong suite mặc định.
- Process test kiểm tra startup/nth:1, toggle/no-op, reload và keep-alive snapshot,
  JSON redaction, SIGTERM, socket cleanup và restart disabled/run mới.
- Test recorder/admin kiểm tra queue đầy, sink chậm/lỗi, active fault cũ, overload,
  timeout/TLS và concurrent reload/toggle/request. Timeout classification chạy lặp 10 lần pass.
- `go vet ./...`, build CLI macOS và cross-build Linux arm64/CGO=0 pass;
  binary help và validate multi-file pass. Các kiểm thử mạng dùng quyền bind localhost/Unix socket.
- Review thủ công source/diff: giữ snapshot immutable, không log raw errors/config,
  không lưu revision/flow map vô hạn; counters cập nhật trước queue. Bổ sung
  summary sau cleanup và cảnh báo stderr khi mất events. Process test stdout bị
  đóng pass sau khi `serve` ignore SIGPIPE để lỗi sink không giết proxy.

## Kiểm chứng container

`TestContainerRuntime` đã được thêm: build image scratch tạm, mount cây config và
TLS files, chạy validate/serve/status/enable/reload/disable, kiểm tra duplicate ID
giữ revision, rồi dọn image/container riêng. Chạy bằng:

```sh
rtk proxy env FAULTLINE_DOCKER_TEST=1 go test ./tests/integration -run '^TestContainerRuntime$' -timeout 180s
```

Test pass 3 lần liên tiếp trên Docker/OrbStack ngày 2026-09-13: validate cây
config/TLS mount, startup disabled và ready, enable, reload revision mới, reload
tương đương no-op, duplicate ID trả đúng source và giữ snapshot, disable và stop
với exit code 0. Image/container của test được dọn sau mỗi lần chạy.

Fixture được ghi qua file tạm rồi rename; test đợi `validate` trong container
nhận đúng nội dung mới trước bước reload duplicate ID vì bind mount qua VM có
thể tạm thời trả nội dung chưa đầy đủ. CLI không retry mutation. P10/P11/MC5 đã
có bằng chứng container; Dockerfile phát hành, demo và nghiệm thu toàn MVP vẫn
thuộc phase 5.
