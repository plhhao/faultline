package integration_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/plhhao/faultline/internal/control"
	httpproxy "github.com/plhhao/faultline/internal/proxy/http"
)

func certificate(t *testing.T, wrongHost bool) (tls.Certificate, string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Faultline test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}}
	if !wrongHost {
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatal(err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	return cert, certFile, keyFile, roots
}

func TestTLSMatrixHTTP1(t *testing.T) {
	cert, certFile, keyFile, roots := certificate(t, false)
	for _, inbound := range []bool{false, true} {
		for _, outbound := range []bool{false, true} {
			t.Run(fmt.Sprintf("in=%t/out=%t", inbound, outbound), func(t *testing.T) {
				protocol := make(chan string, 1)
				upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { protocol <- r.Proto; w.Write([]byte("ok")) }))
				upstream.EnableHTTP2 = true
				if outbound {
					upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
					upstream.StartTLS()
				} else {
					upstream.Start()
				}
				defer upstream.Close()
				addr, extra, scheme := address(t), "", "http"
				if inbound {
					extra += fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n", certFile, keyFile)
					scheme = "https"
				}
				if outbound {
					extra += fmt.Sprintf("    upstream_tls: {ca_file: '%s'}\n", certFile)
				}
				start(t, document(t, addr, upstream.URL, extra, ""), false, httpproxy.Options{})
				c := client(t)
				transport := c.Transport.(*http.Transport)
				transport.TLSClientConfig = &tls.Config{RootCAs: roots}
				transport.ForceAttemptHTTP2 = true
				resp, err := c.Get(scheme + "://" + addr)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != 200 || resp.Proto != "HTTP/1.1" || receive(t, protocol) != "HTTP/1.1" {
					t.Fatalf("%+v", resp)
				}
				if inbound && resp.TLS.NegotiatedProtocol != "http/1.1" {
					t.Fatal(resp.TLS.NegotiatedProtocol)
				}
			})
		}
	}
}

func TestUpstreamTLSFailuresAreNatural(t *testing.T) {
	for _, wrongHost := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongHost), func(t *testing.T) {
			cert, certFile, _, _ := certificate(t, wrongHost)
			var attempts atomic.Int32
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts.Add(1) }))
			upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
			upstream.StartTLS()
			defer upstream.Close()
			extra := rule
			if wrongHost {
				extra += fmt.Sprintf("    upstream_tls: {ca_file: '%s'}\n", certFile)
			}
			addr := address(t)
			reports := make(chan httpproxy.Report, 1)
			start(t, document(t, addr, upstream.URL, extra, ""), true, httpproxy.Options{Observe: func(r httpproxy.Report) { reports <- r }})
			if status, _ := readResponse(t, client(t), "http://"+addr); status != 502 {
				t.Fatal(status)
			}
			r := receive(t, reports)
			if r.Outcome != "upstream_error" || r.ErrorKind != "upstream_tls" || !r.NotReached || r.Applied || attempts.Load() != 0 {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestChangedListenerKeyFailsBeforeServing(t *testing.T) {
	_, certFile, keyFile, _ := certificate(t, false)
	addr := address(t)
	doc := document(t, addr, "http://127.0.0.1:9000", fmt.Sprintf("    tls: {cert_file: '%s', key_file: '%s'}\n", certFile, keyFile), "")
	if err := os.WriteFile(keyFile, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := control.New(doc)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := httpproxy.Start(s, httpproxy.Options{}); err == nil {
		p.Close()
		t.Fatal("invalid key accepted")
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
}

func TestUpstreamSNI(t *testing.T) {
	cert, certFile, _, _ := certificate(t, false)
	name := make(chan string, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) { name <- hello.ServerName; return nil, nil }}
	upstream.StartTLS()
	defer upstream.Close()
	addr := address(t)
	start(t, document(t, addr, strings.Replace(upstream.URL, "127.0.0.1", "localhost", 1), fmt.Sprintf("    upstream_tls: {ca_file: '%s'}\n", certFile), ""), false, httpproxy.Options{})
	if status, _ := readResponse(t, client(t), "http://"+addr); status != 204 {
		t.Fatal(status)
	}
	if got := receive(t, name); got != "localhost" {
		t.Fatal(got)
	}
}
