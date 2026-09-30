package tcp

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/config"
)

func TestThrottlePacesForwardedBytes(t *testing.T) {
	src, writer := net.Pipe()
	dst, reader := net.Pipe()
	defer src.Close()
	defer writer.Close()
	defer dst.Close()
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rate := 1000
	f := newFault(ctx, config.Fault{Action: "throttle", Direction: "client_to_upstream", BytesPerSecond: &rate}, func() {}, func() {})
	f.start()
	defer f.stop()
	done := make(chan struct{})
	var count atomic.Int64
	go func() { defer close(done); pump(ctx, src, dst, "client_to_upstream", f, &count) }()
	start := time.Now()
	written := make(chan error, 1)
	go func() { _, err := writer.Write(make([]byte, 1000)); written <- err }()
	buffer := make([]byte, 1000)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("1000 bytes at 1000 B/s took %v", elapsed)
	}
	deadline := time.Now().Add(time.Second)
	for count.Load() != 1000 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if count.Load() != 1000 {
		t.Fatalf("forwarded %d bytes", count.Load())
	}
	cancel()
	src.Close()
	dst.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pump did not stop")
	}
}
