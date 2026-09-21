package postgresql

import (
	"encoding/binary"
	"errors"
	"io"
)

const maxFrame = 1 << 20
const maxStartup = 10 << 10
const sslRequest = 80877103
const cancelRequest = 80877102

var errProtocol = errors.New("unsupported or malformed PostgreSQL protocol")

type frame struct {
	kind byte
	body []byte
}

func readPacket(r io.Reader, limit int) ([]byte, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(h[:])
	if n < 4 || n > uint32(limit) {
		return nil, errProtocol
	}
	b := make([]byte, int(n)-4)
	_, err := io.ReadFull(r, b)
	return b, err
}
func readFrame(r io.Reader) (frame, error) {
	var h [1]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return frame{}, err
	}
	b, err := readPacket(r, maxFrame)
	return frame{h[0], b}, err
}
func writePacket(w io.Writer, b []byte) error {
	h := binary.BigEndian.AppendUint32(nil, uint32(len(b)+4))
	h = append(h, b...)
	return writeAll(w, h)
}
func writeFrame(w io.Writer, f frame) error {
	b := []byte{f.kind}
	b = binary.BigEndian.AppendUint32(b, uint32(len(f.body)+4))
	b = append(b, f.body...)
	return writeAll(w, b)
}
func writeAll(w io.Writer, b []byte) error {
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
func reject(w io.Writer) {
	_ = writeFrame(w, frame{'E', []byte("SFATAL\x00C0A000\x00MFaultline: unsupported protocol, authentication or operation\x00\x00")})
}
