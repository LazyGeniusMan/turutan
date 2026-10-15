// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
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
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
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
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), target, ref, regs, testOptions(&stdout)); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(target, "go.mod")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
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
		if state.SourceKind != "registry" {
			t.Errorf("SourceKind = %q, want registry", state.SourceKind)
		}
		if state.Registry != testRegistryName {
			t.Errorf("Registry = %q, want %q", state.Registry, testRegistryName)
		}
		if state.Template != testRegistryName+"/"+testRegistryTemplate+"@"+testRegistryVersion {
			t.Errorf("Template = %q, want registry ref", state.Template)
		}
		if state.RequestedRef != testRegistryVersion {
			t.Errorf("RequestedRef = %q, want %q", state.RequestedRef, testRegistryVersion)
		}
		if !strings.HasPrefix(state.TemplateDigest, "sha256:") {
			t.Errorf("TemplateDigest = %q, want sha256: prefix", state.TemplateDigest)
		}
		if strings.TrimSpace(state.TemplateURI) == "" {
			t.Error("TemplateURI is empty, want the fetch URI")
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
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: fromfile\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		opts := testOptions(&strings.Builder{})
		opts.DataFile = dataFile
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "go.mod")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if string(got) != "module fromfile\n\ngo 1.26\n" {
			t.Errorf("go.mod = %q, want data-file value", got)
		}
	})
	t.Run("nested dirs render under sub", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, basicManifest, map[string]string{"sub/go.mod.tmpl": "module {{.project_name}}\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "sub", "go.mod")); err != nil {
			t.Errorf("expected nested render: %v", err)
		}
	})
	t.Run("non-interactive without data fails", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		opts := testOptions(&strings.Builder{})
		opts.Defaults = false
		if err := Init(context.Background(), filepath.Join(t.TempDir(), "p"), ref, regs, opts); err == nil {
			t.Error("non-interactive init without data succeeded, want error")
		}
	})
	t.Run("missing key names the key", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, basicManifest, map[string]string{"f.txt.tmpl": "{{.other}}\n"})
		opts := testOptions(&strings.Builder{})
		err := Init(context.Background(), filepath.Join(t.TempDir(), "p"), ref, regs, opts)
		if err == nil || !strings.Contains(err.Error(), `"other"`) {
			t.Errorf("error = %v, want it to name the missing key", err)
		}
	})
	t.Run("non-empty target needs force", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Init(context.Background(), target, ref, regs, testOptions(&strings.Builder{})); err == nil {
			t.Error("init into non-empty dir succeeded, want error")
		}
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Force = true
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Errorf("init with --force error: %v", err)
		}
	})
	t.Run("preserve keeps local file under force", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("local keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Force = true
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "keep.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if string(got) != "local keep\n" {
			t.Errorf("keep.txt = %q, want local content preserved", got)
		}
	})
	t.Run("min-engine floor enforced", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, "min-engine: \">=99.0.0\"\n", basicFiles)
		opts := testOptions(&strings.Builder{})
		if err := Init(context.Background(), filepath.Join(t.TempDir(), "p"), ref, regs, opts); err == nil {
			t.Error("init below min-engine succeeded, want error")
		}
	})
	t.Run("hooks refused without consent", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, "hooks:\n  post: [\"./hooks/post.sh\"]\n", basicFiles)
		opts := testOptions(&strings.Builder{})
		if err := Init(context.Background(), filepath.Join(t.TempDir(), "p"), ref, regs, opts); err == nil {
			t.Error("hook-bearing template in non-interactive mode succeeded, want refusal")
		}
	})
	t.Run("hooks consented with allow-hooks", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, "hooks:\n  post: [\"hooks/post.sh\"]\n", map[string]string{
			"go.mod.tmpl":   "module {{.project_name}}\n",
			"hooks/post.sh": "touch hook-ran.txt\n",
		})
		target := filepath.Join(t.TempDir(), "p")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.AllowHooks = true
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
		regs, ref := makeTestRegistry(t, "hooks:\n  post: [\"touch hook-ran.txt\"]\n", basicFiles)
		target := filepath.Join(t.TempDir(), "p")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.NonInteractive = false
		opts.Stdin = strings.NewReader("n\n")
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
		regs, ref := makeTestRegistry(t, "hooks:\n  post: [\"touch hook-ran.txt\"]\n", basicFiles)
		target := filepath.Join(t.TempDir(), "p")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.NonInteractive = false
		opts.Stdin = strings.NewReader("y\n")
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Fatalf("Init with accepted hooks error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "hook-ran.txt")); err != nil {
			t.Errorf("accepted hook did not run: %v", err)
		}
	})
	t.Run("interactive input supplies missing key", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, "", map[string]string{"f.txt.tmpl": "{{.project_name}}-ok\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := Options{Engine: "0.1.0", Stdout: &stdout, Stderr: &strings.Builder{}, Stdin: strings.NewReader("typed\n")}
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		got, _ := os.ReadFile(filepath.Join(target, "f.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if string(got) != "typed-ok\n" {
			t.Errorf("f.txt = %q, want input value", got)
		}
	})
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
	regs, ref := makeTestRegistry(t, manifest, files)
	target := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	opts := testOptions(&stdout)
	opts.DataFile = profilesDataFile(t, "b")
	if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Conflict = ConflictRej
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
		regs, ref := makeTestRegistry(t, basicManifest, basicFiles)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Init(context.Background(), target, ref, regs, testOptions(&stdout)); err != nil {
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
		regs, ref := makeTestRegistry(t, condManifest, files)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = profilesDataFile(t, "ai", "github", "go")
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
			t.Fatalf("Init error: %v", err)
		}
		for _, path := range []string{"go.mod", "cmd/app/main.go", ".github/ci.yml", "keep.txt"} {
			if _, err := os.Stat(filepath.Join(target, path)); err != nil {
				t.Errorf("expected %q rendered: %v", path, err)
			}
		}
	})
	t.Run("default profiles skip go and github", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, condManifest, files)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = dataFile
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
		regs, ref := makeTestRegistry(t, condManifest, files)
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.DataFile = dataFile
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
		regs, ref := makeTestRegistry(t, noDefaultManifest, map[string]string{"go.mod.tmpl": "module {{.project_name}}\n"})
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := testOptions(&strings.Builder{})
		opts.Defaults = false
		opts.DataFile = dataFile
		err := Init(context.Background(), filepath.Join(t.TempDir(), "p"), ref, regs, opts)
		if err == nil {
			t.Fatal("Init succeeded, want missing-key error")
		}
		if !strings.Contains(err.Error(), `"service_name"`) {
			t.Errorf("error = %v, want it to name the missing input key", err)
		}
	})
	t.Run("interactive input supplies project fields", func(t *testing.T) {
		regs, ref := makeTestRegistry(t, condManifest, files)
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := Options{Engine: "0.1.0", Stdout: &stdout, Stderr: &strings.Builder{}, Stdin: strings.NewReader("demo\ndesc\n"), DataFile: profilesDataFile(t, "go")}
		if err := Init(context.Background(), target, ref, regs, opts); err != nil {
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
