# P12 — JSON events và counters có giới hạn

- Trạng thái: **Done** (2026-09-13)
- Phụ thuộc: [P10](../04-runtime-control/01-local-admin.md), [P11](../04-runtime-control/02-atomic-reload.md)
- Nguồn: [specific.md](../../specific.md), mục 6.3, 10, 11.
- Nghiệm thu MVP liên quan: AC13, AC15, AC16, AC18
- Vùng thay đổi dự kiến: `internal/recorder/, internal/control/, internal/proxy/http/`

## DEFINE

Ghi bằng chứng từ decision đến outcome, phân biệt lỗi inject với lỗi proxy/upstream.

## PLAN → BUILD

1. Xuất JSON stdout gồm run/flow/proxy/protocol/revision/state/control sequence, thời gian, rule/eligible sequence/selector, phase/action/outcome.
2. Counters riêng total/eligible/selected/applied/active/dropped; counter chính xác không phụ thuộc event còn trong queue.
3. Queue bounded, đầy thì bỏ event/tăng dropped thay vì block vô hạn; status/control events và shutdown có cách kết thúc bounded.
4. Giới hạn retention revision và cardinality, không dùng flow ID/header làm metric label; mặc định không log body/Authorization/toàn bộ headers.

Quyết định: recorder nhận lifecycle events đồng bộ, cập nhật counters trước khi
enqueue không chặn. Queue mặc định 1024 (`--event-buffer`); một writer xuất JSON.
Counters recorder tích lũy theo run, engine counters theo revision hiện tại;
không giữ map revision/flow đã xong. Shutdown flush tối đa 2s; sink io.Writer bị
kẹt có thể giữ một writer goroutine đến khi process thoát, không chặn shutdown.
Event chỉ có IDs do hệ thống/config đặt, selector số, timestamps và error kind;
không serialize raw error, URL, headers, request/response body hoặc config.

## VERIFY

- Dựng selected→not_reached và selected→applied; TLS/upstream/cancel/timeout/overload không bị gán thành injected fault.
- Làm đầy queue bằng sink chậm, xác nhận dropped tăng mà proxy vẫn tiến triển; kiểm tra field bắt buộc và dữ liệu nhạy cảm.
- Flow cũ sau reload ghi đúng revision, status còn active faults sau disable; chạy -race.

## REVIEW

Không suy luận retry, business commit, circuit breaker hoặc PASS/FAIL chỉ từ traffic.

## Kết quả VERIFY/REVIEW

- Unit/integration/process tests pass với `-race`: queue đầy/sink chậm không chặn proxy/admin; counters total/eligible/selected/applied/active vẫn chính xác, dropped tăng.
- Builtin báo action bắt đầu trước khi delay/hold kết thúc qua ApplicationObserver; adapter chống đếm applied hai lần. Status vẫn thấy active fault của snapshot cũ sau disable/reload.
- Flow events có identity/revision/control sequence, timestamps, selector, phase và action. Finish tách selected/not_reached, applied, upstream/TLS, proxy overload/timeout, client cancellation/shutdown; không serialize raw error, URL, header, body.
- Redaction tests dùng request body/Authorization/query/path và configured response body; các giá trị không xuất hiện trong JSON. SIGPIPE test xác nhận closed stdout chỉ thành sink error, không thoát server.
- Cleanup xong mới queue shutdown summary; flush tối đa 2s, stderr báo dropped/write_errors/pending hoặc flush error. Unit test sink block xác nhận Close có deadline; generic writer có thể giữ tối đa một goroutine đến process exit.
- REVIEW: không giữ map historical revisions/completed flows; recorder totals theo run, rule counters theo revision. Không suy luận business commit/retry/PASS từ traffic. Full suite, vet/build và kiểm tra tài liệu được ghi tại phase README.
