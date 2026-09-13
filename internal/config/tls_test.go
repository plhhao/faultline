package config_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"faultline/internal/config"
)

func writeCertificate(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "key.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw})} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTLSPathsValidationAndChanges(t *testing.T) {
	dir := t.TempDir()
	writeCertificate(t, dir)
	data := strings.Replace(validYAML, "http://EXAMPLE.test:80/", "https://localhost", 1)
	data = strings.Replace(data, "    rules:", "    tls: {cert_file: cert.pem, key_file: key.pem}\n    upstream_tls: {ca_file: cert.pem}\n    rules:", 1)
	filename := filepath.Join(dir, "faultline.yaml")
	d, err := config.Parse([]byte(data), filename)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Config().Proxies[0]
	if p.TLS.CertFile != filepath.Join(dir, "cert.pem") || p.UpstreamTLS.CAFile != p.TLS.CertFile {
		t.Fatal("relative path not resolved against config")
	}
	p.TLS.CertFile = "changed"
	p.UpstreamTLS.CAFile = "changed"
	if d.Config().Proxies[0].TLS.CertFile == "changed" || d.Config().Proxies[0].UpstreamTLS.CAFile == "changed" {
		t.Fatal("TLS mutation escaped")
	}
	for _, bad := range []string{
		strings.Replace(data, "key_file: key.pem", "key_file: missing.pem", 1),
		strings.Replace(data, "key_file: key.pem", "key_file: cert.pem", 1),
		strings.Replace(data, "ca_file: cert.pem", "ca_file: key.pem", 1),
		strings.Replace(data, "https://localhost", "http://localhost", 1),
	} {
		if _, err := config.Parse([]byte(bad), filename); err == nil {
			t.Fatal("invalid TLS accepted")
		}
	}
	other := t.TempDir()
	writeCertificate(t, other)
	mismatched := strings.Replace(data, "key_file: key.pem", "key_file: "+filepath.Join(other, "key.pem"), 1)
	if _, err := config.Parse([]byte(mismatched), filename); err == nil {
		t.Fatal("mismatched key accepted")
	}
	writeCertificate(t, dir)
	changed, err := config.Parse([]byte(data), filename)
	if err != nil {
		t.Fatal(err)
	}
	if d.Equal(changed) || d.RestartCompatible(changed) {
		t.Fatal("changed certificate bytes must require restart")
	}
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), []byte("secret-value"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Parse([]byte(data), filename); err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("unsafe TLS error: %v", err)
	}
}
