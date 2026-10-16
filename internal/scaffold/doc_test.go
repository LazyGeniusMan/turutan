// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
)

func TestPlantDocFiles(t *testing.T) {
	t.Parallel()
	t.Run("moves rendered file and removes stage-root copy", func(t *testing.T) {
		t.Parallel()
		stage := t.TempDir()
		const body = "# rendered title\n"
		if err := os.WriteFile(filepath.Join(stage, "README.md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		manifest := &turutanconfig.Manifest{DocFiles: []string{"README.md"}}
		if err := plantDocFiles(stage, manifest); err != nil {
			t.Fatalf("plantDocFiles error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(stage, ".config", "turutan", "README.md"))
		if err != nil {
			t.Fatalf("planted doc missing: %v", err)
		}
		if string(got) != body {
			t.Errorf("planted doc = %q, want rendered %q", got, body)
		}
		if _, err := os.Stat(filepath.Join(stage, "README.md")); !os.IsNotExist(err) {
			t.Error("staged README.md still present, want it moved")
		}
	})
	t.Run("missing entry is skipped silently", func(t *testing.T) {
		t.Parallel()
		manifest := &turutanconfig.Manifest{DocFiles: []string{"PROFILE-ONLY.md"}}
		if err := plantDocFiles(t.TempDir(), manifest); err != nil {
			t.Errorf("plantDocFiles error: %v", err)
		}
	})
	t.Run("nested parents are created", func(t *testing.T) {
		t.Parallel()
		stage := t.TempDir()
		if err := os.MkdirAll(filepath.Join(stage, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "docs", "guide.md"), []byte("# guide\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		manifest := &turutanconfig.Manifest{DocFiles: []string{"docs/guide.md"}}
		if err := plantDocFiles(stage, manifest); err != nil {
			t.Fatalf("plantDocFiles error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(stage, ".config", "turutan", "docs", "guide.md")); err != nil {
			t.Errorf("planted nested doc missing: %v", err)
		}
	})
	t.Run("no doc-files is a no-op", func(t *testing.T) {
		t.Parallel()
		if err := plantDocFiles(t.TempDir(), &turutanconfig.Manifest{}); err != nil {
			t.Errorf("plantDocFiles error: %v", err)
		}
	})
}

func TestInitDocFiles(t *testing.T) {
	t.Parallel()
	t.Run("rendered template output plants into config dir", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"go.mod.tmpl":    "module {{.project_name}}\n",
			"README.md.tmpl": "# {{.project_name}}\n",
		}
		_, ref := makeTestRegistry(t, "doc-files: [README.md]\n", files)
		target := filepath.Join(t.TempDir(), "proj")
		if err := Init(context.Background(), target, ref, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, ".config", "turutan", "README.md"))
		if err != nil {
			t.Fatalf("planted doc missing: %v", err)
		}
		if string(got) != "# proj\n" {
			t.Errorf("planted doc = %q, want rendered %q", got, "# proj\n")
		}
		if _, err := os.Stat(filepath.Join(target, "README.md")); !os.IsNotExist(err) {
			t.Error("template-root README.md published, want only the .config copy")
		}
	})
	t.Run("profile-gated doc skipped when profile off", func(t *testing.T) {
		t.Parallel()
		const condManifest = "inputs:\n" +
			"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: my-proj\n" +
			"conditionals:\n" +
			"  - if: '{{ not (contains .profiles \"go\") }}'\n" +
			"    excludes: [\"GO.md\", \"GO.md.tmpl\"]\n" +
			"doc-files: [GO.md]\n"
		files := map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
			"GO.md.tmpl":  "# {{.project_name}} go notes\n",
		}
		_, ref := makeTestRegistry(t, condManifest, files)
		target := filepath.Join(t.TempDir(), "proj")
		if err := Init(context.Background(), target, ref, testOptions(&strings.Builder{})); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, ".config", "turutan", "GO.md")); !os.IsNotExist(err) {
			t.Error("GO.md planted without its profile, want it skipped")
		}
	})
}
