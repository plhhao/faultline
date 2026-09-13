package config_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"faultline/internal/config"
)

func writeConfig(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func proxyYAML(id string, port int) string {
	return fmt.Sprintf("  - id: %s\n    protocol: http1\n    listen: :%d\n    upstream: http://localhost\n    rules: []\n", id, port)
}

func TestLoadInlineAndIncludes(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	header := "api_version: faultline/v1alpha1\nseed: 42\n"
	writeConfig(t, root, header+"include: ['proxies/*.yaml']\nproxies:\n"+proxyYAML("inline", 8080))
	writeConfig(t, filepath.Join(dir, "proxies", "b.yaml"), "proxies:\n"+proxyYAML("b", 8082))
	writeConfig(t, filepath.Join(dir, "proxies", "a.yaml"), "proxies:\n"+proxyYAML("a", 8081))
	doc, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"inline", "a", "b"} {
		if doc.Config().Proxies[i].ID != id {
			t.Fatal("unstable include order")
		}
	}
	flat, err := config.Parse([]byte(header+"proxies:\n"+proxyYAML("b", 8082)+proxyYAML("inline", 8080)+proxyYAML("a", 8081)), root)
	if err != nil || !doc.Equal(flat) {
		t.Fatalf("file layout changed effective config: %v", err)
	}
	writeConfig(t, root, header+"include: ['proxies/b.yaml', 'proxies/a.yaml']\n")
	doc, err = config.Load(root)
	if err != nil || doc.Config().Proxies[0].ID != "b" {
		t.Fatalf("include list order: %v", err)
	}
	if _, err := config.Parse([]byte(header+"include: ['proxies/*.yaml']"), root); err == nil {
		t.Fatal("standalone Parse silently accepted include")
	}
}

func TestLoadRejectsInvalidFileSets(t *testing.T) {
	for _, tt := range []struct{ name, include, fragment, field string }{
		{"missing", "missing.yaml", "", "include[0]"},
		{"empty glob", "missing/*.yaml", "", "include[0]"},
		{"bad glob", "[", "", "include[0]"},
		{"empty path", "", "", "include[0]"},
		{"directory", "proxies", "", "include[0]"},
		{"self", "root.yaml", "", "include[0]"},
		{"empty fragment", "proxies/a.yaml", "proxies: []", "proxies"},
		{"missing proxies", "proxies/a.yaml", "{}", "proxies"},
		{"nested", "proxies/a.yaml", "include: [b.yaml]", "include"},
		{"fragment seed", "proxies/a.yaml", "seed: 42", "seed"},
		{"fragment runtime", "proxies/a.yaml", "runtime: {}", "runtime"},
		{"fragment version", "proxies/a.yaml", "api_version: faultline/v1alpha1", "api_version"},
		{"syntax", "proxies/a.yaml", "proxies: [", "config"},
		{"multi document", "proxies/a.yaml", "proxies: []\n---\n{}", "config"},
		{"semantic source", "proxies/a.yaml", "proxies:\n" + strings.Replace(proxyYAML("a", 8080), "protocol: http1", "protocol: secret-value", 1), "proxies[0].protocol"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "root.yaml")
			writeConfig(t, filepath.Join(dir, "proxies", "a.yaml"), tt.fragment)
			writeConfig(t, root, fmt.Sprintf("api_version: faultline/v1alpha1\ninclude: [%q]\n", tt.include))
			_, err := config.Load(root)
			var field *config.FieldError
			if !errors.As(err, &field) || field.Path != tt.field || field.File == "" {
				t.Fatalf("expected source/%s: %v", tt.field, err)
			}
			if tt.name == "semantic source" && field.File != filepath.Join(dir, "proxies", "a.yaml") {
				t.Fatal("lost fragment source")
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("leaked value")
			}
		})
	}
}

func TestLoadDuplicateIDsAndFiles(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.yaml")
	a, b := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")
	writeConfig(t, a, "proxies:\n"+proxyYAML("same", 8080))
	writeConfig(t, b, "proxies:\n"+proxyYAML("same", 8081))
	writeConfig(t, root, "api_version: faultline/v1alpha1\ninclude: [a.yaml, b.yaml]\n")
	_, err := config.Load(root)
	if err == nil || !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Fatalf("missing both duplicate locations: %v", err)
	}
	writeConfig(t, root, "api_version: faultline/v1alpha1\ninclude: [a.yaml]\nproxies:\n"+proxyYAML("same", 8081))
	_, err = config.Load(root)
	if err == nil || !strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), a) {
		t.Fatalf("inline duplicate: %v", err)
	}
	for _, second := range []string{"a.yaml", "alias.yaml", "hard.yaml"} {
		if second == "alias.yaml" {
			if err := os.Symlink(a, filepath.Join(dir, second)); err != nil {
				t.Fatal(err)
			}
		}
		if second == "hard.yaml" {
			if err := os.Link(a, filepath.Join(dir, second)); err != nil {
				t.Fatal(err)
			}
		}
		writeConfig(t, root, fmt.Sprintf("api_version: faultline/v1alpha1\ninclude: [a.yaml, %s]\n", second))
		_, err := config.Load(root)
		if err == nil || !strings.Contains(err.Error(), "file included more than once") {
			t.Fatalf("file identity not checked: %v", err)
		}
	}
	rule := "      - id: shared\n        select: {nth: 1}\n        fault: {action: close_connection, phase: before_upstream_request}\n"
	fragment := "proxies:\n" + strings.Replace(proxyYAML("a", 8080), "    rules: []\n", "    rules:\n"+rule+rule, 1)
	writeConfig(t, a, fragment)
	writeConfig(t, root, "api_version: faultline/v1alpha1\ninclude: [a.yaml]\n")
	_, err = config.Load(root)
	if err == nil || !strings.Contains(err.Error(), "rules[0].id") || !strings.Contains(err.Error(), "rules[1].id") {
		t.Fatalf("duplicate rule sources: %v", err)
	}
	writeConfig(t, a, strings.Replace(fragment, rule+rule, rule, 1))
	writeConfig(t, b, strings.Replace(strings.Replace(strings.Replace(fragment, rule+rule, rule, 1), "id: a\n", "id: b\n", 1), ":8080", ":8081", 1))
	writeConfig(t, root, "api_version: faultline/v1alpha1\ninclude: [a.yaml, b.yaml]\n")
	if _, err := config.Load(root); err != nil {
		t.Fatalf("same rule ID across proxies: %v", err)
	}
}

func TestLoadFragmentTLSPaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "proxies")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	writeCertificate(t, sub)
	root := filepath.Join(dir, "root.yaml")
	writeConfig(t, root, "api_version: faultline/v1alpha1\ninclude: [proxies/a.yaml]\n")
	fragment := "proxies:\n" + strings.Replace(proxyYAML("a", 8080), "    rules: []", "    tls: {cert_file: cert.pem, key_file: key.pem}\n    rules: []", 1)
	fragment = strings.Replace(fragment, "upstream: http://localhost", "upstream: https://localhost\n    upstream_tls: {ca_file: cert.pem}", 1)
	writeConfig(t, filepath.Join(sub, "a.yaml"), fragment)
	doc, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config().Proxies[0].TLS.CertFile != filepath.Join(sub, "cert.pem") || doc.Config().Proxies[0].UpstreamTLS.CAFile != filepath.Join(sub, "cert.pem") {
		t.Fatal("wrong declaring directory")
	}
	flat := strings.ReplaceAll(fragment, "cert.pem", filepath.Join(sub, "cert.pem"))
	flat = strings.ReplaceAll(flat, "key.pem", filepath.Join(sub, "key.pem"))
	one, err := config.Parse([]byte("api_version: faultline/v1alpha1\n"+flat), root)
	if err != nil || !doc.Equal(one) {
		t.Fatalf("source location changed TLS config: %v", err)
	}
	other := filepath.Join(dir, "other")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cert.pem", "key.pem"} {
		data, err := os.ReadFile(filepath.Join(sub, name))
		if err != nil {
			t.Fatal(err)
		}
		writeConfig(t, filepath.Join(other, name), string(data))
	}
	writeConfig(t, filepath.Join(other, "a.yaml"), fragment)
	writeConfig(t, root, "api_version: faultline/v1alpha1\ninclude: [other/a.yaml]\n")
	moved, err := config.Load(root)
	if err != nil || doc.RestartCompatible(moved) {
		t.Fatalf("changed resolved TLS paths must require restart: %v", err)
	}
}

func TestMultiFileExample(t *testing.T) {
	if _, err := config.Load("../../examples/http/multi-file/faultline.yaml"); err != nil {
		t.Fatal(err)
	}
}
