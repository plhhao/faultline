package httpproxy

import (
	"io"
	"net"
	"net/http"
	"sync"
)

// singleAttempt bypasses Transport's retry loop. The flow owns this connection.
type singleAttempt struct{ transport *http.Transport }

func (t singleAttempt) RoundTrip(r *http.Request) (*http.Response, error) {
	port := r.URL.Port()
	if port == "" {
		if r.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	cc, err := t.transport.NewClientConn(r.Context(), r.URL.Scheme, net.JoinHostPort(r.URL.Hostname(), port))
	if err != nil {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, err
	}
	resp, err := cc.RoundTrip(r)
	if err != nil {
		cc.Close()
		return nil, err
	}
	resp.Body = &connectionBody{ReadCloser: resp.Body, conn: cc}
	return resp, nil
}

type connectionBody struct {
	io.ReadCloser
	conn *http.ClientConn
	once sync.Once
}

func (b *connectionBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { b.conn.Close() })
	return err
}
