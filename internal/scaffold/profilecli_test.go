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

const profileCLIManifest = "inputs:\n" +
	"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: my-proj\n" +
	"  profiles:\n    label: \"Profiles\"\n    type: array\n    default: []\n" +
	"conditionals:\n" +
	"  - if: '{{not (contains .profiles \"go\")}}'\n    excludes: [\"go.mod\"]\n" +
	"  - if: '{{not (contains .profiles \"github\")}}'\n    excludes: [\".github/**\"]\n"

var profileCLIFiles = map[string]string{
	"keep.txt":       "keep v1\n",
	"go.mod":         "module example\n",
	".github/ci.yml": "ci\n",
}

func diffPaths(diffs []FileDiff) []string {
	paths := make([]string, 0, len(diffs))
	for _, diff := range diffs {
		paths = append(paths, diff.Path)
	}
	slices.Sort(paths)
	return paths
}

func updateCLIOptions(stdout, stderr *strings.Builder, _ string) UpdateOptions {
	opts := updateTestOptions(stdout, stderr)
	opts.Force = true
	opts.NonInteractive = true
	return opts
}

func TestComputeDiffWhat(t *testing.T) {
	setup := func(t *testing.T) (string, string, string) {
		t.Helper()
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"up.txt": "v1\n", "loc.txt": "v1\n"})
		fresh := updateTestTemplateFile(t, registryURI, testRegistryTemplate, "up.txt", "v2\n")
		if err := os.WriteFile(filepath.Join(projectDir, "loc.txt"), []byte("local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return projectDir, registryURI, fresh
	}
	t.Run("default all shows both sides", func(t *testing.T) {
		projectDir, _, fresh := setup(t)
		opts := diffOptions()
		opts.Ref = fresh
		diffs, err := ComputeDiff(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if got := diffPaths(diffs); !slices.Equal(got, []string{"loc.txt", "up.txt"}) {
			t.Errorf("paths = %v, want [loc.txt up.txt]", got)
		}
	})
	t.Run("upstream hides local edits", func(t *testing.T) {
		projectDir, _, fresh := setup(t)
		opts := diffOptions()
		opts.Ref = fresh
		opts.What = "upstream"
		diffs, err := ComputeDiff(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if got := diffPaths(diffs); !slices.Equal(got, []string{"up.txt"}) {
			t.Errorf("paths = %v, want [up.txt]", got)
		}
		if !strings.Contains(diffs[0].Unified, "+v2") {
			t.Errorf("Unified missing template line:\n%s", diffs[0].Unified)
		}
	})
	t.Run("local hides template changes", func(t *testing.T) {
		projectDir, _, _ := setup(t)
		opts := diffOptions()
		opts.What = "local"
		diffs, err := ComputeDiff(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if got := diffPaths(diffs); !slices.Equal(got, []string{"loc.txt"}) {
			t.Errorf("paths = %v, want [loc.txt]", got)
		}
		if !strings.Contains(diffs[0].Unified, "+local") {
			t.Errorf("Unified missing worktree line:\n%s", diffs[0].Unified)
		}
	})
	t.Run("unknown what errors", func(t *testing.T) {
		projectDir, _, _ := setup(t)
		opts := diffOptions()
		opts.What = "bogus"
		if _, err := ComputeDiff(context.Background(), projectDir, opts); err == nil {
			t.Error("ComputeDiff with bogus --what succeeded, want error")
		} else if !strings.Contains(err.Error(), "--what") {
			t.Errorf("error = %q, want it to mention --what", err)
		}
	})
	t.Run("upstream clean when only local edits", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := diffOptions()
		opts.What = "upstream"
		diffs, err := ComputeDiff(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 0 {
			t.Errorf("diffs = %+v, want none upstream", diffs)
		}
	})
	t.Run("local clean when only template changes", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "hello.txt", "v2\n")
		opts := diffOptions()
		opts.What = "local"
		diffs, err := ComputeDiff(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 0 {
			t.Errorf("diffs = %+v, want none local", diffs)
		}
	})
}

func TestUpdatePreview(t *testing.T) {
	t.Run("preview prints plan without writing", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "hello.txt", "v2\n")
		beforeState, err := os.ReadFile(filepath.Join(projectDir, config.StateFileName)) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		beforeLock, err := os.ReadFile(filepath.Join(projectDir, config.LockFileName)) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr, registryURI)
		opts.Preview = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "hello.txt")); string(got) != "v1\n" { // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
			t.Errorf("hello.txt = %q, want preview to leave the worktree alone", got)
		}
		afterState, err := os.ReadFile(filepath.Join(projectDir, config.StateFileName)) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		afterLock, err := os.ReadFile(filepath.Join(projectDir, config.LockFileName)) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		if string(afterState) != string(beforeState) {
			t.Error("preview mutated state.yml")
		}
		if string(afterLock) != string(beforeLock) {
			t.Error("preview mutated lock.json")
		}
		for _, want := range []string{"preview", "hello.txt", "modified"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout = %q, want it to mention %q", stdout.String(), want)
			}
		}
		if !slices.Contains(result.Updated, "hello.txt") {
			t.Errorf("Updated = %v, want it to list hello.txt", result.Updated)
		}
		if result.Old == result.New {
			t.Error("Old == New, want the preview to resolve the fresh identity")
		}
	})
	t.Run("preview groups entries by kind", func(t *testing.T) {
		projectDir, registryURI := initRegistryProfiles(t, profileCLIManifest, profileCLIFiles, "core")
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "keep.txt", "keep v2\n")
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "new.txt", "new\n")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr, registryURI)
		opts.Preview = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		for _, want := range []string{"preview added", "preview modified", "+ new.txt (added)", "~ keep.txt (modified)"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout = %q, want it to mention %q", stdout.String(), want)
			}
		}
		assertProfileAbsent(t, projectDir, "new.txt")
	})
}
