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

const profileCLIManifest = "prompts:\n" +
	"  project_name:\n    prompt: \"Project name\"\n    type: string\n" +
	"  enabled_profiles:\n    prompt: \"Enabled profiles\"\n    type: array\n" +
	"    default: [\"core\"]\n" +
	"    options: [\"core\", \"go\", \"github\"]\n" +
	"profiles:\n" +
	"  go:\n    label: \"Go\"\n    includes: [\"go.mod\"]\n" +
	"  github:\n    label: \"GitHub\"\n    includes: [\".github/**\"]\n"

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

func updateCLIOptions(stdout, stderr *strings.Builder) UpdateOptions {
	opts := updateTestOptions(stdout, stderr)
	opts.Force = true
	opts.NonInteractive = true
	return opts
}

func TestComputeDiffWhat(t *testing.T) {
	setup := func(t *testing.T) string {
		t.Helper()
		src := makeTemplate(t, "", map[string]string{"up.txt": "v1\n", "loc.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "up.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, "loc.txt"), []byte("local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return projectDir
	}
	t.Run("default all shows both sides", func(t *testing.T) {
		projectDir := setup(t)
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if got := diffPaths(diffs); !slices.Equal(got, []string{"loc.txt", "up.txt"}) {
			t.Errorf("paths = %v, want [loc.txt up.txt]", got)
		}
	})
	t.Run("upstream hides local edits", func(t *testing.T) {
		projectDir := setup(t)
		opts := diffOptions()
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
		projectDir := setup(t)
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
		projectDir := setup(t)
		opts := diffOptions()
		opts.What = "bogus"
		if _, err := ComputeDiff(context.Background(), projectDir, opts); err == nil {
			t.Error("ComputeDiff with bogus --what succeeded, want error")
		} else if !strings.Contains(err.Error(), "--what") {
			t.Errorf("error = %q, want it to mention --what", err)
		}
	})
	t.Run("upstream clean when only local edits", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
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
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
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

func TestUpdateEnableDisable(t *testing.T) {
	t.Run("enable adds profile files and stores answers", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		assertProfileAbsent(t, projectDir, "go.mod")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Enable = []string{"go"}
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
		assertProfileAbsent(t, projectDir, ".github/ci.yml")
		if !slices.Contains(result.Updated, "go.mod") {
			t.Errorf("Updated = %v, want it to list go.mod", result.Updated)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core go custom]", got)
		}
		if !strings.Contains(stderr.String(), "turutan: template offers new profiles:") {
			t.Errorf("stderr = %q, want the new-profiles report", stderr.String())
		}
	})
	t.Run("disable removes profile files and untracks them", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Disable = []string{"go"}
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileAbsent(t, projectDir, "go.mod")
		if paths := lockPaths(t, projectDir); slices.Contains(paths, "go.mod") {
			t.Error("lock still contains go.mod after --disable go")
		}
		if !slices.Contains(result.Updated, "go.mod") {
			t.Errorf("Updated = %v, want it to list go.mod", result.Updated)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core custom]", got)
		}
	})
	t.Run("unknown profile errors with offered list", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Enable = []string{"nope"}
		_, err := Update(context.Background(), projectDir, opts)
		if err == nil {
			t.Fatal("Update with unknown profile succeeded, want error")
		}
		if !strings.Contains(err.Error(), `unknown profile "nope"`) {
			t.Errorf("error = %q, want it to name the profile", err)
		}
		if !strings.Contains(err.Error(), "go") {
			t.Errorf("error = %q, want it to list offered profiles", err)
		}
	})
}

func TestUpdatePreview(t *testing.T) {
	t.Run("preview prints plan without writing", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		beforeState, err := os.ReadFile(filepath.Join(projectDir, config.StateFileName)) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		beforeLock, err := os.ReadFile(filepath.Join(projectDir, config.LockFileName)) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
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
			t.Error("preview mutated state.json")
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
	t.Run("preview groups entries per profile", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Preview = true
		opts.Enable = []string{"go"}
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		for _, want := range []string{"preview core", "preview go", "+ go.mod (added)", "~ keep.txt (modified)"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout = %q, want it to mention %q", stdout.String(), want)
			}
		}
		assertProfileAbsent(t, projectDir, "go.mod")
	})
}

func TestUpdateRenames(t *testing.T) {
	evolve := func(t *testing.T, src string) {
		t.Helper()
		if err := os.WriteFile(
			filepath.Join(src, ".turutan.yml"),
			[]byte("renames:\n  old.txt: new.txt\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(src, "old.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(src, "old.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "new.txt"), data, 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
	}
	t.Run("unmodified file follows the rename", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"old.txt": "hello\n"})
		projectDir := bootstrapProject(t, src)
		evolve(t, src)
		var stdout, stderr strings.Builder
		result, err := Update(context.Background(), projectDir, updateCLIOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "new.txt", "hello\n")
		assertProfileAbsent(t, projectDir, "old.txt")
		paths := lockPaths(t, projectDir)
		if !slices.Contains(paths, "new.txt") {
			t.Errorf("lock paths = %v, want them to track new.txt", paths)
		}
		if slices.Contains(paths, "old.txt") {
			t.Errorf("lock paths = %v, want old.txt rewritten", paths)
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none for a clean rename", result.Conflicts)
		}
		if !slices.Contains(result.Updated, "new.txt") {
			t.Errorf("Updated = %v, want it to list new.txt", result.Updated)
		}
	})
	t.Run("diverged file conflicts at the new path", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"old.txt": "a\nbase\nc\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "old.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		evolve(t, src)
		if err := os.WriteFile(filepath.Join(src, "new.txt"), []byte("a\nREMOTE\nc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		result, err := Update(context.Background(), projectDir, updateCLIOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if !slices.Equal(result.Conflicts, []string{"new.txt"}) {
			t.Fatalf("Conflicts = %v, want [new.txt]", result.Conflicts)
		}
		got, err := os.ReadFile(filepath.Join(projectDir, "new.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"<<<<<<< local", "LOCAL", "REMOTE"} {
			if !strings.Contains(string(got), want) {
				t.Errorf("new.txt missing %q:\n%s", want, got)
			}
		}
		assertProfileAbsent(t, projectDir, "old.txt")
	})
}

func TestUpdatePersistsLockProfiles(t *testing.T) {
	src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
	projectDir := bootstrapProfiles(t, src, "core", "go")
	var stdout, stderr strings.Builder
	if _, err := Update(context.Background(), projectDir, updateCLIOptions(&stdout, &stderr)); err != nil {
		t.Fatalf("Update error: %v", err)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, entry := range lock.Files {
		byPath[entry.Path] = entry.Profile
	}
	if byPath["go.mod"] != "go" {
		t.Errorf("go.mod profile = %q, want go", byPath["go.mod"])
	}
	if byPath["keep.txt"] != "" {
		t.Errorf("keep.txt profile = %q, want empty", byPath["keep.txt"])
	}
}

func TestCheckUpdateNewProfiles(t *testing.T) {
	evolveManifest := func(t *testing.T, src, old, addition string) {
		t.Helper()
		manifest, err := os.ReadFile(filepath.Join(src, ".turutan.yml")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		augmented := strings.Replace(string(manifest), old, old+addition, 1)
		if augmented == string(manifest) {
			t.Fatal("could not splice the fixture manifest")
		}
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte(augmented), 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("quiet project reports offered profiles without flagging", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false: enabling fewer profiles is not drift")
		}
		if !strings.Contains(stdout.String(), "up to date") {
			t.Errorf("stdout = %q, want the up-to-date line", stdout.String())
		}
		for _, want := range []string{"turutan: template offers new profiles:", "go", "github"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout = %q, want it to mention %q", stdout.String(), want)
			}
		}
	})
	t.Run("added profile reported alongside template change", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		evolveManifest(t, src,
			"  github:\n    label: \"GitHub\"\n    includes: [\".github/**\"]\n",
			"  extra:\n    label: \"Extra\"\n    includes: [\"extra.txt\"]\n")
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if !result.Available {
			t.Error("Available = false, want true for the template change")
		}
		if !strings.Contains(stdout.String(), "update available") {
			t.Errorf("stdout = %q, want the update-available line", stdout.String())
		}
		if !strings.Contains(stdout.String(), "turutan: template offers new profiles: extra") {
			t.Errorf("stdout = %q, want the new-profiles line", stdout.String())
		}
	})
	t.Run("new prompt reported alongside template change", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		evolveManifest(t, src,
			"  project_name:\n    prompt: \"Project name\"\n    type: string\n",
			"  service_name:\n    prompt: \"Service name\"\n    type: string\n")
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if !result.Available {
			t.Error("Available = false, want true for the template change")
		}
		if !strings.Contains(stdout.String(), "turutan: template adds new prompts: service_name") {
			t.Errorf("stdout = %q, want the new-prompts line", stdout.String())
		}
	})
}

func TestListProfiles(t *testing.T) {
	src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
	projectDir := bootstrapProfiles(t, src, "core", "go")
	info, err := ListProfiles(context.Background(), projectDir, ProfileOptions{})
	if err != nil {
		t.Fatalf("ListProfiles error: %v", err)
	}
	if !slices.Equal(info.Offered, []string{"core", "custom", "github", "go"}) {
		t.Errorf("Offered = %v, want [core custom github go]", info.Offered)
	}
	if !slices.Equal(info.Enabled, []string{"core", "custom", "go"}) {
		t.Errorf("Enabled = %v, want [core custom go]", info.Enabled)
	}
	if _, err := ListProfiles(context.Background(), t.TempDir(), ProfileOptions{}); err == nil {
		t.Error("ListProfiles without state succeeded, want error")
	}
}

func TestUpdateDisableValidation(t *testing.T) {
	t.Run("unknown disable errors with offered list", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Disable = []string{"nope"}
		_, err := Update(context.Background(), projectDir, opts)
		if err == nil {
			t.Fatal("Update with unknown --disable succeeded, want error")
		}
		if !strings.Contains(err.Error(), `unknown profile "nope"`) {
			t.Errorf("error = %q, want it to name the profile", err)
		}
		if !strings.Contains(err.Error(), "go") {
			t.Errorf("error = %q, want it to list offered profiles", err)
		}
	})
	t.Run("same id enable and disable keeps it enabled", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Enable = []string{"go"}
		opts.Disable = []string{"go"}
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core go custom] (enable wins)", got)
		}
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
	})
	t.Run("whitespace ids are trimmed", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Enable = []string{"  go  "}
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core go custom]", got)
		}
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
	})
	t.Run("whitespace disable is trimmed", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Disable = []string{"  go  "}
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core custom]", got)
		}
		assertProfileAbsent(t, projectDir, "go.mod")
	})
	t.Run("disable core is refused", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Disable = []string{"core"}
		if _, err := Update(context.Background(), projectDir, opts); err == nil {
			t.Fatal("Update with --disable core succeeded, want refusal")
		} else if !strings.Contains(err.Error(), `cannot disable always-enabled profile "core"`) {
			t.Errorf("error = %q, want the always-enabled refusal", err)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want unchanged [core go custom]", got)
		}
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
		assertProfileFile(t, projectDir, "keep.txt", "keep v1\n")
	})
	t.Run("disable custom is refused", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Disable = []string{"custom"}
		if _, err := Update(context.Background(), projectDir, opts); err == nil {
			t.Fatal("Update with --disable custom succeeded, want refusal")
		} else if !strings.Contains(err.Error(), `cannot disable always-enabled profile "custom"`) {
			t.Errorf("error = %q, want the always-enabled refusal", err)
		}
	})
	t.Run("enable core and custom is a no-op", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Enable = []string{"core", "custom"}
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core go custom]", got)
		}
	})
}

func TestUpdateDroppedProfilesStderr(t *testing.T) {
	t.Run("dropped profile reported on stderr", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go", "github")
		manifest, err := os.ReadFile(filepath.Join(src, ".turutan.yml")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		trimmed := strings.Replace(string(manifest), "  github:\n    label: \"GitHub\"\n    includes: [\".github/**\"]\n", "", 1)
		if trimmed == string(manifest) {
			t.Fatal("could not splice the fixture manifest")
		}
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte(trimmed), 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		result, err := Update(context.Background(), projectDir, updateCLIOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if !strings.Contains(stderr.String(), "turutan: template dropped profiles: github") {
			t.Errorf("stderr = %q, want the dropped-profiles line", stderr.String())
		}
		assertProfileFile(t, projectDir, "keep.txt", "keep v2\n")
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none", result.Conflicts)
		}
	})
}

func TestUpdateNewPromptsStderr(t *testing.T) {
	t.Run("new prompt reported on stderr alongside change", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest, profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core")
		manifest, err := os.ReadFile(filepath.Join(src, ".turutan.yml")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		augmented := strings.Replace(string(manifest),
			"  project_name:\n    prompt: \"Project name\"\n    type: string\n",
			"  project_name:\n    prompt: \"Project name\"\n    type: string\n  service_name:\n    prompt: \"Service name\"\n    type: string\n    default: \"svc\"\n", 1)
		if augmented == string(manifest) {
			t.Fatal("could not splice the fixture manifest")
		}
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte(augmented), 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Defaults = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if !strings.Contains(stderr.String(), "turutan: template adds new prompts: service_name") {
			t.Errorf("stderr = %q, want the new-prompts line", stderr.String())
		}
		assertProfileFile(t, projectDir, "keep.txt", "keep v2\n")
	})
}

func TestUpdateLocalGitRename(t *testing.T) {
	t.Run("commit identity advances across rename", func(t *testing.T) {
		repoDir, old := initTemplateRepo(t, map[string]string{"old.txt": "hello\n"})
		projectDir := bootstrapProject(t, repoDir)
		if err := os.WriteFile(filepath.Join(repoDir, ".turutan.yml"), []byte("renames:\n  old.txt: new.txt\n"), 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(repoDir, "old.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(repoDir, "old.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "new.txt"), data, 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		fresh := commitAll(t, repoDir, "rename old to new")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if result.Old != old {
			t.Errorf("Old = %q, want %q", result.Old, old)
		}
		if result.New != fresh {
			t.Errorf("New = %q, want %q", result.New, fresh)
		}
		assertProfileFile(t, projectDir, "new.txt", "hello\n")
		assertProfileAbsent(t, projectDir, "old.txt")
		paths := lockPaths(t, projectDir)
		if !slices.Contains(paths, "new.txt") {
			t.Errorf("lock paths = %v, want them to track new.txt", paths)
		}
		if slices.Contains(paths, "old.txt") {
			t.Errorf("lock paths = %v, want old.txt rewritten", paths)
		}
	})
	t.Run("rename of profile-owned file", func(t *testing.T) {
		files := map[string]string{".turutan.yml": profileCLIManifest}
		for name, content := range profileCLIFiles {
			files[name] = content
		}
		repoDir, _ := initTemplateRepo(t, files)
		projectDir := bootstrapProfiles(t, repoDir, "core", "go")
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
		manifest, err := os.ReadFile(filepath.Join(repoDir, ".turutan.yml")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		renamed := "renames:\n  go.mod: go-new.mod\n" + string(manifest)
		if err := os.WriteFile(filepath.Join(repoDir, ".turutan.yml"), []byte(renamed), 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(repoDir, "go.mod")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(repoDir, "go.mod")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "go-new.mod"), data, 0o600); err != nil { // #nosec G703 -- test-only fixture path under t.TempDir() with a constant segment, not external input
			t.Fatal(err)
		}
		commitAll(t, repoDir, "rename profile-owned file")
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if result.Old == result.New {
			t.Error("Old == New, want the commit identity to advance")
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none for a clean profile rename", result.Conflicts)
		}
	})
}

func TestUpdatePreserveExemptFromPruning(t *testing.T) {
	t.Run("preserve-listed file under removed profile stays", func(t *testing.T) {
		src := makeTemplate(t, profileCLIManifest+"preserve:\n  - \"go.mod\"\n", profileCLIFiles)
		projectDir := bootstrapProfiles(t, src, "core", "go")
		if err := os.WriteFile(filepath.Join(projectDir, "go.mod"), []byte("module example // local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateCLIOptions(&stdout, &stderr)
		opts.Disable = []string{"go"}
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "go.mod", "module example // local\n")
		if paths := lockPaths(t, projectDir); slices.Contains(paths, "go.mod") {
			t.Error("lock still contains go.mod after --disable go, want it untracked")
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "custom"}) {
			t.Errorf("stored enabled_profiles = %v, want [core custom]", got)
		}
	})
}
