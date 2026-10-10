// SPDX-License-Identifier: Apache-2.0

package template

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func renderFixture() fstest.MapFS {
	return fstest.MapFS{
		"go.mod.tmpl":              {Data: []byte("module {{.project_name}}\n\ngo 1.26\n")},
		"internal/app/app.go.tmpl": {Data: []byte("// Package app implements the {{.project_name}} application logic.\npackage app\n\n// Greet returns the project greeting.\nfunc Greet() string {\n\treturn \"{{.project_name}}\"\n}\n")},
		"README.md.tmpl":           {Data: []byte("# {{.project_name | upper}}\n")},
		"static.txt":               {Data: []byte("verbatim {{not_a_template}}\n")},
		".turutan/skip.yml":        {Data: []byte("must not render\n")},
		".git/config":              {Data: []byte("must not render\n")},
	}
}

func TestRenderFS(t *testing.T) {
	t.Run("renders tmpl strips suffix skips metadata", func(t *testing.T) {
		dst := t.TempDir()
		answers := map[string]any{"project_name": "demo"}
		if err := RenderFS(renderFixture(), dst, answers); err != nil {
			t.Fatalf("RenderFS error: %v", err)
		}
		for _, path := range []string{"go.mod", "internal/app/app.go", "README.md", "static.txt"} {
			if _, err := os.Stat(filepath.Join(dst, path)); err != nil {
				t.Errorf("expected rendered file %q: %v", path, err)
			}
		}
		for _, path := range []string{"go.mod.tmpl", ".turutan/skip.yml", ".git/config"} {
			if _, err := os.Stat(filepath.Join(dst, path)); err == nil {
				t.Errorf("file %q should not be rendered", path)
			}
		}
		got, err := os.ReadFile(filepath.Join(dst, "static.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "verbatim {{not_a_template}}\n" {
			t.Errorf("plain file was templated: %q", got)
		}
	})
	t.Run("go.mod matches golden", func(t *testing.T) {
		dst := t.TempDir()
		if err := RenderFS(renderFixture(), dst, map[string]any{"project_name": "demo"}); err != nil {
			t.Fatalf("RenderFS error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile("testdata/golden/go.mod.golden")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("go.mod = %q, want golden %q", got, want)
		}
	})
	t.Run("app.go matches golden", func(t *testing.T) {
		dst := t.TempDir()
		if err := RenderFS(renderFixture(), dst, map[string]any{"project_name": "demo"}); err != nil {
			t.Fatalf("RenderFS error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "internal/app/app.go"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile("testdata/golden/app.go.golden")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("app.go = %q, want golden %q", got, want)
		}
	})
	t.Run("sprig upper applied", func(t *testing.T) {
		dst := t.TempDir()
		if err := RenderFS(renderFixture(), dst, map[string]any{"project_name": "demo"}); err != nil {
			t.Fatalf("RenderFS error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "# DEMO\n" {
			t.Errorf("README.md = %q, want %q", got, "# DEMO\n")
		}
	})
	t.Run("missing key fails", func(t *testing.T) {
		if err := RenderFS(renderFixture(), t.TempDir(), map[string]any{}); err == nil {
			t.Error("RenderFS with missing answers succeeded, want error")
		}
	})
}

func TestRenderFSSkipsNestedMetadataDirs(t *testing.T) {
	tests := []struct {
		name    string
		skipped string
	}{
		{name: "nested turutan tree", skipped: "a/.turutan/b.yml"},
		{name: "nested git tree", skipped: "a/.git/config"},
		{name: "deeply nested turutan file", skipped: "a/b/.turutan/c/d.yml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			src := fstest.MapFS{
				"a/keep.txt": {Data: []byte("keep\n")},
				tt.skipped:   {Data: []byte("must not render\n")},
			}
			dst := t.TempDir()
			is.NoError(RenderFS(src, dst, map[string]any{}))
			_, err := os.Stat(filepath.Join(dst, filepath.FromSlash(tt.skipped)))
			is.True(os.IsNotExist(err), "nested metadata file %q must not render", tt.skipped)
			got, err := os.ReadFile(filepath.Join(dst, "a", "keep.txt"))
			is.NoError(err)
			is.Equal("keep\n", string(got))
		})
	}
}

func TestRenderDirSkipsNestedMetadataDirs(t *testing.T) {
	tests := []struct {
		name    string
		skipped string
	}{
		{name: "nested turutan tree", skipped: "a/.turutan/b.yml"},
		{name: "nested git tree", skipped: "a/.git/config"},
		{name: "deeply nested turutan file", skipped: "a/b/.turutan/c/d.yml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			src := t.TempDir()
			skippedPath := filepath.Join(src, filepath.FromSlash(tt.skipped))
			is.NoError(os.MkdirAll(filepath.Dir(skippedPath), 0o755))
			is.NoError(os.WriteFile(skippedPath, []byte("must not render\n"), 0o600))
			is.NoError(os.WriteFile(filepath.Join(src, "a", "keep.txt"), []byte("keep\n"), 0o600))
			dst := t.TempDir()
			is.NoError(RenderDir(src, dst, map[string]any{}))
			_, err := os.Stat(filepath.Join(dst, filepath.FromSlash(tt.skipped)))
			is.True(os.IsNotExist(err), "nested metadata file %q must not render", tt.skipped)
			got, err := os.ReadFile(filepath.Join(dst, "a", "keep.txt"))
			is.NoError(err)
			is.Equal("keep\n", string(got))
		})
	}
}

func TestSprigEnvBlocked(t *testing.T) {
	t.Setenv("TURUTAN_CANARY_ENV", "leaked-secret")
	for _, content := range []string{
		`{{env "TURUTAN_CANARY_ENV"}}`,
		`{{expandenv "prefix-$TURUTAN_CANARY_ENV"}}`,
	} {
		src := fstest.MapFS{
			"out.txt.tmpl": {Data: []byte(content)},
		}
		dst := t.TempDir()
		err := RenderFS(src, dst, map[string]any{})
		if err == nil {
			t.Errorf("RenderFS with %q succeeded, want env blocked", content)
			continue
		}
		if got, readErr := os.ReadFile(filepath.Join(dst, "out.txt")); readErr == nil {
			if string(got) == "leaked-secret" || string(got) == "prefix-leaked-secret" {
				t.Errorf("RenderFS leaked canary env via %q: %q", content, got)
			}
		}
	}
}

func TestSprigSafeFuncsRetained(t *testing.T) {
	src := fstest.MapFS{
		"out.txt.tmpl": {Data: []byte(`{{.name | upper}}`)},
	}
	dst := t.TempDir()
	if err := RenderFS(src, dst, map[string]any{"name": "demo"}); err != nil {
		t.Fatalf("RenderFS safe func error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "DEMO" {
		t.Errorf("safe sprig func upper = %q, want DEMO", got)
	}
}

func TestRenderDirSymlinks(t *testing.T) {
	t.Run("internal symlink dereferenced", func(t *testing.T) {
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("content\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real.txt", filepath.Join(src, "link.txt")); err != nil {
			t.Fatal(err)
		}
		dst := t.TempDir()
		if err := RenderDir(src, dst, map[string]any{}); err != nil {
			t.Fatalf("RenderDir error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "link.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "content\n" {
			t.Errorf("dereferenced link = %q, want %q", got, "content\n")
		}
	})
	t.Run("escaping symlink refused", func(t *testing.T) {
		src := t.TempDir()
		outside := t.TempDir()
		secret := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(secret, filepath.Join(src, "evil.txt")); err != nil {
			t.Fatal(err)
		}
		if err := RenderDir(src, t.TempDir(), map[string]any{}); err == nil {
			t.Error("RenderDir with escaping symlink succeeded, want refusal")
		}
	})
}
