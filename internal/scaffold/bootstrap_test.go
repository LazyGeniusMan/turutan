// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"time"
)

// makeTemplate writes a template tree: manifest plus name→content files.
func makeTemplate(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, ".turutan.yml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const basicManifest = "source: \"test\"\nmin-engine: \">=0.1.0\"\nconflict: inline\nignore:\n  - \"ignored/**\"\npreserve:\n  - \"keep.txt\"\n"

var basicFiles = map[string]string{
	"go.mod.tmpl":      "module {{.project_name}}\n\ngo 1.27\n",
	"keep.txt":         "template keep\n",
	"ignored/skip.txt": "must not copy\n",
}

// testOptions returns non-interactive options capturing output.
func testOptions(stdout *strings.Builder) Options {
	return Options{
		NonInteractive: true,
		Defaults:       true,
		Engine:         "0.1.0",
		Stdout:         stdout,
		Stderr:         &strings.Builder{},
		Stdin:          strings.NewReader(""),
	}
}

func TestBootstrapFilesystem(t *testing.T) {
	t.Run("renders answers writes state and lock", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "module proj\n\ngo 1.27\n" {
			t.Errorf("go.mod = %q, want defaulted project name", got)
		}
		if _, err := os.Stat(filepath.Join(target, "ignored", "skip.txt")); err == nil {
			t.Error("ignored file was copied, want it skipped")
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.SourceKind != "filesystem" {
			t.Errorf("SourceKind = %q, want filesystem", state.SourceKind)
		}
		if len(state.ResolvedCommit) != 64 {
			t.Errorf("ResolvedCommit = %q, want 64-hex content identity", state.ResolvedCommit)
		}
		if state.TemplateLicense != turutanconfig.TemplateLicenseMIT0 {
			t.Errorf("TemplateLicense = %q, want MIT-0", state.TemplateLicense)
		}
		if state.Engine != "turutan/0.1.0" {
			t.Errorf("Engine = %q, want turutan/0.1.0", state.Engine)
		}
		lock, err := turutanconfig.LoadLock(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadLock error: %v", err)
		}
		if !strings.HasPrefix(lock.ManifestHash, "sha256:") {
			t.Errorf("ManifestHash = %q, want sha256: prefix", lock.ManifestHash)
		}
		if len(lock.Files) == 0 {
			t.Error("lock files empty, want rendered entries")
		}
		if !strings.Contains(stdout.String(), "bootstrapped") {
			t.Errorf("summary missing from stdout: %q", stdout.String())
		}
	})
	t.Run("answers file wins over defaults", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		answersFile := filepath.Join(t.TempDir(), "answers.yml")
		if err := os.WriteFile(answersFile, []byte("project_name: fromfile\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		opts := testOptions(&strings.Builder{})
		opts.AnswersFile = answersFile
		if err := Bootstrap(src, target, opts); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "go.mod"))
		if string(got) != "module fromfile\n\ngo 1.27\n" {
			t.Errorf("go.mod = %q, want answers-file value", got)
		}
	})
	t.Run("subpath scoped", func(t *testing.T) {
		root := makeTemplate(t, basicManifest, map[string]string{"sub/go.mod.tmpl": "module {{.project_name}}\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		if err := Bootstrap(root+"//sub", target, opts); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
			t.Errorf("expected subpath-scoped render: %v", err)
		}
	})
	t.Run("non-interactive without answers fails", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		opts := testOptions(&strings.Builder{})
		opts.Defaults = false
		if err := Bootstrap(src, filepath.Join(t.TempDir(), "p"), opts); err == nil {
			t.Error("non-interactive bootstrap without answers succeeded, want error")
		}
	})
	t.Run("missing key names the key", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, map[string]string{"f.txt.tmpl": "{{.other}}\n"})
		opts := testOptions(&strings.Builder{})
		err := Bootstrap(src, filepath.Join(t.TempDir(), "p"), opts)
		if err == nil || !strings.Contains(err.Error(), `"other"`) {
			t.Errorf("error = %v, want it to name the missing key", err)
		}
	})
	t.Run("non-empty target needs force", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Bootstrap(src, target, testOptions(&strings.Builder{})); err == nil {
			t.Error("bootstrap into non-empty dir succeeded, want error")
		}
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Force = true
		if err := Bootstrap(src, target, opts); err != nil {
			t.Errorf("bootstrap with --force error: %v", err)
		}
	})
	t.Run("preserve keeps local file under force", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Force = true
		if err := Bootstrap(src, target, opts); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "keep.txt"))
		if string(got) != "local keep\n" {
			t.Errorf("keep.txt = %q, want local content preserved", got)
		}
	})
	t.Run("min-engine floor enforced", func(t *testing.T) {
		src := makeTemplate(t, "min-engine: \">=99.0.0\"\n", basicFiles)
		opts := testOptions(&strings.Builder{})
		if err := Bootstrap(src, filepath.Join(t.TempDir(), "p"), opts); err == nil {
			t.Error("bootstrap below min-engine succeeded, want error")
		}
	})
	t.Run("hooks refused without consent", func(t *testing.T) {
		src := makeTemplate(t, "hooks:\n  post: [\"./hooks/post.sh\"]\n", basicFiles)
		opts := testOptions(&strings.Builder{})
		if err := Bootstrap(src, filepath.Join(t.TempDir(), "p"), opts); err == nil {
			t.Error("hook-bearing template in non-interactive mode succeeded, want refusal")
		}
	})
	t.Run("hooks consented with allow-hooks", func(t *testing.T) {
		src := makeTemplate(t, "hooks:\n  post: [\"hooks/post.sh\"]\n", map[string]string{
			"go.mod.tmpl":   "module {{.project_name}}\n",
			"hooks/post.sh": "touch hook-ran.txt\n",
		})
		target := filepath.Join(t.TempDir(), "p")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.AllowHooks = true
		if err := Bootstrap(src, target, opts); err != nil {
			t.Fatalf("Bootstrap with --allow-hooks error: %v", err)
		}
		if !strings.Contains(stdout.String(), "ran 1 template hook") {
			t.Errorf("hooks ran notice missing: %q", stdout.String())
		}
		if _, err := os.Stat(filepath.Join(target, "hook-ran.txt")); err != nil {
			t.Errorf("hook did not run (hook-ran.txt missing): %v", err)
		}
	})
	t.Run("interactive decline skips hooks with notice", func(t *testing.T) {
		src := makeTemplate(t, "hooks:\n  post: [\"touch hook-ran.txt\"]\n", basicFiles)
		target := filepath.Join(t.TempDir(), "p")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.NonInteractive = false
		opts.Stdin = strings.NewReader("n\n")
		if err := Bootstrap(src, target, opts); err != nil {
			t.Fatalf("Bootstrap with declined hooks error: %v", err)
		}
		if !strings.Contains(stdout.String(), "skipped (consent declined)") {
			t.Errorf("hooks skip notice missing: %q", stdout.String())
		}
		if _, err := os.Stat(filepath.Join(target, "hook-ran.txt")); err == nil {
			t.Error("declined hook ran, want it skipped")
		}
	})
	t.Run("interactive accept runs hooks", func(t *testing.T) {
		src := makeTemplate(t, "hooks:\n  post: [\"touch hook-ran.txt\"]\n", basicFiles)
		target := filepath.Join(t.TempDir(), "p")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.NonInteractive = false
		opts.Stdin = strings.NewReader("y\n")
		if err := Bootstrap(src, target, opts); err != nil {
			t.Fatalf("Bootstrap with accepted hooks error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "hook-ran.txt")); err != nil {
			t.Errorf("accepted hook did not run: %v", err)
		}
	})
	t.Run("interactive prompt supplies missing key", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"f.txt.tmpl": "{{.project_name}}-ok\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := Options{Engine: "0.1.0", Stdout: &stdout, Stderr: &strings.Builder{}, Stdin: strings.NewReader("typed\n")}
		if err := Bootstrap(src, target, opts); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "f.txt"))
		if string(got) != "typed-ok\n" {
			t.Errorf("f.txt = %q, want prompted value", got)
		}
	})
}

func TestBootstrapLocalGit(t *testing.T) {
	t.Run("records commit SHA", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "tpl")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range basicFiles {
			path := filepath.Join(repoDir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(repoDir, ".turutan.yml"), []byte(basicManifest), 0o600); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainInit(repoDir, false)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := repo.Config()
		if err != nil {
			t.Fatal(err)
		}
		cfg.Commit.GpgSign = config.OptBoolFalse
		if err := repo.SetConfig(cfg); err != nil {
			t.Fatal(err)
		}
		worktree, err := repo.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := worktree.Add("."); err != nil {
			t.Fatal(err)
		}
		want, err := worktree.Commit("template", &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()},
		})
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(repoDir, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatal(err)
		}
		if state.SourceKind != "local-git" {
			t.Errorf("SourceKind = %q, want local-git", state.SourceKind)
		}
		if state.ResolvedCommit != want.String() {
			t.Errorf("ResolvedCommit = %q, want %q", state.ResolvedCommit, want.String())
		}
	})
}

func TestBootstrapDefaultTemplateOffline(t *testing.T) {
	t.Run("in-repo default renders from filesystem", func(t *testing.T) {
		src := filepath.Join("..", "..", "templates", "default")
		if _, err := os.Stat(src); err != nil {
			t.Skipf("default template not found: %v", err)
		}
		target := filepath.Join(t.TempDir(), "myapp")
		var stdout strings.Builder
		if err := Bootstrap(src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, "go.mod"))
		if err != nil {
			t.Fatalf("reading rendered go.mod: %v", err)
		}
		if string(got) != "module myapp\n\ngo 1.27\n" {
			t.Errorf("go.mod = %q, want defaulted module", got)
		}
	})
}
