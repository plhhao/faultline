package httpproxy

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/plhhao/faultline/internal/config"
)

func protocols(protocol string, secure bool) *http.Protocols {
	p := new(http.Protocols)
	if protocol == "http1" {
		p.SetHTTP1(true)
	} else if secure {
		p.SetHTTP2(true)
	} else {
		p.SetUnencryptedHTTP2(true)
	}
	return p
}

func alpn(protocol string) []string {
	if protocol == "http1" {
		return []string{"http/1.1"}
	}
	return []string{"h2"}
}

func transportFor(p config.Proxy) (*http.Transport, *tls.Config, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.Protocols = protocols(p.UpstreamProtocol, strings.HasPrefix(p.Upstream, "https://"))
	t.ForceAttemptHTTP2 = false
	// HTTP/1 uses fresh connections; HTTP/2 bypasses Transport.RoundTrip's retry loop.
	t.DisableKeepAlives = true
	t.DisableCompression = true
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: alpn(p.UpstreamProtocol)}
	if p.UpstreamProtocol == "http2" {
		t.TLSClientConfig.VerifyConnection = func(state tls.ConnectionState) error {
			if state.NegotiatedProtocol != "h2" {
				return errors.New("upstream did not negotiate HTTP/2")
			}
			return nil
		}
	}
	if p.UpstreamTLS != nil && p.UpstreamTLS.CAFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, nil, errors.New("cannot load system CA pool")
		}
		data, err := os.ReadFile(p.UpstreamTLS.CAFile)
		if err != nil || !roots.AppendCertsFromPEM(data) {
			return nil, nil, errors.New("cannot load upstream CA")
		}
		t.TLSClientConfig.RootCAs = roots
	}
	if p.UpstreamTLS != nil && p.UpstreamTLS.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(p.UpstreamTLS.CertFile, p.UpstreamTLS.KeyFile)
		if err != nil {
			return nil, nil, errors.New("cannot load upstream client certificate/key")
		}
		t.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}
	var listenerTLS *tls.Config
	if p.TLS != nil {
		cert, err := tls.LoadX509KeyPair(p.TLS.CertFile, p.TLS.KeyFile)
		if err != nil {
			return nil, nil, errors.New("cannot load listener certificate/key")
		}
		listenerTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, NextProtos: alpn(p.Protocol)}
	}
	if p.TLS != nil && p.TLS.ClientCAFile != "" {
		roots := x509.NewCertPool()
		data, err := os.ReadFile(p.TLS.ClientCAFile)
		if err != nil || !roots.AppendCertsFromPEM(data) {
			return nil, nil, errors.New("cannot load listener client CA")
		}
		listenerTLS.ClientCAs = roots
		listenerTLS.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return t, listenerTLS, nil
}
