// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

const customSeedManifest = "inputs:\n" +
	"  project_name:\n    question: \"Project Name\"\n    type: string\n" +
	"profiles:\n" +
	"  go:\n    label: \"Go\"\n    includes: [\"go.mod\"]\n" +
	"  custom:\n    label: \"Custom\"\n    includes: [\"custom.txt\", \"extra.txt\"]\n"

var customSeedFiles = map[string]string{
	"keep.txt":   "keep v1\n",
	"go.mod":     "module example\n",
	"custom.txt": "seed v1\n",
}

func writeCustomLocal(t *testing.T, projectDir, rel, content string) {
	t.Helper()
	path := filepath.Join(projectDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOfferedProfilesDeduplicatesCustom(t *testing.T) {
	manifest := &config.Manifest{Profiles: map[string]config.ManifestProfile{
		"go":     {},
		"custom": {},
	}}
	if got := offeredProfiles(manifest); !slices.Equal(got, []string{"core", "custom", "go"}) {
		t.Errorf("offeredProfiles = %v, want [core custom go]", got)
	}
	if got := offeredProfiles(nil); !slices.Equal(got, []string{"core", "custom"}) {
		t.Errorf("offeredProfiles(nil) = %v, want [core custom]", got)
	}
}

func TestNormalizeProfilesSelectionOrder(t *testing.T) {
	manifest := &config.Manifest{Inputs: map[string]config.ManifestInput{
		"project_name": {Type: "string"},
	}}
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "shuffled slice", input: []string{"go", "core"}, want: []string{"core", "go", "custom"}},
		{name: "custom first", input: []string{"custom", "go"}, want: []string{"core", "go", "custom"}},
		{name: "comma packed", input: []string{"go, core"}, want: []string{"core", "go", "custom"}},
		{name: "empty defaults", input: nil, want: []string{"core", "ai", "custom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = manifest
			got := normalizeProfilesSelection(tt.input)
			if !slices.Equal(got, tt.want) {
				t.Errorf("profiles = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMisercOrderWithShuffledProfiles(t *testing.T) {
	src := makeTemplate(t, customSeedManifest, map[string]string{
		"env.txt.tmpl": "env = [{{ range $i, $p := .profiles }}{{ if $i }}, {{ end }}\"{{ $p }}\"{{ end }}]\n",
		"keep.txt":     "keep\n",
	})
	profiles := []string{"go", "core"}
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	opts := testOptions(&stdout)
	opts.Profiles = profiles
	if err := Bootstrap(context.Background(), src, target, opts); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "env.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "env = [\"core\", \"go\", \"custom\"]\n" {
		t.Errorf("env.txt = %q, want core-first custom-last order", got)
	}
}

func TestCustomBootstrapSeedOnce(t *testing.T) {
	src := makeTemplate(t, customSeedManifest, customSeedFiles)
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	opts := testOptions(&stdout)
	opts.Profiles = []string{"core"}
	if err := Bootstrap(context.Background(), src, target, opts); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	writeCustomLocal(t, target, "custom.txt", "local\n")
	writeFiles(t, src, map[string]string{"custom.txt": "seed v2\n", "keep.txt": "keep v2\n"})
	var reStdout strings.Builder
	reOpts := testOptions(&reStdout)
	reOpts.Force = true
	reOpts.Profiles = []string{"core"}
	if err := Bootstrap(context.Background(), src, target, reOpts); err != nil {
		t.Fatalf("re-bootstrap error: %v", err)
	}
	assertProfileFile(t, target, "custom.txt", "local\n")
	assertProfileFile(t, target, "keep.txt", "keep v2\n")
	lock, err := config.LoadLock(os.DirFS(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range lock.Files {
		if entry.Path != "custom.txt" {
			continue
		}
		if entry.SHA256 != config.FileEntry("custom.txt", []byte("local\n")).SHA256 {
			t.Error("lock custom.txt does not match the preserved local content")
		}
	}
}

func TestCustomUpdateSeedOnce(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		src := makeTemplate(t, customSeedManifest, customSeedFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		return src, projectDir
	}
	t.Run("both changed keeps local custom but updates core", func(t *testing.T) {
		src, projectDir := setup(t)
		writeCustomLocal(t, projectDir, "custom.txt", "local\n")
		writeFiles(t, src, map[string]string{"custom.txt": "seed v2\n", "keep.txt": "keep v2\n"})
		updateProfiles(t, projectDir, "core")
		assertProfileFile(t, projectDir, "custom.txt", "local\n")
		assertProfileFile(t, projectDir, "keep.txt", "keep v2\n")
	})
	t.Run("added present keeps local custom", func(t *testing.T) {
		src, projectDir := setup(t)
		writeCustomLocal(t, projectDir, "extra.txt", "local extra\n")
		writeFiles(t, src, map[string]string{"extra.txt": "seed extra\n"})
		updateProfiles(t, projectDir, "core")
		assertProfileFile(t, projectDir, "extra.txt", "local extra\n")
	})
	t.Run("added absent seeds custom", func(t *testing.T) {
		src, projectDir := setup(t)
		writeFiles(t, src, map[string]string{"extra.txt": "seed extra\n"})
		result := updateProfiles(t, projectDir, "core")
		assertProfileFile(t, projectDir, "extra.txt", "seed extra\n")
		if !slices.Contains(result.Updated, "extra.txt") {
			t.Errorf("Updated = %v, want it to list the seeded extra.txt", result.Updated)
		}
	})
	t.Run("dropped keeps local custom with note", func(t *testing.T) {
		src, projectDir := setup(t)
		writeCustomLocal(t, projectDir, "custom.txt", "local\n")
		if err := os.Remove(filepath.Join(src, "custom.txt")); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "custom.txt", "local\n")
		if !strings.Contains(stderr.String(), `keeping custom-owned "custom.txt"`) {
			t.Errorf("stderr = %q, want the custom-owned keep note", stderr.String())
		}
	})
	t.Run("dirty guard ignores custom-owned edits", func(t *testing.T) {
		_, projectDir := setup(t)
		writeCustomLocal(t, projectDir, "custom.txt", "local\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update without --force error: %v", err)
		}
	})
	t.Run("dirty guard still fires for core files", func(t *testing.T) {
		_, projectDir := setup(t)
		writeCustomLocal(t, projectDir, "keep.txt", "keep // local\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		_, err := Update(context.Background(), projectDir, opts)
		if err == nil {
			t.Fatal("Update without --force succeeded, want dirty-tree error")
		}
		if !strings.Contains(err.Error(), "keep.txt") {
			t.Errorf("error = %q, want it to name keep.txt", err)
		}
	})
}

func TestCustomPreviewSeedOnce(t *testing.T) {
	src := makeTemplate(t, customSeedManifest, customSeedFiles)
	projectDir := bootstrapProfiles(t, src, "core")
	writeCustomLocal(t, projectDir, "custom.txt", "local\n")
	writeFiles(t, src, map[string]string{
		"custom.txt": "seed v2\n",
		"keep.txt":   "keep v2\n",
		"extra.txt":  "seed extra\n",
	})
	preview := func(t *testing.T) string {
		t.Helper()
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Preview = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("preview error: %v", err)
		}
		return stdout.String()
	}
	t.Run("present custom excluded absent custom added", func(t *testing.T) {
		out := preview(t)
		if strings.Contains(out, "custom.txt") {
			t.Errorf("preview = %q, want no present custom-owned entry", out)
		}
		if !strings.Contains(out, "extra.txt") {
			t.Errorf("preview = %q, want the absent custom seed listed", out)
		}
		if !strings.Contains(out, "keep.txt") {
			t.Errorf("preview = %q, want the core change listed", out)
		}
	})
	t.Run("present new custom seed excluded", func(t *testing.T) {
		writeCustomLocal(t, projectDir, "extra.txt", "local extra\n")
		if out := preview(t); strings.Contains(out, "extra.txt") {
			t.Errorf("preview = %q, want no present custom-owned entry", out)
		}
	})
}

func TestCustomDiffSeedOnce(t *testing.T) {
	src := makeTemplate(t, customSeedManifest, customSeedFiles)
	projectDir := bootstrapProfiles(t, src, "core")
	writeFiles(t, src, map[string]string{"custom.txt": "seed v2\n", "keep.txt": "keep v2\n"})
	t.Run("present custom shows no drift", func(t *testing.T) {
		writeCustomLocal(t, projectDir, "custom.txt", "local\n")
		writeCustomLocal(t, projectDir, "keep.txt", "keep // local\n")
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		for _, diff := range diffs {
			if diff.Path == "custom.txt" {
				t.Errorf("diff reports custom.txt, want seed-once suppression:\n%s", diff.Unified)
			}
		}
		if !slices.Contains(diffPaths(diffs), "keep.txt") {
			t.Errorf("paths = %v, want the core drift still reported", diffPaths(diffs))
		}
	})
	t.Run("absent custom reports as new", func(t *testing.T) {
		if err := os.Remove(filepath.Join(projectDir, "custom.txt")); err != nil {
			t.Fatal(err)
		}
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if !slices.Contains(diffPaths(diffs), "custom.txt") {
			t.Errorf("paths = %v, want absent custom seed reported", diffPaths(diffs))
		}
	})
}

func TestCustomCheckUpdateSeedOnce(t *testing.T) {
	src := makeTemplate(t, customSeedManifest, customSeedFiles)
	projectDir := bootstrapProfiles(t, src, "core")
	t.Run("local custom edits stay quiet", func(t *testing.T) {
		writeCustomLocal(t, projectDir, "custom.txt", "local\n")
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true after local-only custom edits, want quiet")
		}
	})
	t.Run("core template change still reported", func(t *testing.T) {
		writeFiles(t, src, map[string]string{"keep.txt": "keep v2\n"})
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if !result.Available {
			t.Error("Available = false after a core template change, want true")
		}
	})
}
