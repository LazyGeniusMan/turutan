// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	turutantemplate "github.com/LazyGeniusMan/turutan/internal/template"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
)

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

const basicManifest = "source: \"test\"\nmin-engine: \">=0.1.0\"\nconflict: inline\nexcludes:\n  - \"ignored/**\"\npreserves:\n  - \"keep.txt\"\n"

var basicFiles = map[string]string{
	"go.mod.tmpl":      "module {{.project_name}}\n\ngo 1.26\n",
	"keep.txt":         "template keep\n",
	"ignored/skip.txt": "must not copy\n",
}

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

func profilesDataFile(t *testing.T, profiles ...string) string {
	t.Helper()
	var b strings.Builder
	if len(profiles) == 0 {
		b.WriteString("profiles: []\n")
	} else {
		b.WriteString("profiles:\n")
	}
	for _, id := range profiles {
		b.WriteString("  - \"" + id + "\"\n")
	}
	path := filepath.Join(t.TempDir(), "profiles.yml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInitFilesystem(t *testing.T) {
	t.Run("renders data writes state and lock", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "module proj\n\ngo 1.26\n" {
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
		if !strings.Contains(stdout.String(), "initialized") {
			t.Errorf("summary missing from stdout: %q", stdout.String())
		}
	})
	t.Run("data file wins over defaults", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: fromfile\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		opts := testOptions(&strings.Builder{})
		opts.DataFile = dataFile
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "go.mod"))
		if string(got) != "module fromfile\n\ngo 1.26\n" {
			t.Errorf("go.mod = %q, want data-file value", got)
		}
	})
	t.Run("subpath scoped", func(t *testing.T) {
		root := makeTemplate(t, basicManifest, map[string]string{"sub/go.mod.tmpl": "module {{.project_name}}\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		if err := Init(context.Background(), root+"//sub", target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
			t.Errorf("expected subpath-scoped render: %v", err)
		}
	})
	t.Run("non-interactive without data fails", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		opts := testOptions(&strings.Builder{})
		opts.Defaults = false
		if err := Init(context.Background(), src, filepath.Join(t.TempDir(), "p"), opts); err == nil {
			t.Error("non-interactive init without data succeeded, want error")
		}
	})
	t.Run("missing key names the key", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, map[string]string{"f.txt.tmpl": "{{.other}}\n"})
		opts := testOptions(&strings.Builder{})
		err := Init(context.Background(), src, filepath.Join(t.TempDir(), "p"), opts)
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
		if err := Init(context.Background(), src, target, testOptions(&strings.Builder{})); err == nil {
			t.Error("init into non-empty dir succeeded, want error")
		}
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Force = true
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Errorf("init with --force error: %v", err)
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
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "keep.txt"))
		if string(got) != "local keep\n" {
			t.Errorf("keep.txt = %q, want local content preserved", got)
		}
	})
	t.Run("min-engine floor enforced", func(t *testing.T) {
		src := makeTemplate(t, "min-engine: \">=99.0.0\"\n", basicFiles)
		opts := testOptions(&strings.Builder{})
		if err := Init(context.Background(), src, filepath.Join(t.TempDir(), "p"), opts); err == nil {
			t.Error("init below min-engine succeeded, want error")
		}
	})
	t.Run("hooks refused without consent", func(t *testing.T) {
		src := makeTemplate(t, "hooks:\n  post: [\"./hooks/post.sh\"]\n", basicFiles)
		opts := testOptions(&strings.Builder{})
		if err := Init(context.Background(), src, filepath.Join(t.TempDir(), "p"), opts); err == nil {
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
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init with --allow-hooks error: %v", err)
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
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init with declined hooks error: %v", err)
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
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init with accepted hooks error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "hook-ran.txt")); err != nil {
			t.Errorf("accepted hook did not run: %v", err)
		}
	})
	t.Run("interactive input supplies missing key", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"f.txt.tmpl": "{{.project_name}}-ok\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := Options{Engine: "0.1.0", Stdout: &stdout, Stderr: &strings.Builder{}, Stdin: strings.NewReader("typed\n")}
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "f.txt"))
		if string(got) != "typed-ok\n" {
			t.Errorf("f.txt = %q, want input value", got)
		}
	})
}

func TestInitLocalGit(t *testing.T) {
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
		if err := Init(context.Background(), repoDir, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
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

func TestInitDefaultTemplateOffline(t *testing.T) {
	t.Run("in-repo default renders from filesystem", func(t *testing.T) {
		src := filepath.Join("..", "..", "templates", "default")
		if _, err := os.Stat(src); err != nil {
			t.Skipf("default template not found: %v", err)
		}
		target := filepath.Join(t.TempDir(), "myapp")
		var stdout strings.Builder
		if err := Init(context.Background(), src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, ".config", "turutan", "README.md"))
		if err != nil {
			t.Fatalf("reading rendered README.md: %v", err)
		}
		if !strings.Contains(string(got), "# myapp") {
			t.Errorf("README.md = %q, want defaulted project name", got)
		}
		if _, err := os.Stat(filepath.Join(target, ".config", "mise", "config.go.toml")); !os.IsNotExist(err) {
			t.Error("config.go.toml exists, want go-gated files kept out of the core-only default")
		}
	})
}

func TestInitLayeredYmlShared(t *testing.T) {
	fixture := filepath.Join("..", "..", "templates", "default")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("default template not found: %v", err)
	}
	initWith := func(t *testing.T, profiles ...string) string {
		t.Helper()
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = profilesDataFile(t, profiles...)
		if err := Init(context.Background(), fixture, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		return target
	}
	ymlKeep := filepath.Join(".config", "ai-rulez", "domains", "yml", ".gitkeep")
	for _, profiles := range [][]string{
		{"ai", "go", "github"},
		{"ai", "docker"},
		{"ai", "kubernetes"},
	} {
		target := initWith(t, profiles...)
		if _, err := os.Stat(filepath.Join(target, ymlKeep)); err != nil {
			t.Errorf("yml .gitkeep missing with profiles %v: %v", profiles, err)
		}
	}
	for _, profiles := range [][]string{
		{"docker"},
		{"kubernetes"},
		{"github"},
		{"ai"},
	} {
		target := initWith(t, profiles...)
		if _, err := os.Stat(filepath.Join(target, ymlKeep)); !os.IsNotExist(err) {
			t.Errorf("yml .gitkeep present with profiles %v, want ai+language gating", profiles)
		}
	}
	bare := initWith(t)
	if _, err := os.Stat(filepath.Join(bare, ymlKeep)); !os.IsNotExist(err) {
		t.Error("yml .gitkeep exists with no profiles, want yml-gated files kept out of the core-only default")
	}
}

func TestInitLayeredIncludesExcludes(t *testing.T) {
	const manifest = "excludes:\n  - \"foo/bar/**\"\n" +
		"conditionals:\n" +
		"  - if: \"true\"\n    includes: [\"foo/**\"]\n" +
		"  - if: '{{contains .profiles \"b\"}}'\n    includes: [\"foo/bar/baz/**\"]\n    excludes: [\"foo/bar/baz/2.md\"]\n"
	files := map[string]string{
		"foo/bar/baz/1.md": "one\n",
		"foo/bar/baz/2.md": "two\n",
		"keep.txt":         "keep\n",
	}
	src := makeTemplate(t, manifest, files)
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	opts := testOptions(&stdout)
	opts.DataFile = profilesDataFile(t, "b")
	if err := Init(context.Background(), src, target, opts); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "foo", "bar", "baz", "1.md")); err != nil {
		t.Errorf("1.md missing, want b re-include after a exclude: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "foo", "bar", "baz", "2.md")); !os.IsNotExist(err) {
		t.Errorf("2.md present, want b exclude to win")
	}
	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Errorf("keep.txt missing, want core-always: %v", err)
	}
}

func TestInitConflictPersisted(t *testing.T) {
	t.Run("conflict flag stored in state", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Conflict = ConflictRej
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.Conflict != string(ConflictRej) {
			t.Errorf("Conflict = %q, want %q", state.Conflict, ConflictRej)
		}
	})
	t.Run("omitted conflict stores empty", func(t *testing.T) {
		src := makeTemplate(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.Conflict != "" {
			t.Errorf("Conflict = %q, want empty", state.Conflict)
		}
	})
}

func TestInitManifestSourceMismatch(t *testing.T) {
	newOpts := func(stdout, stderr *strings.Builder) Options {
		return Options{
			NonInteractive: true,
			Defaults:       true,
			Engine:         "0.1.0",
			Stdout:         stdout,
			Stderr:         stderr,
			Stdin:          strings.NewReader(""),
		}
	}
	t.Run("mismatched source warns but succeeds", func(t *testing.T) {
		src := makeTemplate(t, "source: \"git::https://example.com/other.git\"\n", basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout, stderr strings.Builder
		if err := Init(context.Background(), src, target, newOpts(&stdout, &stderr)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if !strings.Contains(stderr.String(), "differs from requested") {
			t.Errorf("stderr = %q, want a source-mismatch warning", stderr.String())
		}
	})
	t.Run("absent manifest stays silent", func(t *testing.T) {
		src := makeTemplate(t, "", basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout, stderr strings.Builder
		if err := Init(context.Background(), src, target, newOpts(&stdout, &stderr)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if strings.Contains(stderr.String(), "differs from requested") {
			t.Errorf("stderr = %q, want no source warning without a manifest", stderr.String())
		}
	})
}

func TestInitConditionalProfiles(t *testing.T) {
	const condManifest = "inputs:\n" +
		"  project_name:\n    label: \"Project Name\"\n    type: string\n" +
		"  project_description:\n    label: \"Project Description\"\n    type: string\n    default: \"\"\n" +
		"conditionals:\n" +
		"  - if: '{{ not (contains .profiles \"go\") }}'\n" +
		"    excludes: [\"go.mod\", \"go.mod.tmpl\", \"cmd/**\"]\n" +
		"  - if: '{{ not (contains .profiles \"github\") }}'\n" +
		"    excludes: [\".github/**\"]\n"
	files := map[string]string{
		"go.mod.tmpl":         "module {{.project_name}}\n",
		"cmd/app/main.go":     "main {{.project_name}}\n",
		".github/ci.yml":      "ci\n",
		"keep.txt":            "keep\n",
		"README.md.tmpl":      "# {{.project_name}}\n",
		".config/miserc.toml": "env\n",
	}
	t.Run("explicit profiles keep all", func(t *testing.T) {
		src := makeTemplate(t, condManifest, files)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = profilesDataFile(t, "ai", "github", "go")
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		for _, path := range []string{"go.mod", "cmd/app/main.go", ".github/ci.yml", "keep.txt"} {
			if _, err := os.Stat(filepath.Join(target, path)); err != nil {
				t.Errorf("expected %q rendered: %v", path, err)
			}
		}
	})
	t.Run("default profiles skip go and github", func(t *testing.T) {
		src := makeTemplate(t, condManifest, files)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = dataFile
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		for _, path := range []string{"go.mod", "cmd/app/main.go", ".github/ci.yml"} {
			if _, err := os.Stat(filepath.Join(target, path)); !os.IsNotExist(err) {
				t.Errorf("file %q exists, want it skipped", path)
			}
		}
		if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
			t.Errorf("keep.txt missing: %v", err)
		}
	})
	t.Run("comma-packed profiles normalized", func(t *testing.T) {
		src := makeTemplate(t, condManifest, files)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = dataFile
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "go.mod")); !os.IsNotExist(err) {
			t.Error("go.mod exists, want it skipped for core+ai only")
		}
	})
	t.Run("non-interactive missing key names key", func(t *testing.T) {
		const noDefaultManifest = "inputs:\n" +
			"  project_name:\n    label: \"Project Name\"\n    type: string\n" +
			"  service_name:\n    label: \"Service name\"\n    type: string\n"
		src := makeTemplate(t, noDefaultManifest, map[string]string{"go.mod.tmpl": "module {{.project_name}}\n"})
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := testOptions(&strings.Builder{})
		opts.Defaults = false
		opts.DataFile = dataFile
		err := Init(context.Background(), src, filepath.Join(t.TempDir(), "p"), opts)
		if err == nil {
			t.Fatal("Init succeeded, want missing-key error")
		}
		if !strings.Contains(err.Error(), `"service_name"`) {
			t.Errorf("error = %v, want it to name the missing input key", err)
		}
	})
	t.Run("interactive input supplies project fields", func(t *testing.T) {
		src := makeTemplate(t, condManifest, files)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := Options{Engine: "0.1.0", Stdout: &stdout, Stderr: &strings.Builder{}, Stdin: strings.NewReader("demo\ndesc\n"), DataFile: profilesDataFile(t, "go")}
		if err := Init(context.Background(), src, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
			t.Errorf("go.mod missing, want go profile kept: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, ".github/ci.yml")); !os.IsNotExist(err) {
			t.Error(".github/ci.yml exists, want it skipped without github profile")
		}
	})
}

func TestInitDefaultREADME(t *testing.T) {
	t.Run("readme renders project name", func(t *testing.T) {
		src := filepath.Join("..", "..", "templates", "default")
		if _, err := os.Stat(src); err != nil {
			t.Skipf("default template not found: %v", err)
		}
		target := filepath.Join(t.TempDir(), "myapp")
		var stdout strings.Builder
		if err := Init(context.Background(), src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, ".config", "turutan", "README.md")) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatalf("reading rendered README.md: %v", err)
		}
		if !strings.Contains(string(got), "# myapp") {
			t.Errorf("README.md missing project name heading:\n%.500s", got)
		}
		if !strings.Contains(string(got), "turutan") {
			t.Errorf("README.md missing turutan attribution:\n%.500s", got)
		}
		if _, err := os.Stat(filepath.Join(target, "README.md")); !os.IsNotExist(err) {
			t.Error("README.md still at project root, want it planted under .config/turutan only")
		}
		if _, err := os.Stat(filepath.Join(target, ".config", "mise", "config.go.toml")); !os.IsNotExist(err) {
			t.Error("config.go.toml exists, want go-gated files kept out of the core-only default")
		}
	})
}

func TestInitDefaultFileSet(t *testing.T) {
	t.Run("rendered set matches template declaration", func(t *testing.T) {
		src := filepath.Join("..", "..", "templates", "default")
		if _, err := os.Stat(src); err != nil {
			t.Skipf("default template not found: %v", err)
		}
		target := filepath.Join(t.TempDir(), "myapp")
		var stdout strings.Builder
		if err := Init(context.Background(), src, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		manifest, err := turutanconfig.LoadManifest(os.DirFS(src))
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		layers, err := turutantemplate.EvaluateConditionalIncludes(
			map[string]any{"profiles": []any{}},
			manifestConditionals(manifest),
		)
		if err != nil {
			t.Fatalf("EvaluateConditionalIncludes error: %v", err)
		}
		shouldRender := func(slash string) bool {
			ok, err := turutantemplate.IsIncluded(manifest.Includes, manifest.Excludes, manifest.Preserves, layers, slash)
			if err != nil {
				return false
			}
			return ok
		}
		plantedSrc := map[string]bool{}
		plantedDests := map[string]bool{}
		for _, entry := range manifest.LicenseFiles {
			slash := filepath.ToSlash(entry)
			plantedSrc[slash] = true
			plantedDests[".config/turutan/"+slash] = true
		}
		for _, entry := range manifest.DocFiles {
			slash := filepath.ToSlash(entry)
			plantedSrc[slash] = true
			plantedDests[".config/turutan/"+slash] = true
		}
		expected := map[string]bool{}
		for dest := range plantedDests {
			expected[dest] = true
		}
		err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			slash := filepath.ToSlash(rel)
			if slash == ".config/turutan" || strings.HasPrefix(slash, ".config/turutan/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if slash == ".git" || strings.HasPrefix(slash, ".git/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			slash = strings.TrimSuffix(slash, ".tmpl")
			if !shouldRender(slash) && !shouldRender(filepath.ToSlash(rel)) {
				return nil
			}
			if ignored, err := filter.MatchAny(manifest.Excludes, slash); err == nil && ignored {
				return nil
			}
			if ignored, err := filter.MatchAny(manifest.Excludes, filepath.ToSlash(rel)); err == nil && ignored {
				return nil
			}
			if plantedSrc[slash] || plantedSrc[filepath.ToSlash(rel)] {
				return nil
			}
			expected[slash] = true
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		actual := map[string]bool{}
		err = filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(target, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			slash := filepath.ToSlash(rel)
			if slash == ".config/turutan" || strings.HasPrefix(slash, ".config/turutan/") {
				if !plantedDests[slash] {
					if d.IsDir() {
						beneath := false
						for dest := range plantedDests {
							if strings.HasPrefix(dest, slash+"/") {
								beneath = true
								break
							}
						}
						if !beneath {
							return filepath.SkipDir
						}
						return nil
					}
					return nil
				}
			}
			if slash == turutanconfig.StateFileName || slash == turutanconfig.LockFileName {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			actual[slash] = true
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for path := range expected {
			if !actual[path] {
				t.Errorf("template declares %q but render missed it", path)
			}
		}
		for path := range actual {
			if !expected[path] {
				t.Errorf("render produced undeclared %q", path)
			}
		}
	})
}

func TestInitFilesystemStoresRelative(t *testing.T) {
	t.Run("absolute input under target stores relative", func(t *testing.T) {
		root := t.TempDir()
		tpl := filepath.Join(root, "tpl")
		if err := os.MkdirAll(tpl, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tpl, ".turutan.yml"), []byte(basicManifest), 0o600); err != nil {
			t.Fatal(err)
		}
		for name, content := range basicFiles {
			path := filepath.Join(tpl, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(root, "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), tpl, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(target))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.SourceKind != "filesystem" {
			t.Errorf("SourceKind = %q, want filesystem", state.SourceKind)
		}
		if filepath.IsAbs(state.Template) {
			t.Errorf("Template = %q, want project-relative path", state.Template)
		}
		if state.Template != "../tpl" {
			t.Errorf("Template = %q, want %q", state.Template, "../tpl")
		}
	})
	t.Run("moved checkout keeps resolving", func(t *testing.T) {
		root := t.TempDir()
		tpl := filepath.Join(root, "tpl")
		if err := os.MkdirAll(tpl, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tpl, ".turutan.yml"), []byte(basicManifest), 0o600); err != nil {
			t.Fatal(err)
		}
		for name, content := range basicFiles {
			path := filepath.Join(tpl, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		target := filepath.Join(root, "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), tpl, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		moved := t.TempDir()
		if err := copyDirContents(root, moved); err != nil {
			t.Fatalf("copying checkout: %v", err)
		}
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		movedProj := filepath.Join(moved, "proj")
		var updateOut strings.Builder
		if _, err := Check(context.Background(), movedProj, checkOptions(&updateOut)); err != nil {
			t.Fatalf("Check after move error: %v", err)
		}
		if diffs, err := ComputeDiff(context.Background(), movedProj, diffOptions()); err != nil {
			t.Fatalf("ComputeDiff after move error: %v", err)
		} else if len(diffs) != 0 {
			t.Errorf("drift after move = %d files, want none", len(diffs))
		}
	})
}

func copyDirContents(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func TestInitDefaultExtensionsConditional(t *testing.T) {
	fixture := filepath.Join("..", "..", "templates", "default")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("default template not found: %v", err)
	}
	readRecommendations := func(t *testing.T, proj string) []string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(proj, ".vscode", "extensions.json"))
		if err != nil {
			t.Fatalf("reading rendered extensions.json: %v", err)
		}
		var decoded struct {
			Recommendations []string `json:"recommendations"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("extensions.json is not valid JSON: %v\n%s", err, data)
		}
		return decoded.Recommendations
	}
	writeDataFile := func(t *testing.T, profiles ...string) string {
		t.Helper()
		var b strings.Builder
		b.WriteString("project_name: exttest\n")
		if len(profiles) > 0 {
			b.WriteString("profiles:\n")
			for _, id := range profiles {
				b.WriteString("  - \"" + id + "\"\n")
			}
		}
		path := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("defaults render base only", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), fixture, target, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got := readRecommendations(t, target)
		for _, want := range []string{"tombi-toml.tombi", "hverlin.mise-vscode"} {
			if !slices.Contains(got, want) {
				t.Errorf("recommendations = %v, want %q", got, want)
			}
		}
		for _, unwanted := range []string{"golang.go", "charliermarsh.ruff", "astral-sh.ty", "void-zero.vite-plus-extension-pack"} {
			if slices.Contains(got, unwanted) {
				t.Errorf("recommendations = %v, want no %q without its profile", got, unwanted)
			}
		}
	})
	t.Run("profiles add their extensions", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = writeDataFile(t, "go", "python", "typescript")
		if err := Init(context.Background(), fixture, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got := readRecommendations(t, target)
		for _, want := range []string{"golang.go", "charliermarsh.ruff", "astral-sh.ty", "void-zero.vite-plus-extension-pack"} {
			if !slices.Contains(got, want) {
				t.Errorf("recommendations = %v, want %q", got, want)
			}
		}
	})
	t.Run("go alone adds only golang", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = writeDataFile(t, "go")
		if err := Init(context.Background(), fixture, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got := readRecommendations(t, target)
		if !slices.Contains(got, "golang.go") {
			t.Errorf("recommendations = %v, want golang.go", got)
		}
		for _, unwanted := range []string{"charliermarsh.ruff", "astral-sh.ty", "void-zero.vite-plus-extension-pack"} {
			if slices.Contains(got, unwanted) {
				t.Errorf("recommendations = %v, want no %q", got, unwanted)
			}
		}
	})
}

func TestInitDefaultEnabledPresets(t *testing.T) {
	fixture := filepath.Join("..", "..", "templates", "default")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("default template not found: %v", err)
	}
	readPresetsLine := func(t *testing.T, proj string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(proj, ".config", "ai-rulez", "config.toml"))
		if err != nil {
			t.Fatalf("reading rendered config.toml: %v", err)
		}
		for line := range strings.Lines(string(data)) {
			if strings.HasPrefix(strings.TrimSpace(line), "presets =") {
				return strings.TrimSpace(line)
			}
		}
		t.Fatal("rendered config.toml has no presets line")
		return ""
	}
	t.Run("explicit data render only the chosen presets", func(t *testing.T) {
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: presettest\nprofiles:\n  - ai\nai_agents:\n  - pi\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = dataFile
		if err := Init(context.Background(), fixture, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if got := readPresetsLine(t, target); got != `presets = ["pi"]` {
			t.Errorf("presets line = %q, want only pi", got)
		}
	})
	t.Run("defaults render the manifest default", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "proj")
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: presettest\nprofiles:\n  - ai\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = dataFile
		if err := Init(context.Background(), fixture, target, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if got := readPresetsLine(t, target); got != `presets = ["pi", "opencode"]` {
			t.Errorf("presets line = %q, want the manifest default", got)
		}
	})
}

// loadDefaultGating reads the real default manifest and evaluates its
// conditionals for profiles, returning the IsIncluded decision func.
// ~keep
func loadDefaultGating(t *testing.T, profiles ...string) func(string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "default", turutanconfig.ManifestFileName))
	if err != nil {
		t.Skipf("default template manifest not found: %v", err)
	}
	manifest, err := turutanconfig.LoadManifest(fstest.MapFS{turutanconfig.ManifestFileName: {Data: data}})
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	var viewProfiles []any
	for _, p := range profiles {
		viewProfiles = append(viewProfiles, p)
	}
	layers, err := turutantemplate.EvaluateConditionalIncludes(
		map[string]any{"profiles": viewProfiles},
		manifestConditionals(manifest),
	)
	if err != nil {
		t.Fatalf("EvaluateConditionalIncludes error: %v", err)
	}
	return func(slash string) bool {
		ok, err := turutantemplate.IsIncluded(manifest.Includes, manifest.Excludes, manifest.Preserves, layers, slash)
		if err != nil {
			t.Fatalf("IsIncluded(%q) error: %v", slash, err)
		}
		return ok
	}
}

// TestAiRulezGatingMatrix guards the ai+language gate: ai-rulez base files
// render only with ai, per-language ai-rulez files only with ai plus that
// language, and plain languages never leak ai-rulez paths.
// ~keep
func TestAiRulezGatingMatrix(t *testing.T) {
	t.Parallel()
	base := []string{
		".config/ai-rulez/config.toml",
		".config/ai-rulez/config.ai.toml",
		".config/ai-rulez/domains/core/rules/dev-ci-environment.md",
		".config/ai-rulez/domains/ai/rules/ai-rulez-usage.md",
		".config/ai-rulez/domains/pkl/.gitkeep",
		".config/ai-rulez/config.custom.toml",
		".config/ai-rulez/domains/custom/.gitkeep",
	}
	cFiles := []string{
		".config/ai-rulez/config.c.toml",
		".config/ai-rulez/domains/c/.gitkeep",
	}
	goFiles := []string{
		".config/ai-rulez/config.go.toml",
		".config/ai-rulez/domains/go/rules/go-skill-usage.md",
	}
	nonAi := []string{
		".config/mise/config.c.toml",
		".config/hk/c.pkl",
		".config/mise/config.go.toml",
		".config/hk/go.pkl",
	}
	t.Run("none renders no ai-rulez", func(t *testing.T) {
		t.Parallel()
		included := loadDefaultGating(t)
		for _, path := range append(append(append([]string{}, base...), cFiles...), goFiles...) {
			if included(path) {
				t.Errorf("IsIncluded(%q) with no profiles = true, want false", path)
			}
		}
	})
	t.Run("ai renders base only", func(t *testing.T) {
		t.Parallel()
		included := loadDefaultGating(t, "ai")
		for _, path := range base {
			if !included(path) {
				t.Errorf("IsIncluded(%q) with [ai] = false, want true", path)
			}
		}
		for _, path := range append([]string{}, append(cFiles, goFiles...)...) {
			if included(path) {
				t.Errorf("IsIncluded(%q) with [ai] = true, want false (no language)", path)
			}
		}
	})
	t.Run("language alone renders toolchain without ai-rulez", func(t *testing.T) {
		t.Parallel()
		included := loadDefaultGating(t, "c")
		for _, path := range nonAi[:2] {
			if !included(path) {
				t.Errorf("IsIncluded(%q) with [c] = false, want true", path)
			}
		}
		for _, path := range append(append(append([]string{}, base...), cFiles...), goFiles...) {
			if included(path) {
				t.Errorf("IsIncluded(%q) with [c] = true, want false (no ai)", path)
			}
		}
	})
	t.Run("ai plus language renders both", func(t *testing.T) {
		t.Parallel()
		included := loadDefaultGating(t, "ai", "c")
		for _, path := range append(append([]string{}, base...), cFiles...) {
			if !included(path) {
				t.Errorf("IsIncluded(%q) with [ai c] = false, want true", path)
			}
		}
		for _, path := range goFiles {
			if included(path) {
				t.Errorf("IsIncluded(%q) with [ai c] = true, want false (other language)", path)
			}
		}
	})
	t.Run("second language spot check", func(t *testing.T) {
		t.Parallel()
		included := loadDefaultGating(t, "ai", "go")
		for _, path := range goFiles {
			if !included(path) {
				t.Errorf("IsIncluded(%q) with [ai go] = false, want true", path)
			}
		}
		for _, path := range cFiles {
			if included(path) {
				t.Errorf("IsIncluded(%q) with [ai go] = true, want false (other language)", path)
			}
		}
	})
	t.Run("plain language carries no ai-rulez patterns", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile(filepath.Join("..", "..", "templates", "default", turutanconfig.ManifestFileName))
		if err != nil {
			t.Skipf("default template manifest not found: %v", err)
		}
		manifest, err := turutanconfig.LoadManifest(fstest.MapFS{turutanconfig.ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		for _, cond := range manifest.Conditionals {
			if !strings.HasPrefix(cond.If, `{{contains .profiles "`) {
				continue
			}
			if cond.If == `{{contains .profiles "ai"}}` {
				continue
			}
			for _, pattern := range cond.Includes {
				if strings.Contains(pattern, "ai-rulez") {
					t.Errorf("plain conditional %s includes %q, want ai-rulez gated on ai", cond.If, pattern)
				}
			}
		}
	})
}
