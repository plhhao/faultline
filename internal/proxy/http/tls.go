package httpproxy

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"

	"faultline/internal/config"
)

func http1() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	return p
}

func transportFor(p config.Proxy) (*http.Transport, *tls.Config, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.Protocols = http1()
	t.ForceAttemptHTTP2 = false
	// Fresh upstream connections prevent Transport's retry-on-reuse behavior.
	t.DisableKeepAlives = true
	t.DisableCompression = true
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}
	if p.UpstreamTLS != nil {
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
	var listenerTLS *tls.Config
	if p.TLS != nil {
		cert, err := tls.LoadX509KeyPair(p.TLS.CertFile, p.TLS.KeyFile)
		if err != nil {
			return nil, nil, errors.New("cannot load listener certificate/key")
		}
		listenerTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}}
	}
	return t, listenerTLS, nil
}
