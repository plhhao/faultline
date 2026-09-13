package config

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"time"

	"go.yaml.in/yaml/v3"
)

type FieldError struct{ File, Path, Message string }

func (e *FieldError) Error() string {
	if e.File != "" {
		return e.File + ": " + e.Path + ": " + e.Message
	}
	return e.Path + ": " + e.Message
}
func invalid(path, message string) error { return &FieldError{Path: path, Message: message} }

// Parse decodes a standalone document; use Load for filesystem includes.
func Parse(data []byte, filename string) (*Document, error) {
	if filename == "" {
		return nil, invalid("config", "filename is required for relative paths")
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return nil, invalid("config", "cannot resolve filename")
	}
	c := Config{Runtime: DefaultRuntime()}
	if err := decodeDocument(data, &c); err != nil {
		return nil, sourceError(err, absolute)
	}
	return finish(c, proxySources(absolute, len(c.Proxies)), absolute)
}

func decodeDocument(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		return invalid("config", "expected one valid YAML document")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return invalid("config", "expected exactly one YAML document")
	}
	if len(node.Content) != 1 {
		return invalid("config", "expected a mapping")
	}
	return decodeNode(node.Content[0], reflect.ValueOf(target).Elem(), "")
}

// decodeNode keeps field paths and suppresses YAML errors that may echo secret values.
func decodeNode(n *yaml.Node, v reflect.Value, path string) error {
	fail := func(message string) error {
		if path == "" {
			return invalid("config", message)
		}
		return invalid(path, message)
	}
	if n.Kind == yaml.AliasNode || n.Tag == "!!null" {
		return fail("aliases and null values are not supported")
	}
	if v.Kind() == reflect.Pointer {
		v.Set(reflect.New(v.Type().Elem()))
		return decodeNode(n, v.Elem(), path)
	}
	if v.Type() == reflect.TypeFor[time.Duration]() {
		if n.Tag != "!!str" {
			return fail("expected a duration string")
		}
		d, err := time.ParseDuration(n.Value)
		if err != nil {
			return fail("invalid duration")
		}
		v.SetInt(int64(d))
		return nil
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Map:
		if n.Kind != yaml.MappingNode {
			return fail("expected a mapping")
		}
		fields := map[string]int{}
		if v.Kind() == reflect.Struct {
			if v.Type() == reflect.TypeFor[Rule]() {
				v.FieldByName("Enabled").SetBool(true)
			}
			for i := 0; i < v.NumField(); i++ {
				fields[v.Type().Field(i).Tag.Get("yaml")] = i
			}
		} else {
			v.Set(reflect.MakeMap(v.Type()))
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			key, value := n.Content[i], n.Content[i+1]
			if key.Tag != "!!str" {
				return fail("expected string field names; YAML merges are not supported")
			}
			child := key.Value
			if path != "" {
				child = path + "." + child
			}
			if seen[key.Value] {
				return invalid(child, "duplicate field")
			}
			seen[key.Value] = true
			if v.Kind() == reflect.Struct {
				index, ok := fields[key.Value]
				if !ok {
					return invalid(child, "unknown field")
				}
				if err := decodeNode(value, v.Field(index), child); err != nil {
					return err
				}
			} else {
				item := reflect.New(v.Type().Elem()).Elem()
				if err := decodeNode(value, item, child); err != nil {
					return err
				}
				v.SetMapIndex(reflect.ValueOf(key.Value), item)
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return fail("expected a sequence")
		}
		v.Set(reflect.MakeSlice(v.Type(), len(n.Content), len(n.Content)))
		for i, item := range n.Content {
			if err := decodeNode(item, v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		tags := map[reflect.Kind]string{
			reflect.String: "!!str", reflect.Bool: "!!bool", reflect.Int: "!!int", reflect.Uint64: "!!int", reflect.Float64: "!!float",
		}
		tag := tags[v.Kind()]
		if n.Kind != yaml.ScalarNode || (n.Tag != tag && !(v.Kind() == reflect.Float64 && n.Tag == "!!int")) {
			return fail("invalid scalar type")
		}
		if err := n.Decode(v.Addr().Interface()); err != nil {
			return fail("invalid scalar value")
		}
	}
	return nil
}
