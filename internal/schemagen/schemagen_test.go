// SPDX-License-Identifier: Apache-2.0

package schemagen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type schemaString string

func (s schemaString) MarshalJSON() ([]byte, error) {
	return []byte(`"` + string(s) + `"`), nil
}

type schemaInner struct {
	Name   string `json:"name"`
	Skip   string `json:"-"`
	hidden string
}

type schemaProbe struct {
	Title   string            `json:"title"`
	Count   int               `json:"count,omitempty"`
	Flag    bool              `yaml:"flag"`
	Tags    []string          `json:"tags,omitempty"`
	Raw     []byte            `json:"raw,omitempty"`
	Meta    map[string]any    `json:"meta,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	Inner   schemaInner       `json:"inner"`
	Wrapped schemaString      `json:"wrapped"`
	Any     any               `json:"any,omitempty"`
}

func probeProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", schema)
	}
	raw, ok := properties[name]
	if !ok {
		t.Fatalf("property %q missing: %v", name, properties)
	}
	prop, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("property %q is not an object: %v", name, raw)
	}
	return prop
}

func TestStructSchemaProbe(t *testing.T) {
	t.Parallel()
	_ = schemaInner{hidden: "unexported fields never surface"}
	schema := StructSchema("probe", schemaProbe{}, "json")
	if schema["$schema"] != Draft {
		t.Errorf("$schema = %v, want draft %q", schema["$schema"], Draft)
	}
	if schema["type"] != "object" {
		t.Errorf("type = %v, want object", schema["type"])
	}
	if got := probeProperty(t, schema, "title")["type"]; got != "string" {
		t.Errorf("title type = %v, want string", got)
	}
	if got := probeProperty(t, schema, "Flag")["type"]; got != "boolean" {
		t.Errorf("Flag type = %v, want boolean", got)
	}
	if got := probeProperty(t, schema, "count")["type"]; got != "integer" {
		t.Errorf("count type = %v, want integer", got)
	}
	items, ok := probeProperty(t, schema, "tags")["items"].(map[string]any)
	if !ok || items["type"] != "string" {
		t.Errorf("tags items = %v, want string items", probeProperty(t, schema, "tags"))
	}
	if got := probeProperty(t, schema, "raw")["type"]; got != "string" {
		t.Errorf("raw ([]byte) type = %v, want string", got)
	}
	meta := probeProperty(t, schema, "meta")
	if meta["type"] != "object" {
		t.Errorf("meta type = %v, want object", meta["type"])
	}
	if _, ok := meta["additionalProperties"]; ok {
		t.Errorf("meta has additionalProperties, want free-form object: %v", meta)
	}
	labels, ok := probeProperty(t, schema, "labels")["additionalProperties"].(map[string]any)
	if !ok || labels["type"] != "string" {
		t.Errorf("labels additionalProperties = %v, want string values", probeProperty(t, schema, "labels"))
	}
	inner := probeProperty(t, schema, "inner")
	if inner["type"] != "object" {
		t.Fatalf("inner type = %v, want object", inner["type"])
	}
	innerProps, ok := inner["properties"].(map[string]any)
	if !ok {
		t.Fatalf("inner has no properties: %v", inner)
	}
	if _, ok := innerProps["name"]; !ok {
		t.Errorf("inner properties = %v, want name", innerProps)
	}
	if _, ok := innerProps["Skip"]; ok {
		t.Errorf("inner properties expose skipped field: %v", innerProps)
	}
	if _, ok := innerProps["hidden"]; ok {
		t.Errorf("inner properties expose unexported field: %v", innerProps)
	}
	if got := probeProperty(t, schema, "wrapped")["type"]; got != "string" {
		t.Errorf("wrapped (json.Marshaler) type = %v, want string", got)
	}
	if len(probeProperty(t, schema, "any")) != 0 {
		t.Errorf("any property = %v, want empty schema", probeProperty(t, schema, "any"))
	}
	properties := schema["properties"].(map[string]any)
	if _, ok := properties["Skip"]; ok {
		t.Errorf("top-level properties expose skipped field: %v", properties)
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required = %v, want string list", schema["required"])
	}
	want := []string{"title", "Flag", "inner", "wrapped"}
	if !reflect.DeepEqual(required, want) {
		t.Errorf("required = %v, want %v (omitempty fields optional)", required, want)
	}
}

func TestStructSchemaUsesTagKey(t *testing.T) {
	t.Parallel()
	schema := StructSchema("probe", schemaProbe{}, "yaml")
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", schema)
	}
	if _, ok := properties["flag"]; !ok {
		t.Errorf("yaml properties = %v, want flag", properties)
	}
	if _, ok := properties["Title"]; !ok {
		t.Errorf("yaml properties = %v, want untagged Title under its field name", properties)
	}
	if _, ok := properties["title"]; ok {
		t.Errorf("yaml properties = %v, want no json tag names", properties)
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "probe.schema.json")
	schema := StructSchema("probe", schemaInner{}, "json")
	if err := WriteFile(path, schema); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("schema perms = %04o, want 0600", perm)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir() in this test
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("schema file misses its trailing newline")
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if decoded["title"] != "probe" {
		t.Errorf("title = %v, want probe", decoded["title"])
	}
}
