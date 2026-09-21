package mysql

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"net"
)

func greeting(b []byte, secure bool) (uint32, bool) {
	if len(b) < 1 || b[0] != 10 {
		return 0, false
	}
	end := bytes.IndexByte(b[1:], 0)
	if end < 0 {
		return 0, false
	}
	lo := 1 + end + 1 + 4 + 8 + 1
	if len(b) < lo+18 {
		return 0, false
	}
	flags := uint32(binary.LittleEndian.Uint16(b[lo:])) | uint32(binary.LittleEndian.Uint16(b[lo+5:]))<<16
	if flags&capProtocol41 == 0 || flags&capPluginAuth == 0 || !bytes.HasSuffix(b, []byte("caching_sha2_password\x00")) {
		return 0, false
	}
	if secure && flags&capSSL == 0 {
		return 0, false
	}
	flags &^= forbiddenCaps
	if !secure {
		flags &^= capSSL
	}
	binary.LittleEndian.PutUint16(b[lo:], uint16(flags))
	binary.LittleEndian.PutUint16(b[lo+5:], uint16(flags>>16))
	return flags, true
}

func authenticate(ctx context.Context, e *endpoint, c, u net.Conn) (net.Conn, net.Conn, uint32, error) {
	hello, err := readPacket(u)
	if err != nil || hello.seq != 0 {
		return c, u, 0, errProtocol
	}
	offered, ok := greeting(hello.body, e.clientTLS != nil)
	if !ok {
		return c, u, 0, errProtocol
	}
	if err = writePacket(c, hello); err != nil {
		return c, u, 0, err
	}
	seq := byte(1)
	response, err := readPacket(c)
	if err != nil || response.seq != seq || len(response.body) < 32 {
		return c, u, 0, errProtocol
	}
	seq++
	flags := binary.LittleEndian.Uint32(response.body)
	if flags&forbiddenCaps != 0 || flags&capProtocol41 == 0 || flags&capPluginAuth == 0 || flags&^offered != 0 {
		return c, u, 0, errProtocol
	}
	if e.clientTLS != nil {
		if flags&capSSL == 0 || len(response.body) != 32 {
			return c, u, 0, errProtocol
		}
		if err = writePacket(u, response); err != nil {
			return c, u, 0, err
		}
		upstream := tls.Client(u, e.upstreamTLS)
		if err = upstream.HandshakeContext(ctx); err != nil {
			return c, u, 0, err
		}
		u = upstream
		client := tls.Server(c, e.clientTLS)
		if err = client.HandshakeContext(ctx); err != nil {
			return c, u, 0, err
		}
		c = client
		response, err = readPacket(c)
		if err != nil || response.seq != seq || len(response.body) <= 32 || binary.LittleEndian.Uint32(response.body) != flags {
			return c, u, 0, errProtocol
		}
		seq++
	} else if flags&capSSL != 0 {
		return c, u, 0, errProtocol
	}
	if err = writePacket(u, response); err != nil {
		return c, u, 0, err
	}
	for round := 0; round < 12; round++ {
		p, err := readPacket(u)
		if err != nil || p.seq != seq {
			return c, u, 0, errProtocol
		}
		seq++
		needResponse := true
		switch p.body[0] {
		case 0:
			if _, ok := okStatus(p.body); !ok {
				return c, u, 0, errProtocol
			}
			return c, u, flags, writePacket(c, p)
		case 0xff:
			_ = writePacket(c, p)
			return c, u, 0, errProtocol
		case 0xfe:
			if !bytes.HasPrefix(p.body[1:], []byte("caching_sha2_password\x00")) {
				return c, u, 0, errProtocol
			}
		case 1:
			if len(p.body) == 2 {
				switch p.body[1] {
				case 3:
					needResponse = false
				case 4:
				default:
					return c, u, 0, errProtocol
				}
			} else if !bytes.HasPrefix(p.body[1:], []byte("-----BEGIN PUBLIC KEY-----")) {
				return c, u, 0, errProtocol
			}
		default:
			return c, u, 0, errProtocol
		}
		if err = writePacket(c, p); err != nil {
			return c, u, 0, err
		}
		if needResponse {
			p, err = readPacket(c)
			if err != nil || p.seq != seq {
				return c, u, 0, errProtocol
			}
			seq++
			if err = writePacket(u, p); err != nil {
				return c, u, 0, err
			}
		}
	}
	return c, u, 0, errProtocol
}
