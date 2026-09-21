package mysql

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestPacketBoundsAndFragmentation(t *testing.T) {
	var b bytes.Buffer
	if err := writePacket(&b, packet{255, []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	p, err := readPacket(fragmented{&b})
	if err != nil || p.seq != 255 || string(p.body) != "hello" {
		t.Fatal(p, err)
	}
	for _, bad := range [][]byte{{0, 0, 0, 0}, {1, 0, 16, 0}, {255, 255, 255, 0}, {4, 0, 0, 0, 1}} {
		if _, err := readPacket(bytes.NewReader(bad)); err == nil {
			t.Fatal("accepted malformed packet")
		}
	}
}
func TestCommitGrammar(t *testing.T) {
	for _, q := range []string{"COMMIT", " commit ; ", "CoMmIt WORK;"} {
		if classify([]byte(q)) != commit {
			t.Fatal(q)
		}
	}
	for _, q := range []string{"SELECT 'COMMIT'", "/*COMMIT*/ SELECT 1", "COMMIT; SELECT 1", "COMMIT AND CHAIN", "COMMIT /*comment*/", "COMMIT;;"} {
		if classify([]byte(q)) == commit {
			t.Fatal(q)
		}
	}
	for _, q := range []string{"BEGIN", "START TRANSACTION", "begin work;"} {
		if classify([]byte(q)) != begin {
			t.Fatal(q)
		}
	}
}
func TestStatusPackets(t *testing.T) {
	for _, status := range []uint16{0, 1, 2, 3} {
		b := []byte{0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(b[3:], status)
		if got, ok := okStatus(b); !ok || got != status {
			t.Fatal(got, ok)
		}
	}
	for _, bad := range [][]byte{{0}, {0, 0xfc}, {0, 0xfb, 0, 0, 0, 0, 0}, {0xff, 0, 0, 0, 0, 0, 0}} {
		if _, ok := okStatus(bad); ok {
			t.Fatal("malformed OK")
		}
	}
	if _, ok := eofStatus([]byte{0, 0, 42, 0, 0, 0, 0}, true); ok {
		t.Fatal("binary row interpreted as EOF")
	}
}

type fragmented struct{ io.Reader }

func (r fragmented) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return r.Reader.Read(b)
}
