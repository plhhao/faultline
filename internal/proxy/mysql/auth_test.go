package mysql

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func testGreeting() []byte {
	b := append([]byte{10}, []byte("8.4.8\x00")...)
	b = append(b, make([]byte, 4+8+1)...)
	offset := len(b)
	b = append(b, make([]byte, 18+13)...)
	flags := capProtocol41 | capPluginAuth | capSSL | capDeprecateEOF | forbiddenCaps
	binary.LittleEndian.PutUint16(b[offset:], uint16(flags))
	binary.LittleEndian.PutUint16(b[offset+5:], uint16(flags>>16))
	b = append(b, []byte("caching_sha2_password\x00")...)
	return b
}
func TestGreetingCapabilities(t *testing.T) {
	for _, secure := range []bool{false, true} {
		b := testGreeting()
		flags, ok := greeting(b, secure)
		if !ok || flags&forbiddenCaps != 0 || (flags&capSSL != 0) != secure {
			t.Fatal(flags, ok)
		}
	}
	for _, b := range [][]byte{nil, {10}, testGreeting()[:20], append(testGreeting(), 1)} {
		if _, ok := greeting(b, false); ok {
			t.Fatal("malformed greeting accepted")
		}
	}
}
func TestAuthenticationSwitch(t *testing.T) {
	for _, plugin := range []string{"caching_sha2_password", "mysql_native_password"} {
		t.Run(plugin, func(t *testing.T) {
			c, front := net.Pipe()
			up, u := net.Pipe()
			defer c.Close()
			defer front.Close()
			defer up.Close()
			defer u.Close()
			for _, conn := range []net.Conn{c, front, up, u} {
				conn.SetDeadline(time.Now().Add(time.Second))
			}
			done := make(chan error, 1)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() { _, _, _, err := authenticate(ctx, &endpoint{}, front, up); done <- err }()
			server := make(chan error, 1)
			go func() {
				if err := writePacket(u, packet{0, testGreeting()}); err != nil {
					server <- err
					return
				}
				if _, err := readPacket(u); err != nil {
					server <- err
					return
				}
				if err := writePacket(u, packet{2, append([]byte{0xfe}, []byte(plugin+"\x00salt")...)}); err != nil {
					server <- err
					return
				}
				if plugin != "caching_sha2_password" {
					server <- nil
					return
				}
				if _, err := readPacket(u); err != nil {
					server <- err
					return
				}
				server <- writePacket(u, packet{4, []byte{0, 0, 0, 2, 0, 0, 0}})
			}()
			if _, err := readPacket(c); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, 33)
			binary.LittleEndian.PutUint32(response, capProtocol41|capPluginAuth)
			if err := writePacket(c, packet{1, response}); err != nil {
				t.Fatal(err)
			}
			if plugin == "caching_sha2_password" {
				p, err := readPacket(c)
				if err != nil || p.seq != 2 {
					t.Fatal(err)
				}
				if err := writePacket(c, packet{3, []byte("auth")}); err != nil {
					t.Fatal(err)
				}
				p, err = readPacket(c)
				if err != nil || p.body[0] != 0 {
					t.Fatal(err)
				}
			}
			if err := <-done; (err == nil) != (plugin == "caching_sha2_password") {
				t.Fatal(err)
			}
			if err := <-server; err != nil {
				t.Fatal(err)
			}
		})
	}
}
