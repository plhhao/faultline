package tcp

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestWaitBeforeDialBuffersClientHelloAndDetectsClose(t *testing.T) {
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	go func() { peer.Write([]byte("hello")) }()
	start := time.Now()
	buffer, err := waitBeforeDial(context.Background(), client, 50*time.Millisecond)
	if err != nil || string(buffer) != "hello" || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("buffer=%q err=%v elapsed=%v", buffer, err, time.Since(start))
	}
	peer.Close()
	start = time.Now()
	_, err = waitBeforeDial(context.Background(), client, time.Second)
	if err == nil || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("close was not detected promptly: %v", err)
	}
	client2, peer2 := net.Pipe()
	defer client2.Close()
	go func() { peer2.Write([]byte("hello")); peer2.Close() }()
	start = time.Now()
	_, err = waitBeforeDial(context.Background(), client2, time.Second)
	if err == nil || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("close after initial bytes was not detected promptly: %v", err)
	}
}
