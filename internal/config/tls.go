package config

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
)

func validateTLS(p *Proxy, base, path string, digests map[string][32]byte) error {
	read := func(name *string, field string) ([]byte, error) {
		if *name == "" {
			return nil, invalid(field, "file is required")
		}
		if !filepath.IsAbs(*name) {
			*name = filepath.Join(base, *name)
		}
		*name = filepath.Clean(*name)
		data, err := os.ReadFile(*name)
		if err != nil {
			return nil, invalid(field, "cannot read file")
		}
		digests[*name] = sha256.Sum256(data)
		return data, nil
	}
	if p.TLS != nil {
		cert, err := read(&p.TLS.CertFile, path+".tls.cert_file")
		if err != nil {
			return err
		}
		key, err := read(&p.TLS.KeyFile, path+".tls.key_file")
		if err != nil {
			return err
		}
		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return invalid(path+".tls", "invalid or mismatched certificate/key")
		}
	}

	validateCA := func(name *string, field string) error {
		data, err := read(name, field)
		if err != nil {
			return err
		}
		count := 0
		for len(data) > 0 {
			block, rest := pem.Decode(data)
			if block == nil || block.Type != "CERTIFICATE" {
				return invalid(field, "expected PEM certificates")
			}
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return invalid(field, "invalid CA certificate")
			}
			count++
			data = bytes.TrimSpace(rest)
		}
		if count == 0 {
			return invalid(field, "expected PEM certificates")
		}
		return nil
	}
	if p.TLS != nil && p.TLS.ClientCAFile != "" {
		if err := validateCA(&p.TLS.ClientCAFile, path+".tls.client_ca_file"); err != nil {
			return err
		}
	}
	if p.UpstreamTLS != nil {
		if !strings.HasPrefix(p.Upstream, "https://") {
			return invalid(path+".upstream_tls", "requires HTTPS upstream")
		}
		u := p.UpstreamTLS
		if u.CAFile == "" && u.CertFile == "" && u.KeyFile == "" {
			return invalid(path+".upstream_tls", "CA or client certificate/key is required")
		}
		if u.CAFile != "" {
			if err := validateCA(&u.CAFile, path+".upstream_tls.ca_file"); err != nil {
				return err
			}
		}
		if u.CertFile != "" || u.KeyFile != "" {
			cert, err := read(&u.CertFile, path+".upstream_tls.cert_file")
			if err != nil {
				return err
			}
			key, err := read(&u.KeyFile, path+".upstream_tls.key_file")
			if err != nil {
				return err
			}
			if _, err := tls.X509KeyPair(cert, key); err != nil {
				return invalid(path+".upstream_tls", "invalid or mismatched certificate/key")
			}
		}
	}
	return nil
}
