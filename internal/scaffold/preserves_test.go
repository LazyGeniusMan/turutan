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

const preservesSeedManifest = "inputs:\n" +
	"  project_name:\n    label: \"Project Name\"\n    type: string\n" +
	"  profiles:\n    label: \"Profiles\"\n    type: array\n    default: []\n" +
	"preserves:\n  - \"keep.local\"\n  - \"extra.local\"\n" +
	"conditionals:\n" +
	"  - if: '{{contains .profiles \"docs\"}}'\n    preserves: [\"docs.local\"]\n"

var preservesSeedFiles = map[string]string{
	"keep.txt":   "keep v1\n",
	"keep.local": "seed local\n",
	"docs.local": "seed docs\n",
}

func writeLocalFile(t *testing.T, projectDir, rel, content string) {
	t.Helper()
	path := filepath.Join(projectDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPreservedGlobsUnion(t *testing.T) {
	t.Parallel()
	src := makeTemplate(t, preservesSeedManifest, preservesSeedFiles)
	manifest, err := config.LoadManifest(os.DirFS(src))
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	t.Run("root only without docs", func(t *testing.T) {
		t.Parallel()
		got, err := preservedGlobs(map[string]any{"profiles": []string{}}, manifest)
		if err != nil {
			t.Fatalf("preservedGlobs error: %v", err)
		}
		if !slices.Equal(got, []string{"keep.local", "extra.local"}) {
			t.Errorf("preserved = %v, want root only", got)
		}
	})
	t.Run("conditional union with docs", func(t *testing.T) {
		t.Parallel()
		got, err := preservedGlobs(map[string]any{"profiles": []string{"docs"}}, manifest)
		if err != nil {
			t.Fatalf("preservedGlobs error: %v", err)
		}
		if !slices.Equal(got, []string{"keep.local", "extra.local", "docs.local"}) {
			t.Errorf("preserved = %v, want root plus conditional", got)
		}
	})
	t.Run("nil manifest", func(t *testing.T) {
		t.Parallel()
		got, err := preservedGlobs(map[string]any{}, nil)
		if err != nil || len(got) != 0 {
			t.Errorf("preservedGlobs(nil) = %v, %v; want empty, nil", got, err)
		}
	})
}

func TestPreserveBootstrapSeedOnce(t *testing.T) {
	src := makeTemplate(t, preservesSeedManifest, preservesSeedFiles)
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	if err := Bootstrap(context.Background(), src, target, testOptions(&stdout)); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	writeLocalFile(t, target, "keep.local", "local\n")
	writeFiles(t, src, map[string]string{"keep.local": "seed v2\n", "keep.txt": "keep v2\n"})
	var reStdout strings.Builder
	reOpts := testOptions(&reStdout)
	reOpts.Force = true
	if err := Bootstrap(context.Background(), src, target, reOpts); err != nil {
		t.Fatalf("re-bootstrap error: %v", err)
	}
	assertProfileFile(t, target, "keep.local", "local\n")
	assertProfileFile(t, target, "keep.txt", "keep v2\n")
}

func TestPreserveUpdateSeedOnce(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		src := makeTemplate(t, preservesSeedManifest, preservesSeedFiles)
		projectDir := bootstrapProject(t, src)
		return src, projectDir
	}
	t.Run("both changed keeps local preserved but updates core", func(t *testing.T) {
		src, projectDir := setup(t)
		writeLocalFile(t, projectDir, "keep.local", "local\n")
		writeFiles(t, src, map[string]string{"keep.local": "seed v2\n", "keep.txt": "keep v2\n"})
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "keep.local", "local\n")
		assertProfileFile(t, projectDir, "keep.txt", "keep v2\n")
	})
	t.Run("added present keeps local preserved", func(t *testing.T) {
		src, projectDir := setup(t)
		if err := os.Remove(filepath.Join(projectDir, "keep.local")); err != nil {
			t.Fatal(err)
		}
		writeLocalFile(t, projectDir, "keep.local", "local\n")
		writeFiles(t, src, map[string]string{"keep.local": "seed v2\n"})
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "keep.local", "local\n")
	})
	t.Run("added absent seeds preserved", func(t *testing.T) {
		src, projectDir := setup(t)
		writeFiles(t, src, map[string]string{"extra.local": "seed extra\n"})
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "extra.local", "seed extra\n")
		if !slices.Contains(result.Updated, "extra.local") {
			t.Errorf("Updated = %v, want it to list the seeded extra.local", result.Updated)
		}
	})
	t.Run("locked deleted preserved stays deleted", func(t *testing.T) {
		src, projectDir := setup(t)
		if err := os.Remove(filepath.Join(projectDir, "keep.local")); err != nil {
			t.Fatal(err)
		}
		writeFiles(t, src, map[string]string{"keep.local": "seed v2\n"})
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(projectDir, "keep.local")); !os.IsNotExist(err) {
			t.Error("keep.local resurrected, want locally deleted preserved kept gone")
		}
	})
	t.Run("dropped keeps local preserved with note", func(t *testing.T) {
		src, projectDir := setup(t)
		writeLocalFile(t, projectDir, "keep.local", "local\n")
		if err := os.Remove(filepath.Join(src, "keep.local")); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "keep.local", "local\n")
		if !strings.Contains(stderr.String(), `keeping preserved "keep.local"`) {
			t.Errorf("stderr = %q, want the preserved keep note", stderr.String())
		}
	})
	t.Run("dirty guard ignores preserved edits", func(t *testing.T) {
		_, projectDir := setup(t)
		writeLocalFile(t, projectDir, "keep.local", "local\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update without --force error: %v", err)
		}
	})
	t.Run("dirty guard still fires for core files", func(t *testing.T) {
		_, projectDir := setup(t)
		writeLocalFile(t, projectDir, "keep.txt", "keep // local\n")
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

func TestPreservePreviewAndDiff(t *testing.T) {
	src := makeTemplate(t, preservesSeedManifest, preservesSeedFiles)
	projectDir := bootstrapProject(t, src)
	writeLocalFile(t, projectDir, "keep.local", "local\n")
	writeFiles(t, src, map[string]string{
		"keep.local": "seed v2\n",
		"keep.txt":   "keep v2\n",
		"extra.txt":  "extra\n",
	})
	t.Run("preview hides present preserved lists absent", func(t *testing.T) {
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Preview = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("preview error: %v", err)
		}
		out := stdout.String()
		if strings.Contains(out, "keep.local") {
			t.Errorf("preview = %q, want no present preserved entry", out)
		}
		if !strings.Contains(out, "extra.txt") {
			t.Errorf("preview = %q, want the added extra.txt listed", out)
		}
		if !strings.Contains(out, "keep.txt") {
			t.Errorf("preview = %q, want the core change listed", out)
		}
	})
	t.Run("diff hides preserved drift", func(t *testing.T) {
		writeLocalFile(t, projectDir, "keep.txt", "keep // local\n")
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		for _, diff := range diffs {
			if diff.Path == "keep.local" {
				t.Errorf("diff reports keep.local, want preserves suppression:\n%s", diff.Unified)
			}
		}
		if len(diffs) == 0 {
			t.Error("diffs empty, want the keep.txt core drift still reported")
		}
	})
}

func TestConditionalPreservesOnlyIfAbsent(t *testing.T) {
	setup := func(t *testing.T, profiles ...string) (string, string) {
		t.Helper()
		src := makeTemplate(t, preservesSeedManifest, preservesSeedFiles)
		return src, bootstrapProfiles(t, src, profiles...)
	}
	t.Run("inactive conditional preserves sync normally", func(t *testing.T) {
		src, projectDir := setup(t)
		writeFiles(t, src, map[string]string{"docs.local": "seed docs v2\n"})
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.NonInteractive = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "docs.local", "seed docs v2\n")
	})
	t.Run("active conditional preserves keep local", func(t *testing.T) {
		src, projectDir := setup(t, "docs")
		writeLocalFile(t, projectDir, "docs.local", "local docs\n")
		writeFiles(t, src, map[string]string{"docs.local": "seed docs v2\n"})
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.NonInteractive = true
		dataFile := profilesDataFile(t, "docs")
		opts.DataFile = dataFile
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "docs.local", "local docs\n")
	})
}
