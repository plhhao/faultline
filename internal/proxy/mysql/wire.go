package mysql

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
)

const maxPacket = 1 << 20
const (
	capSSL          uint32 = 1 << 11
	capProtocol41   uint32 = 1 << 9
	capPluginAuth   uint32 = 1 << 19
	capDeprecateEOF uint32 = 1 << 24
	// Unsupported features change framing, response boundaries, or authentication.
	forbiddenCaps uint32 = 1<<5 | 1<<7 | 1<<16 | 1<<17 | 1<<18 | 1<<25 | 1<<26 | 1<<27 | 1<<28 | 1<<29
)

var errProtocol = errors.New("mysql: unsupported or malformed protocol")

type packet struct {
	seq  byte
	body []byte
}

func readPacket(r io.Reader) (packet, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return packet{}, err
	}
	n := int(h[0]) | int(h[1])<<8 | int(h[2])<<16
	if n == 0 || n > maxPacket {
		return packet{}, errProtocol
	}
	p := packet{seq: h[3], body: make([]byte, n)}
	_, err := io.ReadFull(r, p.body)
	return p, err
}
func writePacket(w io.Writer, p packet) error {
	n := len(p.body)
	if n == 0 || n > maxPacket {
		return errProtocol
	}
	b := []byte{byte(n), byte(n >> 8), byte(n >> 16), p.seq}
	b = append(b, p.body...)
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func reject(w io.Writer, seq byte) {
	_ = writePacket(w, packet{seq, append([]byte{0xff, 0x15, 0x04, '#', 'H', 'Y', '0', '0', '0'}, []byte("Faultline: unsupported or malformed MySQL protocol")...)})
}
func lenenc(b []byte) (uint64, int, bool) {
	if len(b) == 0 {
		return 0, 0, false
	}
	n := 1
	switch b[0] {
	case 0xfb, 0xff:
		return 0, 0, false
	case 0xfc:
		n = 3
	case 0xfd:
		n = 4
	case 0xfe:
		n = 9
	default:
		return uint64(b[0]), 1, true
	}
	if len(b) < n {
		return 0, 0, false
	}
	var v uint64
	for i := 1; i < n; i++ {
		v |= uint64(b[i]) << ((i - 1) * 8)
	}
	return v, n, true
}
func okStatus(b []byte) (uint16, bool) {
	if len(b) < 7 || (b[0] != 0 && b[0] != 0xfe) {
		return 0, false
	}
	_, n, ok := lenenc(b[1:])
	if !ok {
		return 0, false
	}
	_, m, ok := lenenc(b[1+n:])
	if !ok || len(b) < 1+n+m+4 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(b[1+n+m:]), true
}
func eofStatus(b []byte, modern bool) (uint16, bool) {
	if len(b) == 0 || b[0] != 0xfe {
		return 0, false
	}
	if modern {
		return okStatus(b)
	}
	if len(b) != 5 {
		return 0, false
	}
	return binary.LittleEndian.Uint16(b[3:]), true
}

type statement byte

const (
	ordinary statement = iota
	begin
	commit
	rollback
	unsupported
)

func classify(sql []byte) statement {
	s := strings.ToUpper(strings.TrimSpace(string(sql)))
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	s = strings.Join(strings.Fields(s), " ")
	switch s {
	case "BEGIN", "BEGIN WORK", "START TRANSACTION":
		return begin
	case "COMMIT", "COMMIT WORK":
		return commit
	case "ROLLBACK", "ROLLBACK WORK":
		return rollback
	}
	for _, prefix := range []string{"CALL ", "XA ", "PREPARE ", "EXECUTE ", "LOAD DATA "} {
		if strings.HasPrefix(s, prefix) {
			return unsupported
		}
	}
	return ordinary
}
