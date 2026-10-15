// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func readTestFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) // #nosec G304 -- test-only path under t.TempDir() with a fixed schema file name
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(data)
}

func TestSavedStateCarriesVersionAndSchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := validState()
	if err := SaveState(dir, state); err != nil {
		t.Fatalf("SaveState error: %v", err)
	}
	if state.SchemaVersion != StateVersion {
		t.Errorf("SchemaVersion = %d, want %d after save", state.SchemaVersion, StateVersion)
	}
	if state.Schema != StateSchemaRef {
		t.Errorf("Schema = %q, want %q", state.Schema, StateSchemaRef)
	}
	raw := readTestFile(t, dir, StateFileName)
	if !strings.Contains(raw, "schema_version: 1") {
		t.Errorf("state.yml misses schema_version: 1:\n%s", raw)
	}
	if !strings.Contains(raw, "$schema: ./schemas/state.schema.json") {
		t.Errorf("state.yml misses its $schema reference:\n%s", raw)
	}
	schemaRaw := readTestFile(t, dir, StateSchemaFileName)
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaRaw), &schema); err != nil {
		t.Fatalf("state schema is not JSON: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("state schema has no properties: %v", schema)
	}
	for _, key := range []string{"version", "schema_version", "$schema", "template", "data"} {
		if _, ok := properties[key]; !ok {
			t.Errorf("state schema misses property %q", key)
		}
	}
	dataRef, ok := properties["data"].(map[string]any)
	if !ok || dataRef["$ref"] != StateDataRef {
		t.Errorf("state schema data = %v, want $ref to %q", properties["data"], StateDataRef)
	}
}

func TestSavedLockCarriesVersionAndSchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock := validLock()
	if err := SaveLock(dir, lock); err != nil {
		t.Fatalf("SaveLock error: %v", err)
	}
	if lock.SchemaVersion != StateVersion {
		t.Errorf("SchemaVersion = %d, want %d after save", lock.SchemaVersion, StateVersion)
	}
	if lock.Schema != LockSchemaRef {
		t.Errorf("Schema = %q, want %q", lock.Schema, LockSchemaRef)
	}
	raw := readTestFile(t, dir, LockFileName)
	if !strings.Contains(raw, `"schema_version": 1`) {
		t.Errorf("lock.json misses schema_version 1:\n%s", raw)
	}
	if !strings.Contains(raw, `"$schema": "./schemas/lock.schema.json"`) {
		t.Errorf("lock.json misses its $schema reference:\n%s", raw)
	}
	schemaRaw := readTestFile(t, dir, LockSchemaFileName)
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaRaw), &schema); err != nil {
		t.Fatalf("lock schema is not JSON: %v", err)
	}
	if _, ok := schema["properties"]; !ok {
		t.Errorf("lock schema has no properties: %v", schema)
	}
}

func TestSchemaVersionRejected(t *testing.T) {
	t.Parallel()
	t.Run("state missing schema_version rejected", func(t *testing.T) {
		t.Parallel()
		state := validState()
		state.SchemaVersion = 0
		if err := ValidateState(state); err == nil {
			t.Error("ValidateState without schema_version succeeded, want error")
		}
	})
	t.Run("state future schema_version rejected", func(t *testing.T) {
		t.Parallel()
		state := validState()
		state.SchemaVersion = StateVersion + 1
		if err := ValidateState(state); err == nil {
			t.Error("ValidateState with future schema_version succeeded, want error")
		}
	})
	t.Run("lock missing schema_version rejected", func(t *testing.T) {
		t.Parallel()
		lock := validLock()
		lock.SchemaVersion = 0
		if err := ValidateLock(lock); err == nil {
			t.Error("ValidateLock without schema_version succeeded, want error")
		}
	})
	t.Run("lock future schema_version rejected", func(t *testing.T) {
		t.Parallel()
		lock := validLock()
		lock.SchemaVersion = StateVersion + 1
		if err := ValidateLock(lock); err == nil {
			t.Error("ValidateLock with future schema_version succeeded, want error")
		}
	})
}

func TestSchemaRefIgnoredOnLoad(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 64)
	digest := "sha256:" + commit
	t.Run("state loads with any $schema value", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{StateFileName: {Data: []byte(
			"version: 1\nschema_version: 1\n$schema: ./elsewhere/custom.json\n" +
				"template: oci://ghcr.io/acme/templates/web@1.2.0\nsourceKind: registry\n" +
				"registry: oci://ghcr.io/acme/templates\ntemplateURI: oci://ghcr.io/acme/web:1.2.0\n" +
				"templateDigest: " + digest + "\nrequestedRef: 1.2.0\n" +
				"resolvedCommit: " + commit + "\nengine: turutan/0.1.0\ntemplateLicense: MIT-0\n")}}
		state, err := LoadState(fsys)
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.SchemaVersion != StateVersion {
			t.Errorf("SchemaVersion = %d, want %d", state.SchemaVersion, StateVersion)
		}
	})
	t.Run("lock loads with any $schema value", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{LockFileName: {Data: []byte(
			`{"schema_version":1,"$schema":"./elsewhere/custom.json",` +
				`"source":"s","resolved_sha":"` + commit + `",` +
				`"manifest_hash":"` + digest + `","files":[]}`)}}
		lock, err := LoadLock(fsys)
		if err != nil {
			t.Fatalf("LoadLock error: %v", err)
		}
		if lock.SchemaVersion != StateVersion {
			t.Errorf("SchemaVersion = %d, want %d", lock.SchemaVersion, StateVersion)
		}
	})
}

func structTagNames(t *testing.T, value any, tag string) (present map[string]bool, required []string) {
	t.Helper()
	kind := reflect.TypeOf(value)
	present = map[string]bool{}
	for i := range kind.NumField() {
		field := kind.Field(i)
		if field.PkgPath != "" {
			continue
		}
		raw, ok := field.Tag.Lookup(tag)
		if !ok {
			continue
		}
		parts := strings.Split(raw, ",")
		if parts[0] == "" || parts[0] == "-" {
			continue
		}
		present[parts[0]] = true
		if !slices.Contains(parts[1:], "omitempty") {
			required = append(required, parts[0])
		}
	}
	slices.Sort(required)
	return present, required
}

func schemaProperties(t *testing.T, schema map[string]any) (map[string]any, []string) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema has no properties: %v", schema)
	}
	var required []string
	switch raw := schema["required"].(type) {
	case nil:
	case []string:
		required = append([]string(nil), raw...)
	case []any:
		for _, entry := range raw {
			name, ok := entry.(string)
			if !ok {
				t.Fatalf("required entry is not a string: %v", entry)
			}
			required = append(required, name)
		}
	default:
		t.Fatalf("required has unexpected type %T: %v", schema["required"], schema["required"])
	}
	slices.Sort(required)
	return properties, required
}

func TestGeneratedSchemasTrackStructs(t *testing.T) {
	t.Parallel()
	t.Run("state schema tracks State fields", func(t *testing.T) {
		t.Parallel()
		present, required := structTagNames(t, State{}, "yaml")
		properties, schemaRequired := schemaProperties(t, StateJSONSchema())
		for name := range present {
			if _, ok := properties[name]; !ok {
				t.Errorf("state schema misses State field %q", name)
			}
		}
		for name := range properties {
			if name == "data" {
				continue
			}
			if !present[name] {
				t.Errorf("state schema has stale property %q", name)
			}
		}
		if !slices.Equal(schemaRequired, required) {
			t.Errorf("state schema required = %v, want %v", schemaRequired, required)
		}
	})
	t.Run("lock schema tracks Lock fields", func(t *testing.T) {
		t.Parallel()
		present, required := structTagNames(t, Lock{}, "json")
		properties, schemaRequired := schemaProperties(t, LockJSONSchema())
		for name := range present {
			if _, ok := properties[name]; !ok {
				t.Errorf("lock schema misses Lock field %q", name)
			}
		}
		for name := range properties {
			if !present[name] {
				t.Errorf("lock schema has stale property %q", name)
			}
		}
		if !slices.Equal(schemaRequired, required) {
			t.Errorf("lock schema required = %v, want %v", schemaRequired, required)
		}
	})
}

func TestStateDataJSONSchema(t *testing.T) {
	t.Parallel()
	manifest := &Manifest{Inputs: map[string]ManifestInput{
		"project_name": {Label: "Project Name", Type: "string", Default: "demo"},
		"color": {
			Label: "Color", Type: "string",
			Options: []string{"red", "blue"}, Default: "red",
		},
		"profiles": {
			Label: "Profiles", Type: "array",
			Options: []string{"go", "ai"}, Default: []any{"go"},
			Help: "Feature sets",
		},
		"verbose":  {Label: "Verbose", Type: "bool"},
		"nickname": {Label: "Nickname", Type: "string", If: `{{contains .profiles "go"}}`},
	}}
	schema := StateDataJSONSchema(manifest)
	properties, required := schemaProperties(t, schema)
	color, ok := properties["color"].(map[string]any)
	if !ok || color["type"] != "string" {
		t.Fatalf("color property = %v, want string type", properties["color"])
	}
	enum, ok := color["enum"].([]string)
	if !ok || !slices.Equal(enum, []string{"red", "blue"}) {
		t.Errorf("color enum = %v, want [red blue]", color["enum"])
	}
	profiles, ok := properties["profiles"].(map[string]any)
	if !ok || profiles["type"] != "array" {
		t.Fatalf("profiles property = %v, want array type", properties["profiles"])
	}
	items, ok := profiles["items"].(map[string]any)
	if !ok || items["type"] != "string" {
		t.Fatalf("profiles items = %v, want string items", profiles["items"])
	}
	if _, ok := profiles["enum"]; ok {
		t.Errorf("profiles enum = %v, want options on items for arrays", profiles["enum"])
	}
	if verbose, ok := properties["verbose"].(map[string]any); !ok || verbose["type"] != "boolean" {
		t.Errorf("verbose property = %v, want boolean type", properties["verbose"])
	}
	if !slices.Equal(required, []string{"verbose"}) {
		t.Errorf("required = %v, want [verbose]: only default-free unconditional inputs", required)
	}
	if _, ok := properties["nickname"]; !ok {
		t.Errorf("properties = %v, want conditional nickname listed", properties)
	}
}

func TestStateDataSchemaFollowsInputs(t *testing.T) {
	t.Parallel()
	before := StateDataJSONSchema(&Manifest{Inputs: map[string]ManifestInput{
		"color": {Label: "Color", Type: "string", Options: []string{"red"}, Default: "red"},
	}})
	after := StateDataJSONSchema(&Manifest{Inputs: map[string]ManifestInput{
		"color": {Label: "Color", Type: "string", Options: []string{"red", "blue"}, Default: "red"},
		"size":  {Label: "Size", Type: "string", Default: "m"},
	}})
	beforeRaw, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeRaw) == string(afterRaw) {
		t.Error("data schema unchanged after inputs changed, want regeneration")
	}
	if !strings.Contains(string(afterRaw), `"size"`) || !strings.Contains(string(afterRaw), `"blue"`) {
		t.Errorf("regenerated schema misses new input/option: %s", afterRaw)
	}
}

func TestSaveStateDataSchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := &Manifest{Inputs: map[string]ManifestInput{
		"project_name": {Label: "Project Name", Type: "string", Default: "demo"},
	}}
	if err := SaveStateDataSchema(dir, manifest); err != nil {
		t.Fatalf("SaveStateDataSchema error: %v", err)
	}
	raw := readTestFile(t, dir, StateDataSchemaFileName)
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatalf("state-data schema is not JSON: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("state-data schema has no properties: %v", schema)
	}
	if _, ok := properties["project_name"]; !ok {
		t.Errorf("state-data properties = %v, want project_name", properties)
	}
}
