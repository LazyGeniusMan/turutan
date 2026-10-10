// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitpkg "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/assert"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func commitAll(t *testing.T, repoDir, message string) string {
	t.Helper()
	repo, err := gitpkg.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("."); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit(message, &gitpkg.CommitOptions{
		Author: &object.Signature{Name: "turutan-test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash.String()
}

func initTemplateRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := gitpkg.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Commit.GpgSign = gitconfig.OptBoolFalse
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, files)
	return dir, commitAll(t, dir, "template")
}

func updateTemplateFile(t *testing.T, repoDir, name, content string) string {
	t.Helper()
	writeFiles(t, repoDir, map[string]string{name: content})
	return commitAll(t, repoDir, "update "+name)
}

func bootstrapProject(t *testing.T, src string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	if err := Bootstrap(context.Background(), src, target, testOptions(&stdout)); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	return target
}

func checkUpdateOptions(stdout *strings.Builder) CheckUpdateOptions {
	return CheckUpdateOptions{Stdout: stdout}
}

func TestCheckUpdateLocalGit(t *testing.T) {
	t.Run("up to date on clean bootstrap", func(t *testing.T) {
		repoDir, sha := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, repoDir)
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false on clean bootstrap")
		}
		if result.Old != sha || result.New != sha {
			t.Errorf("Old/New = %q/%q, want both %q", result.Old, result.New, sha)
		}
		if !strings.Contains(stdout.String(), "up to date") {
			t.Errorf("stdout = %q, want up-to-date line", stdout.String())
		}
	})
	t.Run("new commit reports available", func(t *testing.T) {
		repoDir, old := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, repoDir)
		fresh := updateTemplateFile(t, repoDir, "hello.txt", "v2\n")
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if !result.Available {
			t.Fatal("Available = false, want true after template commit")
		}
		if result.Old != old {
			t.Errorf("Old = %q, want %q", result.Old, old)
		}
		if result.New != fresh {
			t.Errorf("New = %q, want %q", result.New, fresh)
		}
		if !strings.Contains(stdout.String(), "update available") {
			t.Errorf("stdout = %q, want update-available line", stdout.String())
		}
	})
	t.Run("ref override pins old commit", func(t *testing.T) {
		repoDir, old := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, repoDir)
		updateTemplateFile(t, repoDir, "hello.txt", "v2\n")
		var stdout strings.Builder
		opts := checkUpdateOptions(&stdout)
		opts.Ref = old
		result, err := CheckUpdate(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false when --ref pins the stored commit")
		}
	})
	t.Run("unknown ref errors", func(t *testing.T) {
		repoDir, _ := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, repoDir)
		var stdout strings.Builder
		opts := checkUpdateOptions(&stdout)
		opts.Ref = "does-not-exist"
		if _, err := CheckUpdate(context.Background(), projectDir, opts); err == nil {
			t.Error("CheckUpdate with unknown ref succeeded, want error")
		}
	})
}

func TestCheckUpdateRemoteGitOffline(t *testing.T) {
	saveRemoteState := func(t *testing.T, repoDir, sha string) string {
		t.Helper()
		projectDir := t.TempDir()
		state := &config.State{
			Version:         config.StateVersion,
			Template:        repoDir,
			SourceKind:      "remote-git",
			RequestedRef:    "",
			ResolvedCommit:  sha,
			Engine:          "turutan/0.1.0",
			TemplateLicense: config.TemplateLicenseMIT0,
		}
		if err := config.SaveState(projectDir, state); err != nil {
			t.Fatal(err)
		}
		return projectDir
	}
	t.Run("up to date via ls-remote on local path", func(t *testing.T) {
		repoDir, sha := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := saveRemoteState(t, repoDir, sha)
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false")
		}
		if result.New != sha {
			t.Errorf("New = %q, want %q", result.New, sha)
		}
	})
	t.Run("new commit reports available via ls-remote", func(t *testing.T) {
		repoDir, old := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := saveRemoteState(t, repoDir, old)
		fresh := updateTemplateFile(t, repoDir, "hello.txt", "v2\n")
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if !result.Available {
			t.Fatal("Available = false, want true after template commit")
		}
		if result.Old != old || result.New != fresh {
			t.Errorf("Old/New = %q/%q, want %q/%q", result.Old, result.New, old, fresh)
		}
	})
}

func TestCheckUpdateFilesystem(t *testing.T) {
	t.Run("up to date on clean bootstrap", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false on clean bootstrap")
		}
		if result.New != result.Old {
			t.Errorf("New = %q, Old = %q, want equal when up to date", result.New, result.Old)
		}
	})
	t.Run("project-only edit stays up to date", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("local edit\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false: only the project changed, not the template")
		}
	})
	t.Run("template change reports available", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if !result.Available {
			t.Fatal("Available = false, want true after template change")
		}
		if !strings.Contains(stdout.String(), "update available") {
			t.Errorf("stdout = %q, want update-available line", stdout.String())
		}
	})
	t.Run("preserved local edit stays up to date", func(t *testing.T) {
		src := makeTemplate(t, "preserve:\n  - \"keep.txt\"\n", map[string]string{"keep.txt": "template keep\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false: preserved paths carry local content")
		}
	})
	t.Run("ref rejected for filesystem", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		var stdout strings.Builder
		opts := checkUpdateOptions(&stdout)
		opts.Ref = "v1"
		if _, err := CheckUpdate(context.Background(), projectDir, opts); err == nil {
			t.Error("CheckUpdate with --ref on filesystem succeeded, want error")
		}
	})
	t.Run("relative source re-resolves from project", func(t *testing.T) {
		workdir := t.TempDir()
		writeFiles(t, filepath.Join(workdir, "tpl"), map[string]string{"hello.txt": "v1\n"})
		t.Chdir(workdir)
		projectDir := filepath.Join(workdir, "proj")
		var stdout strings.Builder
		if err := Bootstrap(context.Background(), "./tpl", projectDir, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		stored, err := template.ParseSource(state.Template)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(stored.Repo) {
			t.Errorf("stored repo = %q, want absolute so it re-resolves from the project", stored.Repo)
		}
		t.Chdir(projectDir)
		result, err := CheckUpdate(context.Background(), ".", checkUpdateOptions(&stdout))
		if err != nil {
			t.Fatalf("CheckUpdate error: %v", err)
		}
		if result.Available {
			t.Error("Available = true, want false")
		}
		diffs, err := ComputeDiff(context.Background(), ".", diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 0 {
			t.Errorf("diffs = %+v, want none", diffs)
		}
	})
	t.Run("missing state errors", func(t *testing.T) {
		var stdout strings.Builder
		if _, err := CheckUpdate(context.Background(), t.TempDir(), checkUpdateOptions(&stdout)); err == nil {
			t.Error("CheckUpdate without state succeeded, want error")
		}
	})
	t.Run("missing lock errors for filesystem", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.Remove(filepath.Join(projectDir, config.LockFileName)); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		if _, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout)); err == nil {
			t.Error("CheckUpdate without lock succeeded, want error")
		}
	})
}

func TestCheckUpdateFilesystemMinEngine(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		src := makeTemplate(t, "min-engine: \">=0.1.0\"\n", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte("min-engine: \">=99.0.0\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return src, projectDir
	}
	tests := []struct {
		name    string
		engine  string
		wantErr bool
		errPart string
	}{
		{name: "unsatisfiable floor fails fast", engine: "0.1.0", wantErr: true, errPart: "requires engine"},
		{name: "dev version skips gate", engine: "dev", wantErr: false},
		{name: "empty engine skips gate", engine: "", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			_, projectDir := setup(t)
			var stdout strings.Builder
			opts := checkUpdateOptions(&stdout)
			opts.Engine = tt.engine
			_, err := CheckUpdate(context.Background(), projectDir, opts)
			if tt.wantErr {
				if !is.Error(err) {
					return
				}
				is.Contains(err.Error(), tt.errPart)
				return
			}
			if !is.NoError(err) {
				return
			}
		})
	}
}

func TestCheckUpdateRemoteNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	t.Run("zero identity reports available on reachable remote", func(t *testing.T) {
		projectDir := t.TempDir()
		state := &config.State{
			Version:         config.StateVersion,
			Template:        "https://github.com/LazyGeniusMan/turutan.git",
			SourceKind:      "remote-git",
			RequestedRef:    "",
			ResolvedCommit:  strings.Repeat("0", 40),
			Engine:          "turutan/0.1.0",
			TemplateLicense: config.TemplateLicenseMIT0,
		}
		if err := config.SaveState(projectDir, state); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		result, err := CheckUpdate(context.Background(), projectDir, checkUpdateOptions(&stdout))
		if err != nil {
			t.Skipf("network unavailable: %v", err)
		}
		if !result.Available {
			t.Error("Available = false, want true against a zero stored identity")
		}
		if _, err := git.ResolveRemoteRef(context.Background(), "https://github.com/LazyGeniusMan/turutan.git", ""); err != nil {
			t.Errorf("re-resolve sanity check failed: %v", err)
		}
	})
}
