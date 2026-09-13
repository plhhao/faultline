package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type rootConfig struct {
	APIVersion string   `yaml:"api_version"`
	Seed       uint64   `yaml:"seed"`
	Runtime    Runtime  `yaml:"runtime"`
	Proxies    []Proxy  `yaml:"proxies"`
	Include    []string `yaml:"include"`
}

type proxySource struct{ file, path string }

func proxySources(file string, count int) []proxySource {
	sources := make([]proxySource, count)
	for i := range sources {
		sources[i] = proxySource{file, fmt.Sprintf("proxies[%d]", i)}
	}
	return sources
}

func sourceError(err error, file string) error {
	var field *FieldError
	if errors.As(err, &field) {
		copy := *field
		copy.File = file
		return &copy
	}
	return err
}

// Load reads the complete file set before validating and publishing a Document.
func Load(filename string) (*Document, error) {
	if filename == "" {
		return nil, invalid("config", "filename is required")
	}
	root, err := filepath.Abs(filename)
	if err != nil {
		return nil, invalid("config", "cannot resolve filename")
	}
	info, err := os.Stat(root)
	if err != nil || !info.Mode().IsRegular() {
		return nil, sourceError(invalid("config", "expected a readable regular file"), root)
	}
	data, err := os.ReadFile(root)
	if err != nil {
		return nil, sourceError(invalid("config", "cannot read file"), root)
	}
	raw := rootConfig{Runtime: DefaultRuntime()}
	if err := decodeDocument(data, &raw); err != nil {
		return nil, sourceError(err, root)
	}
	c := Config{APIVersion: raw.APIVersion, Seed: raw.Seed, Runtime: raw.Runtime, Proxies: raw.Proxies}
	sources := proxySources(root, len(c.Proxies))
	seen := []struct {
		file string
		info os.FileInfo
	}{{root, info}}
	for i, pattern := range raw.Include {
		field := fmt.Sprintf("include[%d]", i)
		if pattern == "" {
			return nil, sourceError(invalid(field, "empty include path"), root)
		}
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(filepath.Dir(root), pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			return nil, sourceError(invalid(field, "invalid glob or no matching files"), root)
		}
		for _, file := range matches {
			info, err := os.Stat(file)
			if err != nil || !info.Mode().IsRegular() {
				return nil, sourceError(invalid(field, "expected a readable regular file: "+file), root)
			}
			for _, previous := range seen {
				if os.SameFile(info, previous.info) {
					return nil, sourceError(invalid(field, "file included more than once: "+file+"; first source: "+previous.file), root)
				}
			}
			seen = append(seen, struct {
				file string
				info os.FileInfo
			}{file, info})
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, sourceError(invalid("config", "cannot read file"), file)
			}
			var fragment struct {
				Proxies []Proxy `yaml:"proxies"`
			}
			if err := decodeDocument(data, &fragment); err != nil {
				return nil, sourceError(err, file)
			}
			if len(fragment.Proxies) == 0 {
				return nil, sourceError(invalid("proxies", "fragment must contain at least one proxy"), file)
			}
			c.Proxies = append(c.Proxies, fragment.Proxies...)
			sources = append(sources, proxySources(file, len(fragment.Proxies))...)
		}
	}
	return finish(c, sources, root)
}

func finish(c Config, sources []proxySource, root string) (*Document, error) {
	ids := map[string]proxySource{}
	for i := range c.Proxies {
		p, src := &c.Proxies[i], sources[i]
		if previous, exists := ids[p.ID]; exists {
			return nil, sourceError(invalid(src.path+".id", "duplicate proxy ID; first declaration: "+previous.file+": "+previous.path+".id"), src.file)
		}
		ids[p.ID] = src
		rules := map[string]int{}
		for j, rule := range p.Rules {
			if previous, exists := rules[rule.ID]; exists {
				return nil, sourceError(invalid(fmt.Sprintf("%s.rules[%d].id", src.path, j), fmt.Sprintf("duplicate rule ID; first declaration: %s: %s.rules[%d].id", src.file, src.path, previous)), src.file)
			}
			rules[rule.ID] = j
		}
		resolve := func(path *string) {
			if *path != "" && !filepath.IsAbs(*path) {
				*path = filepath.Join(filepath.Dir(src.file), *path)
			}
		}
		if p.TLS != nil {
			resolve(&p.TLS.CertFile)
			resolve(&p.TLS.KeyFile)
		}
		if p.UpstreamTLS != nil {
			resolve(&p.UpstreamTLS.CAFile)
		}
	}
	digests, err := validate(&c, filepath.Dir(root))
	if err != nil {
		var field *FieldError
		if errors.As(err, &field) {
			for i, src := range sources {
				prefix := fmt.Sprintf("proxies[%d]", i)
				if strings.HasPrefix(field.Path, prefix+".") {
					return nil, &FieldError{File: src.file, Path: src.path + strings.TrimPrefix(field.Path, prefix), Message: field.Message}
				}
			}
		}
		return nil, sourceError(err, root)
	}
	return newDocument(c, digests)
}
