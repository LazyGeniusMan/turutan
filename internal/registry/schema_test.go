// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func readSchemaFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is built from t.TempDir() plus fixed schema names in tests
	if err != nil {
		t.Fatalf("reading schema %s: %v", path, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema %s is not JSON: %v", path, err)
	}
	return schema
}

func TestSaveIndexFileStampsVersionAndSchema(t *testing.T) {
	t.Parallel()
	author, err := ParseAuthor("A <a@b.co>")
	if err != nil {
		t.Fatal(err)
	}
	index := &RegistryIndex{Name: "r", Version: "1.0.0", Author: author}
	dir := t.TempDir()
	path := filepath.Join(dir, IndexFileName)
	if err := SaveIndexFile(path, index); err != nil {
		t.Fatalf("SaveIndexFile error: %v", err)
	}
	if index.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d after save", index.SchemaVersion, SchemaVersion)
	}
	if index.Schema != IndexSchemaRef {
		t.Errorf("Schema = %q, want %q", index.Schema, IndexSchemaRef)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the explicit caller-supplied index file
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"schema_version": 1`) {
		t.Errorf("index misses schema_version 1:\n%s", data)
	}
	if !strings.Contains(string(data), `"$schema": "./schemas/registry-index.schema.json"`) {
		t.Errorf("index misses its $schema reference:\n%s", data)
	}
	schema := readSchemaFile(t, filepath.Join(dir, "schemas", "registry-index.schema.json"))
	if _, ok := schema["properties"]; !ok {
		t.Errorf("index schema has no properties: %v", schema)
	}
	loaded, err := LoadIndexFile(path)
	if err != nil {
		t.Fatalf("LoadIndexFile error: %v", err)
	}
	if loaded.SchemaVersion != SchemaVersion {
		t.Errorf("loaded SchemaVersion = %d, want %d", loaded.SchemaVersion, SchemaVersion)
	}
}

func TestIndexLoadValidatesSchemaVersion(t *testing.T) {
	t.Parallel()
	t.Run("missing schema_version rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseIndex([]byte(`{"name":"x","version":"1.0.0","author":"A <a@b.co>"}`))
		if err == nil {
			t.Error("ParseIndex without schema_version succeeded, want error")
		}
	})
	t.Run("future schema_version rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseIndex([]byte(`{"schema_version":2,"name":"x","version":"1.0.0","author":"A <a@b.co>"}`))
		if err == nil {
			t.Error("ParseIndex with schema_version 2 succeeded, want error")
		}
	})
	t.Run("foreign $schema value loads", func(t *testing.T) {
		t.Parallel()
		index, err := ParseIndex([]byte(
			`{"schema_version":1,"$schema":"./elsewhere/custom.json",` +
				`"name":"x","version":"1.0.0","author":"A <a@b.co>"}`))
		if err != nil {
			t.Fatalf("ParseIndex error: %v", err)
		}
		if index.SchemaVersion != SchemaVersion {
			t.Errorf("SchemaVersion = %d, want %d", index.SchemaVersion, SchemaVersion)
		}
	})
}

func TestSaveVersionedManifestSchemaRef(t *testing.T) {
	t.Parallel()
	author, err := ParseAuthor("A <a@b.co>")
	if err != nil {
		t.Fatal(err)
	}
	uri, err := ParseURI("templates/app/releases/0.1.0.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	newManifest := func(name string) *TemplateManifest {
		return &TemplateManifest{
			Name: name, Version: "0.1.0", Author: author, URI: uri,
		}
	}
	t.Run("flat name references schemas relatively", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		manifest := newManifest("app")
		path, err := SaveVersionedManifest(root, manifest)
		if err != nil {
			t.Fatalf("SaveVersionedManifest error: %v", err)
		}
		if want := "../../../schemas/template-manifest.schema.json"; manifest.Schema != want {
			t.Errorf("Schema = %q, want %q", manifest.Schema, want)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- path is the SaveVersionedManifest result under t.TempDir()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"schema_version": 1`) {
			t.Errorf("manifest misses schema_version 1:\n%s", data)
		}
		loaded, err := LoadVersionedManifest(root, "app", "0.1.0")
		if err != nil {
			t.Fatalf("LoadVersionedManifest error: %v", err)
		}
		if loaded.SchemaVersion != SchemaVersion {
			t.Errorf("loaded SchemaVersion = %d, want %d", loaded.SchemaVersion, SchemaVersion)
		}
		schema := readSchemaFile(t, filepath.Join(root, "schemas", "template-manifest.schema.json"))
		if _, ok := schema["properties"]; !ok {
			t.Errorf("manifest schema has no properties: %v", schema)
		}
	})
	t.Run("nested name climbs further", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		manifest := newManifest("group/app")
		if _, err := SaveVersionedManifest(root, manifest); err != nil {
			t.Fatalf("SaveVersionedManifest error: %v", err)
		}
		if want := "../../../../schemas/template-manifest.schema.json"; manifest.Schema != want {
			t.Errorf("Schema = %q, want %q", manifest.Schema, want)
		}
		if _, err := LoadVersionedManifest(root, "group/app", "0.1.0"); err != nil {
			t.Fatalf("LoadVersionedManifest error: %v", err)
		}
	})
	t.Run("missing schema_version rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ParseVersionedManifest([]byte(
			`{"name":"app","version":"0.1.0","author":"A <a@b.co>","uri":"templates/app/releases/0.1.0.tar.gz"}`))
		if err == nil {
			t.Error("ParseVersionedManifest without schema_version succeeded, want error")
		}
	})
}

func TestGeneratedRegistrySchemasTrackStructs(t *testing.T) {
	t.Parallel()
	jsonTagNames := func(value any) (present map[string]bool, required []string) {
		kind := reflect.TypeOf(value)
		present = map[string]bool{}
		for i := range kind.NumField() {
			field := kind.Field(i)
			if field.PkgPath != "" {
				continue
			}
			raw, ok := field.Tag.Lookup("json")
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
	check := func(t *testing.T, value any, schema map[string]any) {
		t.Helper()
		present, required := jsonTagNames(value)
		raw, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("schema has no properties: %v", schema)
		}
		for name := range present {
			if _, ok := raw[name]; !ok {
				t.Errorf("schema misses field %q", name)
			}
		}
		for name := range raw {
			if !present[name] {
				t.Errorf("schema has stale property %q", name)
			}
		}
		var schemaRequired []string
		if entries, ok := schema["required"].([]string); ok {
			schemaRequired = append([]string(nil), entries...)
			slices.Sort(schemaRequired)
		} else if schema["required"] != nil {
			t.Fatalf("required is not a string list: %v", schema["required"])
		}
		if !slices.Equal(schemaRequired, required) {
			t.Errorf("required = %v, want %v", schemaRequired, required)
		}
	}
	t.Run("index schema tracks RegistryIndex", func(t *testing.T) {
		t.Parallel()
		check(t, RegistryIndex{}, IndexJSONSchema())
	})
	t.Run("manifest schema tracks TemplateManifest", func(t *testing.T) {
		t.Parallel()
		check(t, TemplateManifest{}, ManifestJSONSchema())
	})
}
