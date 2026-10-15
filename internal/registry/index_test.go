// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleIndexJSON = `{
  "name": "turutan",
  "version": "1.0.0",
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
      "min-engine": ">= 0.2.0",
      "manifestHash": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "license": "MIT-0"
    },
    {
      "name": "minimal",
      "version": "0.1.0",
      "author": "Jane Doe <jane@example.com>",
      "uri": "fs:/srv/registry/minimal"
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
		{name: "unknown field", input: `{"name":"x","version":"1.0.0","author":"A <a@b.co>","bogus":1}`},
		{name: "yaml input", input: "name: x\nversion: 1.0.0\nauthor: A <a@b.co>\n"},
		{name: "bad version", input: `{"name":"x","version":"nope","author":"A <a@b.co>"}`},
		{name: "bad author", input: `{"name":"x","version":"1.0.0","author":""}`},
		{
			name: "duplicate template",
			input: `{"name":"x","version":"1.0.0","author":"A <a@b.co>","templates":[` +
				`{"name":"dup","version":"1.0.0","author":"A <a@b.co>","uri":"fs:/a"},` +
				`{"name":"dup","version":"1.0.0","author":"A <a@b.co>","uri":"fs:/b"}]}`,
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
	if IndexFileName != "registry.json" {
		t.Errorf("IndexFileName = %q, want registry.json", IndexFileName)
	}
}

func TestIndexRelativeFSResolution(t *testing.T) {
	t.Parallel()
	index, err := ParseIndex([]byte(
		`{"name":"local","version":"1.0.0","author":"A <a@b.co>","templates":[` +
			`{"name":"app","version":"1.0.0","author":"A <a@b.co>","uri":"templates/app/releases/0.1.0.tar.gz"}]}`))
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
	if DefaultRegistryName != "turutan" {
		t.Errorf("DefaultRegistryName = %q", DefaultRegistryName)
	}
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
	index := &RegistryIndex{Name: "r", Version: "1.0.0", Author: author}
	UpsertIndexRelease(index, TemplateManifest{Name: "app", Version: "1.0.0", Author: author, URI: uri})
	uri2, _ := ParseURI("templates/app/releases/1.1.0.tar.gz")
	UpsertIndexRelease(index, TemplateManifest{Name: "app", Version: "1.1.0", Author: author, URI: uri2})
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
}
