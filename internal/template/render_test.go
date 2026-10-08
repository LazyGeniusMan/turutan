// SPDX-License-Identifier: Apache-2.0

package template

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// renderFixture is a minimal template tree exercising suffix stripping,
// verbatim copies, sprig functions and metadata skipping.
func renderFixture() fstest.MapFS {
	return fstest.MapFS{
		"go.mod.tmpl":              {Data: []byte("module {{.project_name}}\n\ngo 1.27\n")},
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
