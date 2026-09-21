package rabbitmq

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	maxFrame       = 1 << 20
	frameEnd       = 0xce
	frameMethod    = 1
	frameHeader    = 2
	frameBody      = 3
	frameHeartbeat = 8
)

var errProtocol = errors.New("rabbitmq: unsupported or malformed AMQP 0-9-1 protocol")

type frame struct {
	kind    byte
	channel uint16
	body    []byte
}

func readFrame(r io.Reader) (frame, error) {
	var h [7]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(h[3:])
	if n > maxFrame {
		return frame{}, errProtocol
	}
	f := frame{kind: h[0], channel: binary.BigEndian.Uint16(h[1:3]), body: make([]byte, n)}
	if _, err := io.ReadFull(r, f.body); err != nil {
		return frame{}, err
	}
	var end [1]byte
	if _, err := io.ReadFull(r, end[:]); err != nil || end[0] != frameEnd {
		return frame{}, errProtocol
	}
	if f.kind != frameMethod && f.kind != frameHeader && f.kind != frameBody && f.kind != frameHeartbeat {
		return frame{}, errProtocol
	}
	if f.kind == frameHeartbeat && (f.channel != 0 || len(f.body) != 0) {
		return frame{}, errProtocol
	}
	return f, nil
}

func writeFrame(w io.Writer, f frame) error {
	if len(f.body) > maxFrame {
		return errProtocol
	}
	b := make([]byte, 7+len(f.body)+1)
	b[0] = f.kind
	binary.BigEndian.PutUint16(b[1:3], f.channel)
	binary.BigEndian.PutUint32(b[3:7], uint32(len(f.body)))
	copy(b[7:], f.body)
	b[len(b)-1] = frameEnd
	return writeAll(w, b)
}

func writeAll(w io.Writer, b []byte) error {
	for len(b) != 0 {
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

func method(f frame, class, name uint16) bool {
	return f.kind == frameMethod && len(f.body) >= 4 && binary.BigEndian.Uint16(f.body) == class && binary.BigEndian.Uint16(f.body[2:]) == name
}

func shortString(b []byte) (string, []byte, bool) {
	if len(b) == 0 || int(b[0]) > len(b)-1 {
		return "", nil, false
	}
	n := int(b[0])
	return string(b[1 : 1+n]), b[1+n:], true
}

func publishMetadata(f frame) (string, string, bool) {
	if !method(f, 60, 40) || len(f.body) < 6 {
		return "", "", false
	}
	exchange, rest, ok := shortString(f.body[6:])
	if !ok {
		return "", "", false
	}
	routingKey, rest, ok := shortString(rest)
	if !ok || len(rest) != 1 {
		return "", "", false
	}
	return exchange, routingKey, true
}

func contentSize(f frame) (uint64, bool) {
	if f.kind != frameHeader || len(f.body) < 12 || binary.BigEndian.Uint16(f.body) != 60 || binary.BigEndian.Uint16(f.body[2:4]) != 0 {
		return 0, false
	}
	return binary.BigEndian.Uint64(f.body[4:12]), true
}

func returnMetadata(f frame) (string, string, bool) {
	if !method(f, 60, 50) || len(f.body) < 6 {
		return "", "", false
	}
	_, rest, ok := shortString(f.body[6:])
	if !ok {
		return "", "", false
	}
	exchange, rest, ok := shortString(rest)
	if !ok {
		return "", "", false
	}
	routingKey, _, ok := shortString(rest)
	return exchange, routingKey, ok
}

func confirm(f frame) (tag uint64, multiple bool, ok bool) {
	if !method(f, 60, 80) || len(f.body) != 13 {
		return 0, false, false
	}
	return binary.BigEndian.Uint64(f.body[4:12]), f.body[12]&1 != 0, true
}

func negativeConfirm(f frame) (tag uint64, multiple bool, ok bool) {
	if !method(f, 60, 120) || len(f.body) != 13 {
		return 0, false, false
	}
	return binary.BigEndian.Uint64(f.body[4:12]), f.body[12]&1 != 0, true
}
