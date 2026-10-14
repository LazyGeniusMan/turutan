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
		".config/turutan/skip.yml": {Data: []byte("must not render\n")},
		".git/config":              {Data: []byte("must not render\n")},
	}
}

func TestRenderFS(t *testing.T) {
	t.Run("renders tmpl strips suffix skips metadata", func(t *testing.T) {
		dst := t.TempDir()
		data := map[string]any{"project_name": "demo"}
		if err := RenderFS(renderFixture(), dst, data); err != nil {
			t.Fatalf("RenderFS error: %v", err)
		}
		for _, path := range []string{"go.mod", "internal/app/app.go", "README.md", "static.txt"} {
			if _, err := os.Stat(filepath.Join(dst, path)); err != nil {
				t.Errorf("expected rendered file %q: %v", path, err)
			}
		}
		for _, path := range []string{"go.mod.tmpl", ".config/turutan/skip.yml", ".git/config"} {
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
			t.Error("RenderFS with missing data succeeded, want error")
		}
	})
}

func TestRenderFSSkipsNestedMetadataDirs(t *testing.T) {
	tests := []struct {
		name    string
		skipped string
	}{
		{name: "state dir skipped", skipped: ".config/turutan/state.json"},
		{name: "nested git tree", skipped: "a/.git/config"},
		{name: "base store skipped", skipped: ".config/turutan/templates/abc/f.txt"},
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
		{name: "state dir skipped", skipped: ".config/turutan/state.json"},
		{name: "nested git tree", skipped: "a/.git/config"},
		{name: "base store skipped", skipped: ".config/turutan/templates/abc/f.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			src := t.TempDir()
			skippedPath := filepath.Join(src, filepath.FromSlash(tt.skipped))
			is.NoError(os.MkdirAll(filepath.Dir(skippedPath), 0o755))
			is.NoError(os.WriteFile(skippedPath, []byte("must not render\n"), 0o600))
			is.NoError(os.MkdirAll(filepath.Join(src, "a"), 0o755))
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

func TestContainsFunc(t *testing.T) {
	t.Run("sprig string order preserved", func(t *testing.T) {
		is := assert.New(t)
		is.True(containsFunc("cat", "fair catch"))
		is.False(containsFunc("catch", "cat"))
		is.True(containsFunc("world", "hello world"))
		is.False(containsFunc("bye", "hello"))
	})
	t.Run("slices accept either order", func(t *testing.T) {
		is := assert.New(t)
		is.True(containsFunc([]string{"go", "ai"}, "go"))
		is.True(containsFunc("go", []string{"go", "ai"}))
		is.False(containsFunc([]string{"go", "ai"}, "rust"))
		is.False(containsFunc("rust", []string{"go", "ai"}))
		is.True(containsFunc([]any{"go", "ai"}, "ai"))
		is.True(containsFunc("ai", []any{"go", "ai"}))
		is.False(containsFunc(nil, "go"))
		is.False(containsFunc("go", nil))
	})
}

func TestEvaluateSkip(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]any
		conds   []Conditional
		want    map[string]bool
		wantErr string
	}{
		{name: "empty conds empty set", data: map[string]any{}, conds: nil, want: map[string]bool{}},
		{name: "truthy adds skips", data: map[string]any{"profiles": []string{"ai"}}, conds: []Conditional{{If: `{{ not (contains .profiles "go") }}`, Skip: []string{"go.mod"}}}, want: map[string]bool{"go.mod": true}},
		{name: "falsy adds nothing", data: map[string]any{"profiles": []string{"go"}}, conds: []Conditional{{If: `{{ not (contains .profiles "go") }}`, Skip: []string{"go.mod"}}}, want: map[string]bool{}},
		{name: "any slice handled", data: map[string]any{"profiles": []any{"go"}}, conds: []Conditional{{If: `{{ contains .profiles "go" }}`, Skip: []string{"x"}}}, want: map[string]bool{"x": true}},
		{name: "yes truthy", data: map[string]any{}, conds: []Conditional{{If: "yes", Skip: []string{"a"}}}, want: map[string]bool{"a": true}},
		{name: "empty falsy", data: map[string]any{}, conds: []Conditional{{If: "", Skip: []string{"a"}}}, want: map[string]bool{}},
		{name: "non-bool errors", data: map[string]any{}, conds: []Conditional{{If: "maybe", Skip: []string{"a"}}}, wantErr: "not a boolean"},
		{name: "missing key errors", data: map[string]any{}, conds: []Conditional{{If: `{{ .absent_key }}`, Skip: []string{"a"}}}, wantErr: `no entry for key "absent_key"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			got, err := EvaluateSkip(tt.data, tt.conds)
			if tt.wantErr != "" {
				is.Error(err)
				is.Contains(err.Error(), tt.wantErr)
				if key := parseMissingKey(err); tt.name == "missing key errors" {
					is.Equal("absent_key", key)
				}
				return
			}
			is.NoError(err)
			is.Equal(tt.want, got)
		})
	}
}

func TestRenderFSWithConditionals(t *testing.T) {
	newFixture := func() fstest.MapFS {
		return fstest.MapFS{
			"go.mod.tmpl":     {Data: []byte("module {{.project_name}}\n")},
			"github/ci.yml":   {Data: []byte("ci\n")},
			"cmd/app/main.go": {Data: []byte("main\n")},
			"keep.txt":        {Data: []byte("keep\n")},
		}
	}
	goCond := []Conditional{{If: `{{ not (contains .profiles "go") }}`, Skip: []string{"go.mod", "cmd/**"}}}
	t.Run("skips go files when profile absent", func(t *testing.T) {
		is := assert.New(t)
		dst := t.TempDir()
		data := map[string]any{"project_name": "demo", "profiles": []string{"ai"}}
		is.NoError(RenderFSWithConditionals(newFixture(), dst, data, goCond, nil))
		_, err := os.Stat(filepath.Join(dst, "go.mod"))
		is.True(os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(dst, "cmd", "app", "main.go"))
		is.True(os.IsNotExist(err))
		got, err := os.ReadFile(filepath.Join(dst, "keep.txt")) // #nosec G304 -- reads file from this test's temp dir
		is.NoError(err)
		is.Equal("keep\n", string(got))
	})
	t.Run("keeps go files when profile present", func(t *testing.T) {
		is := assert.New(t)
		dst := t.TempDir()
		data := map[string]any{"project_name": "demo", "profiles": []string{"go"}}
		is.NoError(RenderFSWithConditionals(newFixture(), dst, data, goCond, nil))
		got, err := os.ReadFile(filepath.Join(dst, "go.mod")) // #nosec G304 -- reads file from this test's temp dir
		is.NoError(err)
		is.Equal("module demo\n", string(got))
	})
	t.Run("tmpl suffix matches stripped path", func(t *testing.T) {
		is := assert.New(t)
		dst := t.TempDir()
		data := map[string]any{"project_name": "demo", "profiles": []string{}}
		conds := []Conditional{{If: "true", Skip: []string{"go.mod"}}}
		is.NoError(RenderFSWithConditionals(newFixture(), dst, data, conds, nil))
		_, err := os.Stat(filepath.Join(dst, "go.mod"))
		is.True(os.IsNotExist(err))
	})
	t.Run("dir skip prunes tree", func(t *testing.T) {
		is := assert.New(t)
		dst := t.TempDir()
		data := map[string]any{"project_name": "demo"}
		conds := []Conditional{{If: "true", Skip: []string{"github/**"}}}
		is.NoError(RenderFSWithConditionals(newFixture(), dst, data, conds, nil))
		_, err := os.Stat(filepath.Join(dst, "github", "ci.yml"))
		is.True(os.IsNotExist(err))
	})
	t.Run("missing key propagates for input", func(t *testing.T) {
		conds := []Conditional{{If: `{{ .absent_key }}`, Skip: []string{"a"}}}
		err := RenderFSWithConditionals(newFixture(), t.TempDir(), map[string]any{}, conds, nil)
		is := assert.New(t)
		is.Error(err)
		is.Contains(err.Error(), `no entry for key "absent_key"`)
	})
}

func TestRenderDirWithConditionals(t *testing.T) {
	t.Run("skips dir entries via stripped match", func(t *testing.T) {
		is := assert.New(t)
		src := t.TempDir()
		is.NoError(os.MkdirAll(filepath.Join(src, "cmd", "app"), 0o750))
		is.NoError(os.WriteFile(filepath.Join(src, "go.mod.tmpl"), []byte("module {{.project_name}}\n"), 0o600))
		is.NoError(os.WriteFile(filepath.Join(src, "cmd", "app", "main.go"), []byte("main\n"), 0o600))
		dst := t.TempDir()
		data := map[string]any{"project_name": "demo", "profiles": []string{}}
		conds := []Conditional{{If: "true", Skip: []string{"go.mod", "cmd/**"}}}
		is.NoError(RenderDirWithConditionals(src, dst, data, conds, nil))
		_, err := os.Stat(filepath.Join(dst, "go.mod"))
		is.True(os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(dst, "cmd", "app", "main.go"))
		is.True(os.IsNotExist(err))
	})
}
