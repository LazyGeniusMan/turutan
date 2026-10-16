// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func stateSyncTestOptions(stdout, stderr *strings.Builder) SyncOptions {
	return SyncOptions{
		Engine: "0.1.0",
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  strings.NewReader(""),
	}
}

func stateInputTestOptions(stdout, stderr *strings.Builder) InputOptions {
	return InputOptions{
		Engine: "0.1.0",
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  strings.NewReader(""),
	}
}

func storedCommit(t *testing.T, projectDir string) string {
	t.Helper()
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	return state.ResolvedCommit
}

func editStoredData(t *testing.T, projectDir, key, value string) {
	t.Helper()
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	if state.Data == nil {
		state.Data = map[string]any{}
	}
	state.Data[key] = value
	if err := config.SaveState(projectDir, state); err != nil {
		t.Fatal(err)
	}
}

const stateSyncManifest = "inputs:\n" +
	"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: my-proj\n" +
	"  greeting:\n    label: \"Greeting\"\n    type: string\n    default: \"hi\"\n"

var stateSyncFiles = map[string]string{
	"greet.txt.tmpl": "{{.greeting}} {{.project_name}}\n",
}

func TestStateSyncFilesystem(t *testing.T) {
	t.Run("hand-edited data re-renders at pinned commit", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		old := storedCommit(t, projectDir)
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi proj\n" {
			t.Fatalf("greet.txt = %q, want defaulted init render", got)
		}
		editStoredData(t, projectDir, "project_name", "edited")
		var stdout, stderr strings.Builder
		result, err := StateSync(
			context.Background(), projectDir, stateSyncTestOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("StateSync error: %v", err)
		}
		got, _ = os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi edited\n" {
			t.Errorf("greet.txt = %q, want hand-edited re-render", got)
		}
		if result.Old != old || result.New != old {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, old)
		}
		if got := storedCommit(t, projectDir); got != old {
			t.Errorf("stored commit = %q, want pinned %q", got, old)
		}
		lock, err := config.LoadLock(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if lock.ResolvedSHA != old {
			t.Errorf("lock ResolvedSHA = %q, want pinned %q", lock.ResolvedSHA, old)
		}
		if strings.Contains(stderr.String(), "new inputs") {
			t.Errorf("stderr = %q, want no new-inputs warning", stderr.String())
		}
		if !strings.Contains(stdout.String(), "synced") {
			t.Errorf("stdout = %q, want the synced summary", stdout.String())
		}
	})
	t.Run("unreleased template edit stays invisible to the pin", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		old := storedCommit(t, projectDir)
		regDir := testRegistryDir(t, registryURI)
		if err := os.WriteFile(
			filepath.Join(regDir, "templates", testRegistryTemplate, "greet.txt.tmpl"),
			[]byte("changed {{.project_name}}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		result, err := StateSync(
			context.Background(), projectDir, stateSyncTestOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("StateSync error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi proj\n" {
			t.Errorf("greet.txt = %q, want the pinned snapshot render", got)
		}
		if result.Old != old || result.New != old {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, old)
		}
	})
	t.Run("released template change keeps the pin", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		old := storedCommit(t, projectDir)
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "greet.txt.tmpl", "changed {{.project_name}}\n")
		var stdout, stderr strings.Builder
		result, err := StateSync(
			context.Background(), projectDir, stateSyncTestOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("StateSync error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi proj\n" {
			t.Errorf("greet.txt = %q, want the pinned render without upstream drift", got)
		}
		if result.Old != old || result.New != old {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, old)
		}
		if got := storedCommit(t, projectDir); got != old {
			t.Errorf("stored commit = %q, want pinned %q", got, old)
		}
	})
	t.Run("dirty guard without force", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		if err := os.WriteFile(
			filepath.Join(projectDir, "greet.txt"), []byte("local edit\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		if _, err := StateSync(
			context.Background(), projectDir, stateSyncTestOptions(&stdout, &stderr)); err == nil {
			t.Error("StateSync on dirty tree succeeded, want guard error")
		} else if !strings.Contains(err.Error(), "--force") {
			t.Errorf("error = %q, want it to mention --force", err)
		}
		opts := stateSyncTestOptions(&stdout, &stderr)
		opts.Force = true
		if _, err := StateSync(context.Background(), projectDir, opts); err != nil {
			t.Errorf("StateSync with --force error: %v", err)
		}
	})
	t.Run("preview leaves tree and state alone", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		old := storedCommit(t, projectDir)
		editStoredData(t, projectDir, "project_name", "previewed")
		var stdout, stderr strings.Builder
		opts := stateSyncTestOptions(&stdout, &stderr)
		opts.Preview = true
		result, err := StateSync(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("StateSync preview error: %v", err)
		}
		if result.Old != old || result.New != old {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, old)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi proj\n" {
			t.Errorf("greet.txt = %q, want preview to leave the tree alone", got)
		}
		if !strings.Contains(stdout.String(), "preview") {
			t.Errorf("stdout = %q, want the preview plan", stdout.String())
		}
	})
}

func TestStateSyncFailures(t *testing.T) {
	t.Run("missing state errors", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if _, err := StateSync(
			context.Background(), t.TempDir(),
			stateSyncTestOptions(&stdout, &stderr)); err == nil {
			t.Error("StateSync without state succeeded, want error")
		}
	})
	t.Run("tampered pinned digest errors", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		state.TemplateDigest = "sha256:" + strings.Repeat("0", 64)
		state.ResolvedCommit = strings.Repeat("0", 64)
		if err := config.SaveState(projectDir, state); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		if _, err := StateSync(
			context.Background(), projectDir,
			stateSyncTestOptions(&stdout, &stderr)); err == nil {
			t.Error("StateSync with bogus pinned commit succeeded, want error")
		}
	})
	t.Run("bad conflict rejected", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		var stdout, stderr strings.Builder
		opts := stateSyncTestOptions(&stdout, &stderr)
		opts.Conflict = "merge"
		if _, err := StateSync(context.Background(), projectDir, opts); err == nil {
			t.Error("StateSync with bad --conflict succeeded, want error")
		}
	})
}

func TestStateInput(t *testing.T) {
	t.Run("interactive reprompt defaults from stored values", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		var stdout, stderr strings.Builder
		opts := stateInputTestOptions(&stdout, &stderr)
		opts.Stdin = strings.NewReader("newproj\n\n")
		result, err := StateInput(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("StateInput error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi newproj\n" {
			t.Errorf("greet.txt = %q, want re-answered render", got)
		}
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if state.Data["project_name"] != "newproj" {
			t.Errorf("stored project_name = %v, want newproj", state.Data["project_name"])
		}
		if !strings.Contains(stdout.String(), "project_name") {
			t.Errorf("stdout = %q, want the changed-keys line", stdout.String())
		}
		if result.Old != result.New {
			t.Errorf("Old/New = %q/%q, want pinned identity kept", result.Old, result.New)
		}
	})
	t.Run("accepting every default leaves data alone", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		var stdout, stderr strings.Builder
		opts := stateInputTestOptions(&stdout, &stderr)
		opts.Stdin = strings.NewReader("\n\n")
		if _, err := StateInput(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("StateInput error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi proj\n" {
			t.Errorf("greet.txt = %q, want unchanged render", got)
		}
		if out := stdout.String(); !strings.Contains(out, "input unchanged") {
			t.Errorf("stdout = %q, want the unchanged line", out)
		}
	})
	t.Run("non-interactive without data fails", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		var stdout, stderr strings.Builder
		opts := stateInputTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		if _, err := StateInput(context.Background(), projectDir, opts); err == nil {
			t.Error("StateInput non-interactive without data succeeded, want error")
		} else if !strings.Contains(err.Error(), "--data-file") {
			t.Errorf("error = %q, want it to mention --data-file", err)
		}
	})
	t.Run("non-interactive data-file overlay re-renders", func(t *testing.T) {
		projectDir, _ := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		old := storedCommit(t, projectDir)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(
			dataFile, []byte("project_name: overlaid\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := stateInputTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		opts.DataFile = dataFile
		result, err := StateInput(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("StateInput error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi overlaid\n" {
			t.Errorf("greet.txt = %q, want overlaid re-render", got)
		}
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if state.Data["project_name"] != "overlaid" {
			t.Errorf("stored project_name = %v, want overlaid", state.Data["project_name"])
		}
		if result.Old != old || result.New != old {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, old)
		}
	})
	t.Run("unreleased template edit stays invisible to the pin", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, stateSyncManifest, stateSyncFiles)
		old := storedCommit(t, projectDir)
		regDir := testRegistryDir(t, registryURI)
		if err := os.WriteFile(
			filepath.Join(regDir, "templates", testRegistryTemplate, "greet.txt.tmpl"),
			[]byte("changed {{.project_name}}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := stateInputTestOptions(&stdout, &stderr)
		opts.Stdin = strings.NewReader("\n\n")
		result, err := StateInput(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("StateInput error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if string(got) != "hi proj\n" {
			t.Errorf("greet.txt = %q, want the pinned snapshot render", got)
		}
		if result.Old != old || result.New != old {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, old)
		}
	})
}

func TestInputValuesEqual(t *testing.T) {
	tests := []struct {
		name  string
		first any
		next  any
		want  bool
	}{
		{name: "equal strings", first: "a", next: "a", want: true},
		{name: "different strings", first: "a", next: "b", want: false},
		{name: "string slice against yaml sequence", first: []string{"a", "b"}, next: []any{"a", "b"}, want: true},
		{name: "different sequences", first: []string{"a"}, next: []any{"b"}, want: false},
		{name: "scalar stringified", first: 8080, next: "8080", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inputValuesEqual(tt.first, tt.next); got != tt.want {
				t.Errorf("inputValuesEqual(%v, %v) = %v, want %v",
					tt.first, tt.next, got, tt.want)
			}
		})
	}
}
