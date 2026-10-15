// SPDX-License-Identifier: Apache-2.0

// Package schemagen generates JSON Schema documents from the same Go
// structs used by the read/write logic, so generated schemas never drift
// from the file shapes. Property names come from struct tags, and fields
// without omitempty are required.
// ~keep
package schemagen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Draft is the JSON Schema dialect every generated schema declares.
// ~keep
const Draft = "https://json-schema.org/draft/2020-12/schema"

var marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// StructSchema builds a JSON Schema object for the struct value v, naming
// properties from the given struct tag key ("json" or "yaml"). Fields whose
// tag carries omitempty are optional; every other named field is required.
// ~keep
func StructSchema(title string, value any, tag string) map[string]any {
	kind := reflect.TypeOf(value)
	for kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	properties, required := structProperties(kind, tag)
	schema := map[string]any{
		"$schema":    Draft,
		"title":      title,
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func structProperties(kind reflect.Type, tag string) (map[string]any, []string) {
	properties := make(map[string]any, kind.NumField())
	var required []string
	for i := range kind.NumField() {
		field := kind.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, omitempty := fieldName(field, tag)
		if name == "" || name == "-" {
			continue
		}
		properties[name] = typeSchema(field.Type, tag)
		if !omitempty {
			required = append(required, name)
		}
	}
	return properties, required
}

func fieldName(field reflect.StructField, tag string) (string, bool) {
	raw, ok := field.Tag.Lookup(tag)
	if !ok {
		return field.Name, false
	}
	parts := strings.Split(raw, ",")
	name := parts[0]
	var omitempty bool
	for _, opt := range parts[1:] {
		if strings.TrimSpace(opt) == "omitempty" {
			omitempty = true
		}
	}
	if name == "" {
		return field.Name, omitempty
	}
	return name, omitempty
}

func typeSchema(kind reflect.Type, tag string) map[string]any {
	if marshalsAsString(kind) {
		return map[string]any{"type": "string"}
	}
	for kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	switch kind.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if kind.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string"}
		}
		return map[string]any{"type": "array", "items": typeSchema(kind.Elem(), tag)}
	case reflect.Map:
		elem := kind.Elem()
		if elem.Kind() == reflect.Interface && elem.NumMethod() == 0 {
			return map[string]any{"type": "object"}
		}
		return map[string]any{"type": "object", "additionalProperties": typeSchema(elem, tag)}
	case reflect.Struct:
		properties, required := structProperties(kind, tag)
		schema := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	default:
		return map[string]any{}
	}
}

func marshalsAsString(kind reflect.Type) bool {
	if kind.Implements(marshalerType) {
		return true
	}
	if kind.Kind() != reflect.Pointer {
		return reflect.PointerTo(kind).Implements(marshalerType)
	}
	return false
}

// Marshal encodes a schema as indented JSON with a trailing newline.
// ~keep
func Marshal(schema map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		return nil, fmt.Errorf("encoding schema: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteFile writes a schema as indented JSON to path, creating parent
// directories. Schema files share the 0600 posture of the state and lock
// files they describe.
// ~keep
func WriteFile(path string, schema map[string]any) error {
	data, err := Marshal(schema)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("creating schema dir: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing schema %q: %w", path, err)
	}
	return nil
}
