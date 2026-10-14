// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

func createProject(t *testing.T, dir string, force bool) {
	t.Helper()
	if err := Create(dir, CreateOptions{Force: force}); err != nil {
		t.Fatalf("Create(%q) error: %v", dir, err)
	}
}

func loadCreateManifest(t *testing.T, dir string) *config.Manifest {
	t.Helper()
	manifest, err := config.LoadManifest(os.DirFS(dir))
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	return manifest
}

func TestCreateManifestChecklist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-template")
	createProject(t, dir, false)
	manifest := loadCreateManifest(t, dir)

	if manifest.Source == "" {
		t.Error("manifest is missing source")
	}
	if manifest.MinEngine == "" {
		t.Error("manifest is missing min-engine")
	}
	if manifest.License == "" {
		t.Error("manifest is missing license")
	}
	if len(manifest.LicenseFiles) == 0 {
		t.Error("manifest is missing license-files")
	}
	if len(manifest.DocFiles) == 0 {
		t.Error("manifest is missing doc-files")
	}
	if len(manifest.Includes) == 0 {
		t.Error("manifest is missing includes")
	}
	if len(manifest.Excludes) == 0 {
		t.Error("manifest is missing excludes")
	}
	if len(manifest.Preserves) == 0 {
		t.Error("manifest is missing preserves")
	}
	if manifest.Conflict == "" {
		t.Error("manifest is missing conflict")
	}
	if len(manifest.Hooks.Pre) == 0 || len(manifest.Hooks.Post) == 0 {
		t.Error("manifest is missing hooks.pre/post")
	}
	if len(manifest.Migrations) == 0 || len(manifest.Migrations[0].Run) == 0 {
		t.Error("manifest is missing migrations[{from,run}]")
	}
	for _, key := range []string{"project_name", "project_description", "profiles"} {
		if _, ok := manifest.Inputs[key]; !ok {
			t.Errorf("manifest inputs are missing %q", key)
		}
	}
	seenType := map[string]bool{}
	seenIf := false
	for key, input := range manifest.Inputs {
		seenType[input.Type] = true
		if strings.TrimSpace(input.If) != "" {
			seenIf = true
		}
		if input.Label == "" {
			t.Errorf("input %q is missing label", key)
		}
	}
	for _, want := range []string{"string", "array", "bool"} {
		if !seenType[want] {
			t.Errorf("manifest inputs lack a %q type example", want)
		}
	}
	if !seenIf {
		t.Error("manifest inputs lack a conditional `if` example")
	}
	seenLayer := false
	for _, cond := range manifest.Conditionals {
		if len(cond.Includes) > 0 && len(cond.Excludes) > 0 && len(cond.Preserves) > 0 {
			seenLayer = true
		}
	}
	if !seenLayer {
		t.Error("manifest lacks a conditional with if+includes+excludes+preserves")
	}
	if len(manifest.Renames) == 0 {
		t.Error("manifest is missing renames")
	}

	for _, name := range []string{"LICENSE", "hello.txt", "new-name.txt", "hooks/post-setup.sh", "config/local.conf"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("created project is missing %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".turutan.yml")); err != nil {
		t.Errorf("created project is missing .turutan.yml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Errorf("created project is missing author README.md: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "hooks/post-setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("hooks/post-setup.sh is not executable")
	}
}

func renderCreated(t *testing.T, src string, data map[string]any) string {
	t.Helper()
	manifest := loadCreateManifest(t, src)
	dst := t.TempDir()
	if err := template.RenderDirWithConditionals(
		src, dst, data,
		manifest.Includes, manifest.Excludes, manifest.Preserves,
		manifestConditionals(manifest),
	); err != nil {
		t.Fatalf("RenderDirWithConditionals error: %v", err)
	}
	return dst
}

func readRendered(t *testing.T, dst, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(name))) // #nosec G304 -- test-only rendered path under t.TempDir(), not external input
	if err != nil {
		t.Fatalf("reading rendered %q: %v", name, err)
	}
	return string(data)
}

func assertAbsent(t *testing.T, dst string, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Errorf("rendered tree should not contain %q", name)
		}
	}
}

func assertNoTmplSuffix(t *testing.T, dst string) {
	t.Helper()
	err := filepath.WalkDir(dst, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".tmpl") {
			t.Errorf("rendered tree keeps .tmpl suffix: %q", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateRenderSmoke(t *testing.T) {
	src := filepath.Join(t.TempDir(), "my-template")
	createProject(t, src, false)

	t.Run("all layers active", func(t *testing.T) {
		dst := renderCreated(t, src, map[string]any{
			"project_name":        "demo",
			"project_description": "demo desc",
			"profiles":            []string{"extra"},
			"greeting_style":      "formal",
			"features":            []string{"metrics"},
			"enable_extra":        true,
			"extra_detail":        "shiny",
		})
		greeting := readRendered(t, dst, "greeting.txt")
		if !strings.Contains(greeting, "demo") || !strings.Contains(greeting, "FORMAL") {
			t.Errorf("greeting.txt not rendered with inputs: %q", greeting)
		}
		if got := readRendered(t, dst, "hello.txt"); !strings.Contains(got, "{{ .project_name }}") {
			t.Errorf("hello.txt should stay verbatim: %q", got)
		}
		for _, name := range []string{
			"README.md", "docs/guide.md", "optional/feature.txt",
			"optional/local-notes.txt", "config/local.conf",
			"new-name.txt", "hooks/post-setup.sh", "extra-banner.txt",
		} {
			if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(name))); err != nil {
				t.Errorf("rendered tree is missing %q: %v", name, err)
			}
		}
		if got := readRendered(t, dst, "extra-banner.txt"); !strings.Contains(got, "demo") {
			t.Errorf("extra-banner.txt not rendered with inputs: %q", got)
		}
		assertAbsent(t, dst,
			"drafts/sketch.txt", "scratch.log", "optional/skip-me.txt", ".turutan.yml")
		assertNoTmplSuffix(t, dst)
	})

	t.Run("no layers active", func(t *testing.T) {
		dst := renderCreated(t, src, map[string]any{
			"project_name":        "demo",
			"project_description": "demo desc",
			"profiles":            []string{},
			"greeting_style":      "casual",
			"features":            []string{},
			"enable_extra":        false,
		})
		for _, name := range []string{"greeting.txt", "hello.txt", "config/local.conf", "new-name.txt"} {
			if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(name))); err != nil {
				t.Errorf("rendered tree is missing %q: %v", name, err)
			}
		}
		assertAbsent(t, dst,
			"optional/feature.txt", "optional/local-notes.txt", "extra-banner.txt",
			"drafts/sketch.txt", "scratch.log", "optional/skip-me.txt")
		assertNoTmplSuffix(t, dst)
	})
}

func TestCreateRefusesNonEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Create(dir, CreateOptions{}); err == nil {
		t.Error("Create into non-empty dir without --force succeeded, want error")
	}
	createProject(t, dir, true)
	if _, err := os.Stat(filepath.Join(dir, ".turutan.yml")); err != nil {
		t.Errorf("forced Create did not scaffold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mine.txt")); err != nil {
		t.Errorf("forced Create removed existing file: %v", err)
	}
}
