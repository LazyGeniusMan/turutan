// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleIndexJSON = `{
  "schema_version": 1,
  "$schema": ".config/turutan/schemas/registry-index.schema.json",
  "engine_version": "dev",
  "name": "turutan",
  "engine_version": "0.1.0",
  "author": "Turutan Maintainers <maintainers@example.com> (https://github.com/LazyGeniusMan/turutan)",
  "templates": [
    {
      "name": "default",
      "version": "1.2.0",
      "author": "Jane Doe <jane@example.com> (https://example.com)",
      "contributors": ["John Doe <john@example.com>"],
      "uri": "oci://ghcr.io/lazygeniusman/turutan/templates/default:1.2.0",
      "releases": ["1.2.0", "1.1.0", "1.0.0"],
      "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "engine_version": "0.2.0",
      "manifestHash": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "license": "MIT-0"
    },
    {
      "name": "minimal",
      "version": "0.1.0",
      "author": "Jane Doe <jane@example.com>",
      "uri": "fs:/srv/registry/minimal",
      "releases": ["0.1.0"],
      "engine_version": "0.1.0"
    }
  ]
}`

func TestParseIndexExample(t *testing.T) {
	t.Parallel()
	index, err := ParseIndex([]byte(exampleIndexJSON))
	if err != nil {
		t.Fatalf("ParseIndex error: %v", err)
	}
	if index.Name != "turutan" {
		t.Errorf("Name = %q, want turutan", index.Name)
	}
	if len(index.Templates) != 2 {
		t.Fatalf("Templates = %d, want 2", len(index.Templates))
	}
	found, err := index.FindTemplate("default")
	if err != nil {
		t.Fatalf("FindTemplate error: %v", err)
	}
	if found.Version != "1.2.0" {
		t.Errorf("Version = %q, want 1.2.0", found.Version)
	}
	if len(found.Contributors) != 1 || found.Contributors[0].Name != "John Doe" {
		t.Errorf("Contributors = %+v, want John Doe", found.Contributors)
	}
	if len(found.Releases) != 3 || found.Releases[0] != "1.2.0" {
		t.Errorf("Releases = %v, want newest-first history", found.Releases)
	}
	if _, err := index.FindTemplate("missing"); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("FindTemplate error = %v, want ErrTemplateNotFound", err)
	}
}

func TestParseIndexRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "unknown field", input: `{"schema_version":1,"engine_version":"dev","name":"x","author":"A <a@b.co>","templates":[],"bogus":1}`},
		{name: "yaml input", input: "name: x\nauthor: A <a@b.co>\n"},
		{name: "bad version", input: `{"name":"x","version":"nope","author":"A <a@b.co>"}`},
		{name: "bad author", input: `{"schema_version":1,"engine_version":"dev","name":"x","author":"","templates":[]}`},
		{
			name: "duplicate template",
			input: `{"schema_version":1,"name":"x","engine_version":"dev","author":"A <a@b.co>","templates":[` +
				`{"name":"dup","version":"1.0.0","author":"A <a@b.co>","uri":"fs:/a","engine_version":"dev","releases":["1.0.0"]},` +
				`{"name":"dup","version":"1.0.0","author":"A <a@b.co>","uri":"fs:/b","engine_version":"dev","releases":["1.0.0"]}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseIndex([]byte(tt.input)); err == nil {
				t.Errorf("ParseIndex succeeded, want error for %s", tt.name)
			}
		})
	}
}

func TestIndexRoundTrip(t *testing.T) {
	t.Parallel()
	index, err := ParseIndex([]byte(exampleIndexJSON))
	if err != nil {
		t.Fatalf("ParseIndex error: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, IndexFileName)
	if err := SaveIndexFile(path, index); err != nil {
		t.Fatalf("SaveIndexFile error: %v", err)
	}
	loaded, err := LoadIndexFile(path)
	if err != nil {
		t.Fatalf("LoadIndexFile error: %v", err)
	}
	if len(loaded.Templates) != len(index.Templates) {
		t.Fatalf("Templates = %d, want %d", len(loaded.Templates), len(index.Templates))
	}
	for i := range loaded.Templates {
		if loaded.Templates[i].Name != index.Templates[i].Name ||
			loaded.Templates[i].Version != index.Templates[i].Version ||
			loaded.Templates[i].URI.String() != index.Templates[i].URI.String() {
			t.Errorf("Templates[%d] = %+v, want %+v", i, loaded.Templates[i], index.Templates[i])
		}
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the explicit caller-supplied index file
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	reparsed, err := ParseIndex(data)
	if err != nil {
		t.Fatalf("reparse error: %v", err)
	}
	if reparsed.Templates[0].Author.String() != "Jane Doe <jane@example.com> (https://example.com)" {
		t.Errorf("author = %q", reparsed.Templates[0].Author.String())
	}
	if !strings.Contains(string(data), "manifestHash") {
		t.Errorf("saved index drops manifestHash")
	}
	if IndexFileName != "turutan.json" {
		t.Errorf("IndexFileName = %q, want turutan.json", IndexFileName)
	}
}

func TestIndexRelativeFSResolution(t *testing.T) {
	t.Parallel()
	index, err := ParseIndex([]byte(
		`{"schema_version":1,"name":"local","engine_version":"dev","author":"A <a@b.co>","templates":[` +
			`{"name":"app","version":"0.1.0","author":"A <a@b.co>","uri":"templates/app/releases/0.1.0.tar.gz","releases":["0.1.0"],"engine_version":"dev"}]}`))
	if err != nil {
		t.Fatalf("ParseIndex error: %v", err)
	}
	resolved, err := index.Templates[0].URI.ResolveRelativeTo(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRelativeTo error: %v", err)
	}
	if resolved.IsRelative() {
		t.Errorf("resolved URI %q is still relative", resolved.String())
	}
}

func TestDefaultConstants(t *testing.T) {
	t.Parallel()
	uri, err := ParseURI(DefaultRegistryURI)
	if err != nil {
		t.Fatalf("DefaultRegistryURI invalid: %v", err)
	}
	if uri.Host != "ghcr.io" || uri.Path != "lazygeniusman/turutan/templates" {
		t.Errorf("DefaultRegistryURI = %q", DefaultRegistryURI)
	}
	ref, err := ParseURI(DefaultTemplateRef("1.2.0"))
	if err != nil {
		t.Fatalf("DefaultTemplateRef invalid: %v", err)
	}
	if ref.String() != "oci://ghcr.io/lazygeniusman/turutan/templates/default:1.2.0" {
		t.Errorf("DefaultTemplateRef = %q", ref.String())
	}
}

func TestUpsertIndexReleaseHistory(t *testing.T) {
	t.Parallel()
	author, _ := ParseAuthor("A <a@b.co>")
	uri, _ := ParseURI("templates/app/releases/1.0.0.tar.gz")
	index := &RegistryIndex{Name: "r", Author: author, EngineVersion: "dev", Templates: []RegistryTemplate{}}
	UpsertIndexRelease(index, TemplateManifest{Name: "app", Version: "1.0.0", Author: author, URI: uri, EngineVersion: "dev"})
	uri2, _ := ParseURI("templates/app/releases/1.1.0.tar.gz")
	UpsertIndexRelease(index, TemplateManifest{Name: "app", Version: "1.1.0", Author: author, URI: uri2, EngineVersion: "dev"})
	found, err := index.FindTemplate("app")
	if err != nil {
		t.Fatal(err)
	}
	if found.Version != "1.1.0" {
		t.Errorf("Version = %q, want 1.1.0", found.Version)
	}
	if len(found.Releases) != 2 || found.Releases[0] != "1.1.0" || found.Releases[1] != "1.0.0" {
		t.Errorf("Releases = %v, want [1.1.0 1.0.0]", found.Releases)
	}
	manifest := found.Manifest()
	if manifest.Version != "1.1.0" || len(manifest.URI.String()) == 0 {
		t.Errorf("Manifest() = %+v", manifest)
	}
	if err := manifest.Validate(); err != nil {
		t.Errorf("Manifest().Validate error: %v", err)
	}
}

func TestRegistryTemplateUnreleased(t *testing.T) {
	t.Parallel()
	author, err := ParseAuthor("A <a@b.co>")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseURI("templates/app/releases/0.1.0.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		entry   RegistryTemplate
		wantErr bool
	}{
		{
			name:    "created entry with name and author only",
			entry:   RegistryTemplate{Name: "app", Author: author, EngineVersion: "dev"},
			wantErr: false,
		},
		{
			name:    "missing engine rejected",
			entry:   RegistryTemplate{Name: "app", Author: author},
			wantErr: true,
		},
		{
			name:    "bad name",
			entry:   RegistryTemplate{Name: "Bad Name!", Author: author, EngineVersion: "dev"},
			wantErr: true,
		},
		{
			name:    "empty author",
			entry:   RegistryTemplate{Name: "app"},
			wantErr: true,
		},
		{
			name:    "version without releases is a fake draft",
			entry:   RegistryTemplate{Name: "app", Author: author, Version: "0.1.0", EngineVersion: "dev"},
			wantErr: true,
		},
		{
			name:    "uri without releases is a fake draft",
			entry:   RegistryTemplate{Name: "app", Author: author, URI: &snapshot, EngineVersion: "dev"},
			wantErr: true,
		},
		{
			name:    "digest without releases is a fake draft",
			entry:   RegistryTemplate{Name: "app", Author: author, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EngineVersion: "dev"},
			wantErr: true,
		},
		{
			name:    "manifest hash without releases is a fake draft",
			entry:   RegistryTemplate{Name: "app", Author: author, ManifestHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", EngineVersion: "dev"},
			wantErr: true,
		},
		{
			name:    "released entry without uri",
			entry:   RegistryTemplate{Name: "app", Author: author, Version: "0.1.0", Releases: []string{"0.1.0"}, EngineVersion: "dev"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := tt.entry
			err := entry.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Validate succeeded, want error for %s", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate error = %v, want nil", err)
			}
			if !tt.wantErr && entry.IsReleased() {
				t.Errorf("IsReleased = true, want false for %s", tt.name)
			}
		})
	}
}

func TestUnreleasedEntryRoundTripOmitsReleaseFields(t *testing.T) {
	t.Parallel()
	author, err := ParseAuthor("A <a@b.co>")
	if err != nil {
		t.Fatal(err)
	}
	index := &RegistryIndex{
		Name: "r", Author: author, EngineVersion: "dev",
		Templates: []RegistryTemplate{{Name: "app", Author: author, EngineVersion: "dev"}},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, IndexFileName)
	if err := SaveIndexFile(path, index); err != nil {
		t.Fatalf("SaveIndexFile error: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the explicit caller-supplied index file
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Templates []map[string]any `json:"templates"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(raw.Templates))
	}
	for key := range raw.Templates[0] {
		switch key {
		case "name", "author", "engine_version":
		default:
			t.Errorf("saved unreleased entry carries %q: %s", key, data)
		}
	}
	loaded, err := LoadIndexFile(path)
	if err != nil {
		t.Fatalf("LoadIndexFile error: %v", err)
	}
	found, err := loaded.FindTemplate("app")
	if err != nil {
		t.Fatal(err)
	}
	if found.IsReleased() || found.URI != nil || found.Version != "" {
		t.Errorf("reloaded entry = %+v, want created-but-unreleased", found)
	}
}

func TestReleaseSnapshotArchivePath(t *testing.T) {
	t.Parallel()
	got := ReleaseSnapshotPath("default", "0.1.0")
	if got != "templates/default/releases/0.1.0.tar.gz" {
		t.Errorf("ReleaseSnapshotPath = %q", got)
	}
	uri, err := ParseURI(got)
	if err != nil {
		t.Fatal(err)
	}
	if !uri.IsRelative() {
		t.Errorf("snapshot URI %q should stay relative", uri.String())
	}
	if want := "fs://" + got; uri.String() != want {
		t.Errorf("snapshot URI.String() = %q, want %q", uri.String(), want)
	}
}
