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
			src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
			projectDir := bootstrapProject(t, src)
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
	tests := []struct {
		name string
		git  bool
	}{
		{name: "filesystem old to new", git: false},
		{name: "local-git old to new", git: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var src, projectDir string
			var updateRepo func(name, content string)
			if tt.git {
				var repoDir string
				repoDir, _ = initTemplateRepo(t, map[string]string{
					"take.txt": "old\n",
					"keep.txt": "base\n",
					"drop.txt": "bye\n",
					"stay.txt": "hi\n",
				})
				src = repoDir
				projectDir = bootstrapProject(t, src)
				updateRepo = func(name, content string) {
					updateTemplateFile(t, repoDir, name, content)
				}
			} else {
				src = makeTemplate(t, "", map[string]string{
					"take.txt": "old\n",
					"keep.txt": "base\n",
					"drop.txt": "bye\n",
					"stay.txt": "hi\n",
				})
				projectDir = bootstrapProject(t, src)
				updateRepo = func(name, content string) {
					if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(projectDir, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			updateRepo("take.txt", "new\n")
			updateRepo("new.txt", "fresh\n")
			if tt.git {
				if err := os.Remove(filepath.Join(src, "drop.txt")); err != nil {
					t.Fatal(err)
				}
				commitAll(t, src, "drop")
			} else {
				if err := os.Remove(filepath.Join(src, "drop.txt")); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr strings.Builder
			opts := updateTestOptions(&stdout, &stderr)
			opts.Force = true
			result, err := Update(context.Background(), projectDir, opts)
			if err != nil {
				t.Fatalf("Update error: %v", err)
			}
			if got, err := os.ReadFile(filepath.Join(projectDir, "take.txt")); err != nil || string(got) != "new\n" {
				t.Errorf("take.txt = %q, want template-new content", got)
			}
			if got, err := os.ReadFile(filepath.Join(projectDir, "keep.txt")); err != nil || string(got) != "local keep\n" {
				t.Errorf("keep.txt = %q, want local content kept", got)
			}
			if got, err := os.ReadFile(filepath.Join(projectDir, "new.txt")); err != nil || string(got) != "fresh\n" {
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
			src := makeTemplate(t, "", map[string]string{"conflict.txt": "a\nb\nc\n"})
			projectDir := bootstrapProject(t, src)
			if err := os.WriteFile(filepath.Join(projectDir, "conflict.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("a\nREMOTE\nc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
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
			got, err := os.ReadFile(filepath.Join(projectDir, "conflict.txt"))
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
				rej, err := os.ReadFile(filepath.Join(projectDir, "conflict.txt.rej"))
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
		src := makeTemplate(t, "", map[string]string{"img.bin": binaryOld})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "img.bin"), []byte(binaryLocal), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "img.bin"), []byte(binaryNew), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "img.bin")); string(got) != binaryLocal {
			t.Errorf("img.bin = %q, want local binary kept", got)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "img.bin.new")); err != nil || string(got) != binaryNew {
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
		src := makeTemplate(t, "", map[string]string{"img.bin": binaryOld})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "img.bin"), []byte(binaryNew), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "img.bin")); string(got) != binaryNew {
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
			src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
			projectDir := bootstrapProject(t, src)
			if err := os.WriteFile(filepath.Join(src, "img.bin"), []byte(binaryFresh), 0o600); err != nil {
				t.Fatal(err)
			}
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
			if got, _ := os.ReadFile(filepath.Join(projectDir, "img.bin")); string(got) != binaryLocal {
				t.Errorf("img.bin = %q, want local binary kept", got)
			}
			if got, err := os.ReadFile(filepath.Join(projectDir, "img.bin.new")); err != nil || string(got) != binaryFresh {
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
		src := makeTemplate(t, "preserve:\n  - \"keep.txt\"\n", map[string]string{
			"keep.txt":  "template v1\n",
			"merge.txt": "a\nb\nc\n",
		})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, "merge.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("template v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "merge.txt"), []byte("a\nREMOTE\nc\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "keep.txt")); string(got) != "local keep\n" {
			t.Errorf("keep.txt = %q, want local preferred despite template change", got)
		}
		if len(result.Conflicts) != 1 || result.Conflicts[0] != "merge.txt" {
			t.Errorf("Conflicts = %v, want only the non-preserved merge.txt", result.Conflicts)
		}
		got, _ := os.ReadFile(filepath.Join(projectDir, "merge.txt"))
		if !strings.Contains(string(got), "<<<<<<< local") {
			t.Errorf("merge.txt missing inline markers:\n%s", got)
		}
	})
}

func TestUpdateMigrations(t *testing.T) {
	t.Run("migrations run in manifest order", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"app.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		manifest := "migrations:\n" +
			"  - run: [\"echo first >> order.txt\"]\n" +
			"  - run: [\"echo second >> order.txt\"]\n"
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "app.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.Stdin = strings.NewReader("y\n")
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Migrations) != 2 {
			t.Fatalf("Migrations = %v, want 2 in order", result.Migrations)
		}
		if result.Migrations[0] != "echo first >> order.txt" || result.Migrations[1] != "echo second >> order.txt" {
			t.Errorf("Migrations = %v, want ordered echo commands", result.Migrations)
		}
		got, err := os.ReadFile(filepath.Join(projectDir, "order.txt"))
		if err != nil {
			t.Fatalf("reading order.txt: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(got)), "\n")
		if len(lines) != 2 || strings.TrimSpace(lines[0]) != "first" || strings.TrimSpace(lines[1]) != "second" {
			t.Errorf("order.txt = %q, want first-then-second lines", got)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "app.txt")); string(got) != "v2\n" {
			t.Errorf("app.txt = %q, want template-new content alongside migrations", got)
		}
	})
	t.Run("failing migration aborts update", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"app.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		manifest := "migrations:\n  - run: [\"exit 3\"]\n"
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.Stdin = strings.NewReader("y\n")
		if _, err := Update(context.Background(), projectDir, opts); err == nil {
			t.Error("Update with failing migration succeeded, want error")
		}
	})
}

func TestUpdateMigrationsConsent(t *testing.T) {
	setupMigrationProject := func(t *testing.T) (string, string) {
		t.Helper()
		src := makeTemplate(t, "", map[string]string{"app.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		manifest := "migrations:\n  - run: [\"echo hi >> marker.txt\"]\n"
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "app.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return src, projectDir
	}
	t.Run("non-interactive without allow-hooks refuses", func(t *testing.T) {
		_, projectDir := setupMigrationProject(t)
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.NonInteractive = true
		_, err := Update(context.Background(), projectDir, opts)
		if err == nil {
			t.Fatal("Update with migrations in non-interactive mode succeeded, want refusal")
		}
		if !strings.Contains(err.Error(), "--allow-hooks") {
			t.Errorf("error = %q, want it to mention --allow-hooks", err)
		}
		if _, statErr := os.Stat(filepath.Join(projectDir, "marker.txt")); !os.IsNotExist(statErr) {
			t.Error("marker.txt exists, want the refused migration skipped")
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "app.txt")); string(got) != "v1\n" {
			t.Errorf("app.txt = %q, want old content kept after refusal", got)
		}
	})
	t.Run("non-interactive with allow-hooks runs", func(t *testing.T) {
		_, projectDir := setupMigrationProject(t)
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.NonInteractive = true
		opts.AllowHooks = true
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Migrations) != 1 {
			t.Fatalf("Migrations = %v, want the single consented command", result.Migrations)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "marker.txt")); err != nil || strings.TrimSpace(string(got)) != "hi" {
			t.Errorf("marker.txt = %q, want the migration output", got)
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "app.txt")); string(got) != "v2\n" {
			t.Errorf("app.txt = %q, want template-new content", got)
		}
	})
	t.Run("interactive consent runs migrations", func(t *testing.T) {
		_, projectDir := setupMigrationProject(t)
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.Stdin = strings.NewReader("y\n")
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Migrations) != 1 {
			t.Errorf("Migrations = %v, want the consented command", result.Migrations)
		}
		if got, err := os.ReadFile(filepath.Join(projectDir, "marker.txt")); err != nil || strings.TrimSpace(string(got)) != "hi" {
			t.Errorf("marker.txt = %q, want the migration output", got)
		}
	})
	t.Run("interactive decline skips migrations", func(t *testing.T) {
		_, projectDir := setupMigrationProject(t)
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.Stdin = strings.NewReader("n\n")
		result, err := Update(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Migrations) != 0 {
			t.Errorf("Migrations = %v, want none after declined consent", result.Migrations)
		}
		if _, statErr := os.Stat(filepath.Join(projectDir, "marker.txt")); !os.IsNotExist(statErr) {
			t.Error("marker.txt exists, want the declined migration skipped")
		}
		if got, _ := os.ReadFile(filepath.Join(projectDir, "app.txt")); string(got) != "v2\n" {
			t.Errorf("app.txt = %q, want template-new content despite declined migrations", got)
		}
		if !strings.Contains(stdout.String(), "skipped (consent declined)") {
			t.Errorf("stdout = %q, want the skip notice", stdout.String())
		}
	})
	t.Run("non-interactive without migrations passes", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"app.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "app.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.NonInteractive = true
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Errorf("Update error: %v", err)
		}
	})
}

func TestUpdateLockAdvances(t *testing.T) {
	tests := []struct {
		name string
		git  bool
	}{
		{name: "filesystem identity advances", git: false},
		{name: "local-git commit advances", git: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var src, projectDir, old string
			if tt.git {
				repoDir, sha := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
				src = repoDir
				old = sha
				projectDir = bootstrapProject(t, src)
				updateTemplateFile(t, repoDir, "hello.txt", "v2\n")
			} else {
				src = makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
				projectDir = bootstrapProject(t, src)
				state, err := config.LoadState(os.DirFS(projectDir))
				if err != nil {
					t.Fatal(err)
				}
				old = state.ResolvedCommit
				if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
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
			state, err := config.LoadState(os.DirFS(projectDir))
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
			_ = src
		})
	}
}

func TestUpdateAnswersOverlay(t *testing.T) {
	t.Run("answers file overlay re-renders", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"greet.txt.tmpl": "hi {{.project_name}}\n"})
		projectDir := bootstrapProject(t, src)
		got, _ := os.ReadFile(filepath.Join(projectDir, "greet.txt"))
		if string(got) != "hi proj\n" {
			t.Fatalf("greet.txt = %q, want defaulted bootstrap render", got)
		}
		answersFile := filepath.Join(t.TempDir(), "answers.yml")
		if err := os.WriteFile(answersFile, []byte("project_name: overlaid\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "greet.txt.tmpl"), []byte("hey {{.project_name}}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		opts.AnswersFile = answersFile
		if _, err := Update(context.Background(), projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		got, _ = os.ReadFile(filepath.Join(projectDir, "greet.txt"))
		if string(got) != "hey overlaid\n" {
			t.Errorf("greet.txt = %q, want overlaid re-render", got)
		}
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if state.Answers["project_name"] != "overlaid" {
			t.Errorf("stored answers = %v, want overlaid project_name", state.Answers)
		}
	})
	t.Run("missing key suggests answers file", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"a.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "b.txt.tmpl"), []byte("{{.brand_new_key}}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
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
		src := makeTemplate(t, "", map[string]string{"a.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "b.txt.tmpl"), []byte("{{.brand_new_key}}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return src, projectDir
	}
	tests := []struct {
		name           string
		stdin          string
		nonInteractive bool
		wantErr        string
		wantFile       string
	}{
		{
			name:     "interactive prompts for new key",
			stdin:    "prompted\n",
			wantFile: "prompted\n",
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
				if !strings.Contains(err.Error(), "--answers-file") {
					t.Errorf("error = %q, want the --answers-file hint", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Update error: %v", err)
			}
			if got, _ := os.ReadFile(filepath.Join(projectDir, "b.txt")); string(got) != tt.wantFile {
				t.Errorf("b.txt = %q, want prompted render %q", got, tt.wantFile)
			}
			if !strings.Contains(stdout.String(), "Enter value for brand_new_key") {
				t.Errorf("stdout = %q, want the key-name prompt", stdout.String())
			}
			state, err := config.LoadState(os.DirFS(projectDir))
			if err != nil {
				t.Fatal(err)
			}
			if state.Answers["brand_new_key"] != "prompted" {
				t.Errorf("stored answers = %v, want prompted brand_new_key", state.Answers)
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
		{name: "missing state errors", mutate: func(t *testing.T, projectDir string, opts *UpdateOptions) {}, errPart: ""},
		{name: "bad conflict rejected", mutate: func(t *testing.T, _ string, opts *UpdateOptions) { opts.Conflict = "merge" }, errPart: "conflict"},
		{name: "ref rejected for filesystem", mutate: func(t *testing.T, _ string, opts *UpdateOptions) { opts.Ref = "v1" }, errPart: "ref"},
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
				src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
				projectDir = bootstrapProject(t, src)
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
		{name: "stored bootstrap choice applies when silent", flag: "", manifest: "", stored: "rej", want: ConflictRej},
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
			src := makeTemplate(t, manifest, map[string]string{"conflict.txt": "a\nb\nc\n"})
			projectDir := bootstrapProject(t, src)
			if err := os.WriteFile(filepath.Join(projectDir, "conflict.txt"), []byte("a\nLOCAL\nc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "conflict.txt"), []byte("a\nREMOTE\nc\n"), 0o600); err != nil {
				t.Fatal(err)
			}
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

func TestMatchesMigration(t *testing.T) {
	tests := []struct {
		name  string
		from  string
		old   string
		fresh string
		want  bool
	}{
		{name: "empty always applies", from: "", old: "aaa", fresh: "bbb", want: true},
		{name: "old prefix matches", from: "aaa", old: "aaabbb", fresh: "ccc", want: true},
		{name: "SHA identities cannot filter a range", from: "<2.0.0", old: "aaa", fresh: "bbb", want: true},
		{name: "range applies when fresh satisfies", from: ">=2.0.0", old: "v1.9.0", fresh: "v2.1.0", want: true},
		{name: "range applies when old satisfies", from: "<2.0.0", old: "v1.5.0", fresh: "v2.1.0", want: true},
		{name: "range skips when neither version satisfies", from: ">=2.0.0", old: "v1.0.0", fresh: "v1.5.0", want: false},
		{name: "range skips caret mismatch", from: "^1.0", old: "v2.0.0", fresh: "v2.1.0", want: false},
		{name: "unparsable from matches all", from: "not-a-range", old: "v9.9.9", fresh: "v9.9.9", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesMigration(tt.from, tt.old, tt.fresh); got != tt.want {
				t.Errorf("matchesMigration(%q) = %v, want %v", tt.from, got, tt.want)
			}
		})
	}
}

func manifestWithConflict(mode string) *config.Manifest {
	return &config.Manifest{Conflict: mode}
}
