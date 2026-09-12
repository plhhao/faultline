# P13 — Giới hạn tài nguyên và shutdown

- Trạng thái: **Planned**
- Phụ thuộc: [P12](../04-runtime-control/03-events-and-counters.md)
- Nguồn: [specific.md](../../specific.md), mục 11, 12.
- Nghiệm thu MVP liên quan: AC14, AC15
- Vùng thay đổi dự kiến: `internal/proxy/http/, internal/control/, internal/recorder/, tests/integration/`

## DEFINE

Hoàn thiện và kiểm chứng giới hạn đã đưa vào từ forwarding/fault lifecycle dưới tải có kiểm soát.

## PLAN → BUILD

1. Chốt defaults có tài liệu cho inflight, read headers, idle, request deadline và recorder queue; override trước startup.
2. Overload trả 503 theo thiết kế và ghi proxy overload; không tăng eligible của flow bị từ chối.
3. Shutdown ngừng nhận request, chờ có hạn, cancel phần còn lại; bao gồm hold và hijacked connections, admin/recorder.
4. Benchmark direct vs pass-through vs delay/hold, ghi máy/config/tải/limits; không đặt SLA chưa có dữ liệu.

## VERIFY

- Nhiều hold/delay, slow headers, payload lớn và client cancel: flow không vượt deadline, tài nguyên về gần baseline với dung sai có giải thích.
- Kiểm tra deadline toàn flow ngắn hơn fault duration, queue full, inflight overflow và shutdown bounded.
- Chạy go test -race ./... và go vet ./...; ghi số đo benchmark thực tế, không biến timing môi trường thành assertion quá chặt.

## REVIEW

Không hứa bypass khi process chết hoặc mô phỏng packet loss; tách overhead proxy khỏi intentional delay.

Áp dụng [điều kiện Done](../README.md#theo-dõi-thực-hiện); các kiểm tra trên là kế hoạch, chưa phải kết quả đã chạy.
