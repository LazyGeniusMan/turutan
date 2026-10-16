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

func updateTestOptions(stdout, stderr *strings.Builder) UpdateOptions {
	return UpdateOptions{
		Force:  false,
		Engine: "0.1.0",
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  strings.NewReader(""),
	}
}

func TestUpdateDirtyGuard(t *testing.T) {
	tests := []struct {
		name      string
		makeDirty bool
		force     bool
		wantErr   bool
		errPart   string
	}{
		{name: "clean tree updates", makeDirty: false, force: false, wantErr: false},
		{name: "dirty tree fails without force", makeDirty: true, force: false, wantErr: true, errPart: "--force"},
		{name: "dirty tree passes with force", makeDirty: true, force: true, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir, _ := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
			if tt.makeDirty {
				if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("local edit\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Force = tt.force
			_, err := Update(context.Background(), projectDir, opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Update succeeded, want dirty-tree error")
				}
				if tt.errPart != "" && !strings.Contains(err.Error(), tt.errPart) {
					t.Errorf("error = %q, want it to mention %q", err, tt.errPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("Update error: %v", err)
			}
		})
	}
}

func TestUpdateMerge(t *testing.T) {
	t.Run("registry old to new", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{
			"take.txt": "old\n",
			"keep.txt": "base\n",
			"drop.txt": "bye\n",
			"stay.txt": "hi\n",
		})
		updateRepo := func(name, content string) {
			updateTestTemplateFile(t, registryURI, testRegistryTemplate, name, content)
		}
		if err := os.WriteFile(filepath.Join(projectDir, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		updateRepo("take.txt", "new\n")
		updateRepo("new.txt", "fresh\n")
		removeTestTemplateFile(t, registryURI, testRegistryTemplate, "drop.txt")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "take.txt")); err != nil || string(got) != "new\n" { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("take.txt = %q, want template-new content", got)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "keep.txt")); err != nil || string(got) != "local keep\n" { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("keep.txt = %q, want local content kept", got)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "new.txt")); err != nil || string(got) != "fresh\n" { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("new.txt = %q, want new template file written", got)
		}
		if _, err := os.Stat(filepath.Join(projectDir, "drop.txt")); !os.IsNotExist(err) {
			t.Error("drop.txt still exists, want it deleted (unchanged local copy)")
		}
		if result.Old == result.New {
			t.Error("Old == New, want the identity to advance")
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none for disjoint changes", result.Conflicts)
		}
	})
}

func TestUpdateConflicts(t *testing.T) {
	tests := []struct {
		name     string
		conflict ConflictMode
	}{
		{name: "inline markers", conflict: ConflictInline},
		{name: "rej sidecar", conflict: ConflictRej},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir, registryURI := initRegistryProject(t, "", map[string]string{"conflict.txt": "a\nb\nc\n"})
			if err := os.WriteFile(filepath.Join(projectDir, "conflict.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			updateTestTemplateFile(t, registryURI, testRegistryTemplate, "conflict.txt", "a\nREMOTE\nc\n")
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Force = true
			opts.Conflict = tt.conflict
			result, err := Update(context.Background(), projectDir, opts)
			if err != nil {
				t.Fatalf("Update error: %v", err)
			}
			if len(result.Conflicts) != 1 || result.Conflicts[0] != "conflict.txt" {
				t.Fatalf("Conflicts = %v, want [conflict.txt]", result.Conflicts)
			}
			got, err := os.ReadFile(filepath.Join(projectDir, "conflict.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			if err != nil {
				t.Fatal(err)
			}
			if tt.conflict == ConflictInline {
				for _, want := range []string{"<<<<<<< local", "=======", ">>>>>>> template-", "LOCAL", "REMOTE"} {
					if !strings.Contains(string(got), want) {
						t.Errorf("inline file missing %q:\n%s", want, got)
					}
				}
				if _, err := os.Stat(filepath.Join(projectDir, "conflict.txt.rej")); !os.IsNotExist(err) {
					t.Error("unexpected .rej file in inline mode")
				}
			} else {
				if string(got) != "a\nLOCAL\nc\n" {
					t.Errorf("rej mode file = %q, want local body kept", got)
				}
				rej, err := os.ReadFile(filepath.Join(projectDir, "conflict.txt.rej")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
				if err != nil {
					t.Fatalf("reading .rej: %v", err)
				}
				for _, want := range []string{"@@", "-LOCAL", "+REMOTE"} {
					if !strings.Contains(string(rej), want) {
						t.Errorf(".rej missing %q:\n%s", want, rej)
					}
				}
			}
		})
	}
}

func TestUpdateBinary(t *testing.T) {
	binaryOld := "GIF89a\x00old"
	binaryNew := "GIF89a\x00new"
	binaryLocal := "GIF89a\x00local"
	t.Run("both changed keeps local with .new", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"img.bin": binaryOld})
		if err := os.WriteFile(filepath.Join(projectDir, "img.bin"), []byte(binaryLocal), 0o600); err != nil {
			t.Fatal(err)
		}
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "img.bin", string(binaryNew))
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "img.bin")); string(got) != binaryLocal { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("img.bin = %q, want local binary kept", got)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "img.bin.new")); err != nil || string(got) != binaryNew { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("img.bin.new = %q, want template-new sidecar", got)
		}
		if len(result.Conflicts) != 1 {
			t.Errorf("Conflicts = %v, want the binary recorded", result.Conflicts)
		}
		if !strings.Contains(stderr.String(), "binary") {
			t.Errorf("stderr = %q, want a binary warning", stderr.String())
		}
	})
	t.Run("template only takes new without sidecar", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"img.bin": binaryOld})
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "img.bin", string(binaryNew))
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "img.bin")); string(got) != binaryNew { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("img.bin = %q, want template-new content", got)
		}
		if _, err := os.Stat(filepath.Join(projectDir, "img.bin.new")); !os.IsNotExist(err) {
			t.Error("unexpected .new sidecar when local was unchanged")
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none", result.Conflicts)
		}
	})
}

func TestUpdateMergeAddedBinary(t *testing.T) {
	binaryFresh := "GIF89a\x00fresh"
	binaryLocal := "GIF89a\x00local"
	tests := []struct {
		name     string
		conflict ConflictMode
	}{
		{name: "inline mode sidecars", conflict: ConflictInline},
		{name: "rej mode sidecars without .rej", conflict: ConflictRej},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir, registryURI := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
			updateTestTemplateFile(t, registryURI, testRegistryTemplate, "img.bin", string(binaryFresh))
			if err := os.WriteFile(filepath.Join(projectDir, "img.bin"), []byte(binaryLocal), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Conflict = tt.conflict
			result, err := Update(context.Background(), projectDir, opts)
			if err != nil {
				t.Fatalf("Update error: %v", err)
			}
			if got, _ := os.ReadFile(filepath.Join(projectDir, "img.bin")); string(got) != binaryLocal { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
				t.Errorf("img.bin = %q, want local binary kept", got)
			}
			if got, err := os.ReadFile(filepath.Join(projectDir, "img.bin.new")); err != nil || string(got) != binaryFresh { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
				t.Errorf("img.bin.new = %q, want template-new sidecar", got)
			}
			if _, err := os.Stat(filepath.Join(projectDir, "img.bin.rej")); !os.IsNotExist(err) {
				t.Error("unexpected .rej sidecar for a binary new-file collision")
			}
			if len(result.Conflicts) != 1 || result.Conflicts[0] != "img.bin" {
				t.Errorf("Conflicts = %v, want [img.bin]", result.Conflicts)
			}
			if !strings.Contains(stderr.String(), "binary") {
				t.Errorf("stderr = %q, want a binary warning", stderr.String())
			}
		})
	}
}

func TestUpdatePreserve(t *testing.T) {
	t.Run("preserve globs always prefer local", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "preserves:\n  - \"keep.txt\"\n", map[string]string{
			"keep.txt":  "template v1\n",
			"merge.txt": "a\nb\nc\n",
		})
		if err := os.WriteFile(filepath.Join(projectDir, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, "merge.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "keep.txt", "template v2\n")
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "merge.txt", "a\nREMOTE\nc\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "keep.txt")); string(got) != "local keep\n" { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("keep.txt = %q, want local preferred despite template change", got)
		}
		if len(result.Conflicts) != 1 || result.Conflicts[0] != "merge.txt" {
			t.Errorf("Conflicts = %v, want only the non-preserved merge.txt", result.Conflicts)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "merge.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if !strings.Contains(string(got), "<<<<<<< local") {
			t.Errorf("merge.txt missing inline markers:\n%s", got)
		}
	})
	t.Run("pristine preserved survives template-only change", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "preserves:\n  - \"keep.txt\"\n", map[string]string{
			"keep.txt": "template v1\n",
		})
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "keep.txt", "template v2\n")
		var stdout, stderr strings.Builder
		result, err := Update(context.Background(), projectDir, updateTestOptions(&stdout, &stderr))
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "keep.txt")); string(got) != "template v1\n" { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
			t.Errorf("keep.txt = %q, want pristine local copy kept", got)
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none", result.Conflicts)
		}
		if len(result.Updated) != 0 {
			t.Errorf("Updated = %v, want none", result.Updated)
		}
	})
	t.Run("deleted preserved stays deleted", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "preserves:\n  - \"keep.txt\"\n", map[string]string{
			"keep.txt": "template v1\n",
		})
		if err := os.Remove(filepath.Join(projectDir, "keep.txt")); err != nil {
			t.Fatal(err)
		}
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "keep.txt", "template v2\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(projectDir, "keep.txt")); !os.IsNotExist(err) {
			t.Error("keep.txt resurrected, want locally deleted file kept gone")
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none", result.Conflicts)
		}
	})
}

func TestUpdateLockAdvances(t *testing.T) {
	t.Run("registry digest advances", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		old := state.ResolvedCommit
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "hello.txt", "v2\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if result.Old != old {
			t.Errorf("Old = %q, want %q", result.Old, old)
		}
		if result.Old == result.New {
			t.Error("New == Old, want the identity to advance")
		}
		state, err = config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if state.ResolvedCommit != result.New {
			t.Errorf("state ResolvedCommit = %q, want %q", state.ResolvedCommit, result.New)
		}
		lock, err := config.LoadLock(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if lock.ResolvedSHA != result.New {
			t.Errorf("lock ResolvedSHA = %q, want %q", lock.ResolvedSHA, result.New)
		}
		if lock.Registry != registryURI {
			t.Errorf("lock Registry = %q, want %q", lock.Registry, registryURI)
		}
		if state.Registry != registryURI || state.RequestedRef == "" {
			t.Errorf("state registry/version = %q/%q, want registry tracking", state.Registry, state.RequestedRef)
		}
	})
}

func TestUpdateDataOverlay(t *testing.T) {
	t.Run("data file overlay re-renders", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"greet.txt.tmpl": "hi {{.project_name}}\n"})
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if string(got) != "hi proj\n" {
			t.Fatalf("greet.txt = %q, want defaulted init render", got)
		}
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: overlaid\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "greet.txt.tmpl", "hey {{.project_name}}\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.DataFile = dataFile
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		got, _ = os.ReadFile(filepath.Join(projectDir, "greet.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if string(got) != "hey overlaid\n" {
			t.Errorf("greet.txt = %q, want overlaid re-render", got)
		}
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if state.Data["project_name"] != "overlaid" {
			t.Errorf("stored data = %v, want overlaid project_name", state.Data)
		}
	})
	t.Run("missing key suggests data file", func(t *testing.T) {
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"a.txt": "v1\n"})
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "b.txt.tmpl", "{{.brand_new_key}}\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		_, err := Update(context.Background(), projectDir, opts)
		if err == nil || !strings.Contains(err.Error(), `"brand_new_key"`) {
			t.Errorf("error = %v, want it to name the missing key", err)
		}
	})
}

func TestUpdateNewTemplateKeys(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		projectDir, registryURI := initRegistryProject(t, "", map[string]string{"a.txt": "v1\n"})
		updateTestTemplateFile(t, registryURI, testRegistryTemplate, "b.txt.tmpl", "{{.brand_new_key}}\n")
		return registryURI, projectDir
	}
	tests := []struct {
		name           string
		stdin          string
		nonInteractive bool
		wantErr        string
		wantFile       string
	}{
		{
			name:     "interactive inputs for new key",
			stdin:    "inputted\n",
			wantFile: "inputted\n",
		},
		{
			name:           "non-interactive fails with key hint",
			stdin:          "",
			nonInteractive: true,
			wantErr:        `"brand_new_key"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, projectDir := setup(t)
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Force = true
			opts.Stdin = strings.NewReader(tt.stdin)
			opts.NonInteractive = tt.nonInteractive
			_, err := Update(context.Background(), projectDir, opts)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("Update succeeded, want missing-key error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to name %s", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "--data-file") {
					t.Errorf("error = %q, want the --data-file hint", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Update error: %v", err)
			}
			if got, _ := os.ReadFile(filepath.Join(projectDir, "b.txt")); string(got) != tt.wantFile { // #nosec G304 -- test-only path under t.TempDir() with a constant segment
				t.Errorf("b.txt = %q, want input render %q", got, tt.wantFile)
			}
			if !strings.Contains(stdout.String(), "Enter brand_new_key") {
				t.Errorf("stdout = %q, want the key-name input", stdout.String())
			}
			state, err := config.LoadState(os.DirFS(projectDir))
			if err != nil {
				t.Fatal(err)
			}
			if state.Data["brand_new_key"] != "inputted" {
				t.Errorf("stored data = %v, want inputted brand_new_key", state.Data)
			}
		})
	}
}

func TestUpdateFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *testing.T, projectDir string, opts *UpdateOptions)
		errPart string
	}{
		{name: "missing state errors", mutate: func(_ *testing.T, _ string, _ *UpdateOptions) {}, errPart: ""},
		{name: "bad conflict rejected", mutate: func(t *testing.T, _ string, opts *UpdateOptions) { opts.Conflict = "merge" }, errPart: "conflict"},
		{name: "bad version rejected", mutate: func(_ *testing.T, _ string, opts *UpdateOptions) { opts.Ref = "not-a-version" }, errPart: "not-a-version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var projectDir string
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Force = true
			if tt.name == "missing state errors" {
				projectDir = t.TempDir()
			} else {
				projectDir, _ = initRegistryProject(t, "", map[string]string{"hello.txt": "v1\n"})
			}
			tt.mutate(t, projectDir, &opts)
			_, err := Update(context.Background(), projectDir, opts)
			if err == nil {
				t.Error("Update succeeded, want error")
				return
			}
			if tt.errPart != "" && !strings.Contains(err.Error(), tt.errPart) {
				t.Errorf("error = %q, want it to mention %q", err, tt.errPart)
			}
		})
	}
}

func TestEffectiveConflict(t *testing.T) {
	tests := []struct {
		name     string
		flag     ConflictMode
		manifest string
		stored   string
		want     ConflictMode
	}{
		{name: "flag wins over manifest", flag: ConflictRej, manifest: "inline", want: ConflictRej},
		{name: "flag wins over stored state", flag: ConflictInline, manifest: "", stored: "rej", want: ConflictInline},
		{name: "manifest default applies", flag: "", manifest: "rej", want: ConflictRej},
		{name: "manifest wins over stored state", flag: "", manifest: "inline", stored: "rej", want: ConflictInline},
		{name: "stored init choice applies when silent", flag: "", manifest: "", stored: "rej", want: ConflictRej},
		{name: "empty defaults to inline", flag: "", manifest: "", want: ConflictInline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &config.State{Conflict: tt.stored}
			got := effectiveConflict(tt.flag, manifestWithConflict(tt.manifest), state)
			if got != tt.want {
				t.Errorf("effectiveConflict = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdateConflictModePersists(t *testing.T) {
	tests := []struct {
		name             string
		manifestConflict string
		flag             ConflictMode
		wantStored       string
		wantRejSidecar   bool
	}{
		{
			name:           "explicit flag persists",
			flag:           ConflictRej,
			wantStored:     "rej",
			wantRejSidecar: true,
		},
		{
			name:             "manifest default resolves without persisting",
			manifestConflict: "rej",
			wantStored:       "",
			wantRejSidecar:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := ""
			if tt.manifestConflict != "" {
				manifest = "conflict: " + tt.manifestConflict + "\n"
			}
			projectDir, registryURI := initRegistryProject(t, manifest, map[string]string{"conflict.txt": "a\nb\nc\n"})
			if err := os.WriteFile(filepath.Join(projectDir, "conflict.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			updateTestTemplateFile(t, registryURI, testRegistryTemplate, "conflict.txt", "a\nREMOTE\nc\n")
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Force = true
			opts.Conflict = tt.flag
			if _, err := Update(context.Background(), projectDir, opts); err != nil {
				t.Fatalf("Update error: %v", err)
			}
			state, err := config.LoadState(os.DirFS(projectDir))
			if err != nil {
				t.Fatal(err)
			}
			if state.Conflict != tt.wantStored {
				t.Errorf("stored conflict = %q, want %q", state.Conflict, tt.wantStored)
			}
			_, err = os.Stat(filepath.Join(projectDir, "conflict.txt.rej"))
			if tt.wantRejSidecar && err != nil {
				t.Errorf("missing .rej sidecar, want rej mode resolved: %v", err)
			}
			if !tt.wantRejSidecar && !os.IsNotExist(err) {
				t.Errorf("unexpected .rej sidecar, want inline mode resolved")
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "plain text is not binary", data: "hello\nworld\n", want: false},
		{name: "empty is not binary", data: "", want: false},
		{name: "nul byte is binary", data: "GIF89a\x00binary", want: true},
		{name: "markers are text", data: "<<<<<<< local\nabc\n=======\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinary([]byte(tt.data)); got != tt.want {
				t.Errorf("isBinary(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestInlineConflict(t *testing.T) {
	tests := []struct {
		name  string
		local string
		fresh string
	}{
		{name: "both end with newline", local: "a\nLOCAL\n", fresh: "a\nREMOTE\n"},
		{name: "missing trailing newlines", local: "a\nLOCAL", fresh: "a\nREMOTE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(inlineConflict([]byte(tt.local), []byte(tt.fresh), "abc123"))
			for _, want := range []string{"<<<<<<< local", "=======", ">>>>>>> template-abc123", "LOCAL", "REMOTE"} {
				if !strings.Contains(got, want) {
					t.Errorf("inline conflict missing %q:\n%s", want, got)
				}
			}
			if !strings.HasSuffix(got, "\n") {
				t.Errorf("inline conflict = %q, want trailing newline", got)
			}
		})
	}
}

func manifestWithConflict(mode string) *config.Manifest {
	return &config.Manifest{Conflict: mode}
}

const profileTestManifest = "inputs:\n" +
	"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: my-proj\n" +
	"  profiles:\n    label: \"Profiles\"\n    type: array\n    default: []\n" +
	"conditionals:\n" +
	"  - if: '{{not (contains .profiles \"go\")}}'\n    excludes: [\"go.mod\", \"cmd/**\"]\n" +
	"  - if: '{{not (contains .profiles \"github\")}}'\n    excludes: [\".github/**\"]\n"

var profileTestFiles = map[string]string{
	"keep.txt":         "keep v1\n",
	"version.txt.tmpl": "{{.project_name}} v1\n",
	"go.mod":           "module example\n",
	"cmd/app/main.go":  "package main\n",
	".github/ci.yml":   "ci\n",
}

func initRegistryProfiles(t *testing.T, manifest string, files map[string]string, profiles ...string) (string, string) {
	t.Helper()
	registryURI, ref := makeTestRegistry(t, manifest, files)
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	opts := testOptions(&stdout)
	if len(profiles) > 0 {
		opts.DataFile = profilesDataFile(t, profiles...)
	}
	mustInitTestProject(t, target, ref, opts)
	return target, registryURI
}

func updateProfiles(t *testing.T, _ string, projectDir string, profiles ...string) *UpdateResult {
	t.Helper()
	var stdout, stderr strings.Builder
	opts := updateTestOptions(&stdout, &stderr)
	opts.Force = true
	opts.NonInteractive = true
	if len(profiles) > 0 {
		opts.DataFile = profilesDataFile(t, profiles...)
	}
	result, err := Update(context.Background(), projectDir, opts)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	return result
}

func storedProfiles(t *testing.T, projectDir string) []string {
	t.Helper()
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	if state.Data == nil || state.Data["profiles"] == nil {
		return nil
	}
	return normalizeArrayValue(state.Data["profiles"])
}

func lockPaths(t *testing.T, projectDir string) []string {
	t.Helper()
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(lock.Files))
	for _, entry := range lock.Files {
		paths = append(paths, entry.Path)
	}
	return paths
}

func assertProfileFile(t *testing.T, projectDir, rel, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(rel))) // #nosec G304 -- reads a fixture path assembled by the test from a constant segment, not external input
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", rel, got, want)
	}
}

func assertProfileAbsent(t *testing.T, projectDir, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Errorf("%s exists, want it absent", rel)
	}
}

func assertCheckQuiet(t *testing.T, _ string, projectDir string) {
	t.Helper()
	var stdout strings.Builder
	result, err := Check(context.Background(), projectDir, checkOptions(&stdout))
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if result.Available {
		t.Error("Check reports available after a profiles-input update, want up to date")
	}
}

func TestUpdateAddProfilesWithoutTemplateChange(t *testing.T) {
	projectDir, registryURI := initRegistryProfiles(t, profileTestManifest, profileTestFiles, "core")
	for _, rel := range []string{"go.mod", "cmd/app/main.go", ".github/ci.yml"} {
		assertProfileAbsent(t, projectDir, rel)
	}
	result := updateProfiles(t, registryURI, projectDir, "core", "go")
	assertProfileFile(t, projectDir, "go.mod", "module example\n")
	assertProfileFile(t, projectDir, "cmd/app/main.go", "package main\n")
	assertProfileAbsent(t, projectDir, ".github/ci.yml")
	assertProfileFile(t, projectDir, "version.txt", "proj v1\n")
	paths := lockPaths(t, projectDir)
	for _, rel := range []string{"go.mod", "cmd/app/main.go", "keep.txt", "version.txt"} {
		if !slices.Contains(paths, rel) {
			t.Errorf("lock is missing %q after adding the go profile", rel)
		}
	}
	if slices.Contains(paths, ".github/ci.yml") {
		t.Error("lock contains .github/ci.yml without the github profile")
	}
	if !slices.Contains(result.Updated, "go.mod") {
		t.Errorf("Updated = %v, want it to list the added go.mod", result.Updated)
	}
	if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go"}) {
		t.Errorf("stored profiles = %v, want [core go]", got)
	}
	assertCheckQuiet(t, registryURI, projectDir)
}

func TestUpdateRemoveProfilesWithoutTemplateChange(t *testing.T) {
	t.Run("unmodified dropped files are removed with empty dirs pruned", func(t *testing.T) {
		projectDir, registryURI := initRegistryProfiles(t, profileTestManifest, profileTestFiles, "core", "go")
		assertProfileFile(t, projectDir, "go.mod", "module example\n")
		result := updateProfiles(t, registryURI, projectDir, "core")
		assertProfileAbsent(t, projectDir, "go.mod")
		assertProfileAbsent(t, projectDir, "cmd/app/main.go")
		assertProfileAbsent(t, projectDir, "cmd/app")
		assertProfileAbsent(t, projectDir, "cmd")
		assertProfileFile(t, projectDir, "keep.txt", "keep v1\n")
		assertProfileFile(t, projectDir, "version.txt", "proj v1\n")
		paths := lockPaths(t, projectDir)
		for _, rel := range []string{"go.mod", "cmd/app/main.go"} {
			if slices.Contains(paths, rel) {
				t.Errorf("lock still contains %q after removing the go profile", rel)
			}
		}
		if !slices.Contains(result.Updated, "go.mod") {
			t.Errorf("Updated = %v, want it to list the dropped go.mod", result.Updated)
		}
		if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core"}) {
			t.Errorf("stored profiles = %v, want [core]", got)
		}
		assertCheckQuiet(t, registryURI, projectDir)
	})
	t.Run("locally modified dropped file is kept on disk but untracked", func(t *testing.T) {
		projectDir, registryURI := initRegistryProfiles(t, profileTestManifest, profileTestFiles, "core", "go")
		mainPath := filepath.Join(projectDir, "cmd", "app", "main.go")
		if err := os.WriteFile(mainPath, []byte("package main // local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		updateProfiles(t, registryURI, projectDir, "core")
		assertProfileAbsent(t, projectDir, "go.mod")
		assertProfileFile(t, projectDir, "cmd/app/main.go", "package main // local\n")
		if paths := lockPaths(t, projectDir); slices.Contains(paths, "cmd/app/main.go") {
			t.Error("lock still contains locally modified cmd/app/main.go, want it untracked")
		}
		if info, err := os.Stat(filepath.Join(projectDir, "cmd", "app")); err != nil || !info.IsDir() {
			t.Error("cmd/app was pruned while it still holds the kept local file")
		}
		assertCheckQuiet(t, registryURI, projectDir)
	})
}

func TestUpdateKeepsStoredProfilesWithoutOverlay(t *testing.T) {
	projectDir, _ := initRegistryProfiles(t, profileTestManifest, profileTestFiles, "core", "go")
	var stdout, stderr strings.Builder
	opts := updateTestOptions(&stdout, &stderr)
	opts.Force = true
	if _, err := Update(context.Background(), projectDir, opts); err != nil {
		t.Fatalf("Update error: %v", err)
	}
	assertProfileFile(t, projectDir, "go.mod", "module example\n")
	assertProfileFile(t, projectDir, "cmd/app/main.go", "package main\n")
	if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go"}) {
		t.Errorf("stored profiles = %v, want unchanged [core go]", got)
	}
}

func TestUpdateTemplateChangeWithAddedProfiles(t *testing.T) {
	projectDir, registryURI := initRegistryProfiles(t, profileTestManifest, profileTestFiles, "core")
	assertProfileAbsent(t, projectDir, "go.mod")
	updateTestTemplateFile(t, registryURI, testRegistryTemplate, "keep.txt", "keep v2\n")
	result := updateProfiles(t, registryURI, projectDir, "core", "go")
	assertProfileFile(t, projectDir, "keep.txt", "keep v2\n")
	assertProfileFile(t, projectDir, "go.mod", "module example\n")
	assertProfileFile(t, projectDir, "cmd/app/main.go", "package main\n")
	assertProfileAbsent(t, projectDir, ".github/ci.yml")
	if len(result.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want none for disjoint template and profile changes", result.Conflicts)
	}
	if result.Old == result.New {
		t.Error("Old == New, want the commit identity to advance")
	}
	if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core", "go"}) {
		t.Errorf("stored profiles = %v, want [core go]", got)
	}
}

func TestUpdateTemplateChangeWithRemovedProfiles(t *testing.T) {
	projectDir, registryURI := initRegistryProfiles(t, profileTestManifest, profileTestFiles, "core", "go", "github")
	ciPath := filepath.Join(projectDir, ".github", "ci.yml")
	if err := os.WriteFile(ciPath, []byte("ci // local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updateTestTemplateFile(t, registryURI, testRegistryTemplate, "keep.txt", "keep v2\n")
	result := updateProfiles(t, registryURI, projectDir, "core")
	assertProfileFile(t, projectDir, "keep.txt", "keep v2\n")
	assertProfileAbsent(t, projectDir, "go.mod")
	assertProfileAbsent(t, projectDir, "cmd")
	assertProfileFile(t, projectDir, ".github/ci.yml", "ci // local\n")
	paths := lockPaths(t, projectDir)
	for _, rel := range []string{"go.mod", "cmd/app/main.go", ".github/ci.yml"} {
		if slices.Contains(paths, rel) {
			t.Errorf("lock still contains %q after removing its profile", rel)
		}
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want none", result.Conflicts)
	}
	if result.Old == result.New {
		t.Error("Old == New, want the commit identity to advance")
	}
	if got := storedProfiles(t, projectDir); !slices.Equal(got, []string{"core"}) {
		t.Errorf("stored profiles = %v, want [core]", got)
	}
}

func TestUpdateBackfillsNewInputDefault(t *testing.T) {
	projectDir, registryURI := initRegistryProject(t, "", map[string]string{"app.txt": "v1\n"})
	writeTestRegistryFiles(t, registryURI, testRegistryTemplate, map[string]string{
		"template.yml": "inputs:\n" +
			"  project_name:\n    label: \"Project Name\"\n    type: string\n    default: my-proj\n" +
			"  new_feature:\n    label: \"New feature\"\n    type: array\n" +
			"    default: [\"alpha\", \"beta\"]\n",
		"feat.txt.tmpl": "[{{ range $i, $p := .new_feature }}{{ if $i }}, {{ end }}\"{{ $p }}\"{{ end }}]\n",
	})
	releaseTestTemplate(t, testRegistryDir(t, registryURI), testRegistryTemplate)
	t.Run("plain non-interactive update uses the new default", func(t *testing.T) {
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		assertProfileFile(t, projectDir, "feat.txt", "[\"alpha\", \"beta\"]\n")
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		got := normalizeArrayValue(state.Data["new_feature"])
		if !slices.Equal(got, []string{"alpha", "beta"}) {
			t.Errorf("stored new_feature = %v (%T), want [alpha beta]", state.Data["new_feature"], state.Data["new_feature"])
		}
	})
	t.Run("diff succeeds on the backfilled checkout", func(t *testing.T) {
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 0 {
			t.Errorf("diffs = %+v, want none after the backfilled update", diffs)
		}
	})
	t.Run("check reports up to date", func(t *testing.T) {
		var stdout strings.Builder
		result, err := Check(context.Background(), projectDir, checkOptions(&stdout))
		if err != nil {
			t.Fatalf("Check error: %v", err)
		}
		if result.Available {
			t.Error("Available = true after the backfilled update, want up to date")
		}
	})
}
