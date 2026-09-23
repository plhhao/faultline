package bullmq

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func encode(args ...string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	return b.Bytes()
}

func addArgs(queue string) []string {
	args := []string{"EVALSHA", standardSHA, "9"}
	for _, suffix := range []string{"wait", "paused", "meta", "id", "completed", "delayed", "active", "events", "marker"} {
		args = append(args, "bull:"+queue+":"+suffix)
	}
	return append(args, "opaque-packed-args", "secret-payload", "opaque-options")
}

func TestRESPBoundsAndRoundTrip(t *testing.T) {
	for _, wire := range []string{"+OK\r\n", ":-5\r\n", "$-1\r\n", "*-1\r\n", "*2\r\n$3\r\nfoo\r\n*1\r\n:7\r\n", string(encode("AUTH", "secret"))} {
		f, err := readFrame(bufio.NewReader(strings.NewReader(wire)))
		if err != nil || string(f.wire) != wire {
			t.Fatalf("round trip %q: %v", wire, err)
		}
	}
	for _, wire := range []string{"+bad\n", "$-2\r\n", "$1048576\r\n", "*4097\r\n", strings.Repeat("*1\r\n", 10) + "+ok\r\n", ":overflow9999999999999999999999\r\n", "$2\r\nx\r\n", ">1\r\n+push\r\n", "+" + strings.Repeat("x", maxFrame), "*4096\r\n" + strings.Repeat("*2\r\n:1\r\n:1\r\n", 4096)} {
		if _, err := readFrame(bufio.NewReader(strings.NewReader(wire))); err == nil {
			t.Fatalf("accepted malformed/oversized frame (%d bytes)", len(wire))
		}
	}
}

func TestClassificationRequiresPinnedScriptAndAllKeys(t *testing.T) {
	args := addArgs("orders")
	if queueName(args) != "orders" {
		t.Fatal("pinned command not recognized")
	}
	for _, index := range []int{0, 1, 2, 3, 4, 11} {
		copy := append([]string(nil), args...)
		copy[index] = "other"
		if queueName(copy) != "" {
			t.Fatalf("recognized mismatch at %d", index)
		}
	}
	if queueName(addArgs("bad:queue")) != "" {
		t.Fatal("ambiguous queue recognized")
	}
	for _, name := range []string{"MULTI", "HELLO", "SUBSCRIBE", "MONITOR", "SELECT", "PSYNC"} {
		f, _ := readFrame(bufio.NewReader(bytes.NewReader(encode(name))))
		if _, err := command(f); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	f, _ := readFrame(bufio.NewReader(bytes.NewReader(encode("CLIENT", "REPLY", "OFF"))))
	if _, err := command(f); err == nil {
		t.Fatal("CLIENT REPLY accepted")
	}
}

func FuzzRESP(f *testing.F) {
	f.Add([]byte("*1\r\n$4\r\nPING\r\n"))
	f.Add([]byte("$-1\r\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > maxFrame+1 {
			return
		}
		_, _ = readFrame(bufio.NewReader(bytes.NewReader(b)))
	})
}
