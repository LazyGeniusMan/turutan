//go:build integration

// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	gitpkg "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

func TestDefaultSelfHostE2E(t *testing.T) {
	root := e2eModuleRoot(t)
	fixture := filepath.Join(root, "templates", "default")
	if _, err := os.Stat(filepath.Join(fixture, ".turutan.yml")); err != nil {
		t.Skipf("default template fixture not found: %v", err)
	}
	bin := e2eBuildTurutan(t, root)

	t.Run("clean checkout has no drift", func(t *testing.T) {
		t.Parallel()
		proj := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(context.Background(), fixture, proj, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(proj))
		if err != nil {
			t.Fatal(err)
		}
		if state.TemplateLicense != turutanconfig.TemplateLicenseMIT0 {
			t.Errorf("TemplateLicense = %q, want MIT-0", state.TemplateLicense)
		}
		planted, err := os.ReadFile(filepath.Join(proj, ".config", "turutan", "LICENSE"))
		if err != nil {
			t.Fatalf("planted .config/turutan/LICENSE missing: %v", err)
		}
		if !strings.Contains(string(planted), "MIT No Attribution") {
			t.Errorf("planted LICENSE lacks MIT-0 text:\n%.200s", planted)
		}
		if state.Engine != "turutan/0.1.0" {
			t.Errorf("Engine = %q, want turutan/0.1.0", state.Engine)
		}
		diffs, err := ComputeDiff(context.Background(), proj, DiffOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(diffs) != 0 {
			t.Errorf("clean checkout has %d drifted files, want none", len(diffs))
		}
		out, code := e2eRun(t, bin, proj, "diff")
		if code != 0 {
			t.Errorf("turutan diff exit = %d, want 0 on clean checkout:\n%s", code, out)
		}
		if !strings.Contains(out, "no drift") {
			t.Errorf("turutan diff output missing no-drift line:\n%s", out)
		}
	})

	t.Run("remote parity", func(t *testing.T) {
		t.Parallel()
		if testing.Short() {
			t.Skip("network parity skipped in -short mode")
		}
		repo := "https://github.com/LazyGeniusMan/turutan.git"
		if _, err := git.ResolveRemoteRef(context.Background(), repo, template.DefaultRef); err != nil {
			t.Skipf("default template ref %q unresolvable (unpublished or offline): %v", template.DefaultRef, err)
		}
		dataFile := filepath.Join(t.TempDir(), "data.yml")
		if err := os.WriteFile(dataFile, []byte("project_name: parity\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := func(stdout *strings.Builder) Options {
			o := testOptions(stdout)
			o.DataFile = dataFile
			return o
		}
		local := filepath.Join(t.TempDir(), "local")
		var localOut strings.Builder
		if err := Bootstrap(context.Background(), fixture, local, opts(&localOut)); err != nil {
			t.Fatalf("local bootstrap error: %v", err)
		}
		remoteSrc := "git::" + repo + "//templates/default?ref=" + template.DefaultRef
		remote := filepath.Join(t.TempDir(), "remote")
		var remoteOut strings.Builder
		if err := Bootstrap(context.Background(), remoteSrc, remote, opts(&remoteOut)); err != nil {
			t.Fatalf("remote bootstrap error: %v", err)
		}
		e2eCompareTrees(t, local, remote)
	})

	t.Run("full update cycle", func(t *testing.T) {
		t.Parallel()
		tmpl := filepath.Join(t.TempDir(), "tpl")
		if err := e2eCopyDir(fixture, tmpl); err != nil {
			t.Fatal(err)
		}
		e2eInitRepo(t, tmpl)
		sha1 := commitAll(t, tmpl, "template v1")

		proj := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(context.Background(), tmpl, proj, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(proj))
		if err != nil {
			t.Fatal(err)
		}
		if state.SourceKind != "local-git" {
			t.Errorf("SourceKind = %q, want local-git", state.SourceKind)
		}
		if state.ResolvedCommit != sha1 {
			t.Errorf("ResolvedCommit = %q, want %q", state.ResolvedCommit, sha1)
		}

		var updateOut strings.Builder
		result, err := CheckUpdate(context.Background(), proj, checkUpdateOptions(&updateOut))
		if err != nil {
			t.Fatal(err)
		}
		if result.Available {
			t.Error("Available = true on clean checkout, want false")
		}
		if result.Old != sha1 || result.New != sha1 {
			t.Errorf("identity = %q -> %q, want %q -> %q", result.Old, result.New, sha1, sha1)
		}

		anchorPath := filepath.Join(proj, ".config", "mise", "config.toml")
		original, err := os.ReadFile(anchorPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(anchorPath, append(append([]byte{}, original...), []byte("\n# local edit\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		diffs, err := ComputeDiff(context.Background(), proj, DiffOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(diffs) == 0 {
			t.Error("no drift after local edit, want drift")
		}
		if _, code := e2eRun(t, bin, proj, "diff"); code != 2 {
			t.Errorf("turutan diff exit = %d after local edit, want 2", code)
		}
		if err := os.WriteFile(anchorPath, original, 0o600); err != nil {
			t.Fatal(err)
		}

		writeFiles(t, tmpl, map[string]string{"feature.txt": "feature v2\n"})
		sha2 := commitAll(t, tmpl, "template v2")
		if sha2 == sha1 {
			t.Fatal("second template commit matches first, want advance")
		}
		result, err = CheckUpdate(context.Background(), proj, checkUpdateOptions(&updateOut))
		if err != nil {
			t.Fatal(err)
		}
		if !result.Available {
			t.Error("Available = false after template commit, want true")
		}
		if result.Old != sha1 || result.New != sha2 {
			t.Errorf("identity = %q -> %q, want %q -> %q", result.Old, result.New, sha1, sha2)
		}

		var updateStdout, updateStderr strings.Builder
		updateOpts := updateTestOptions(&updateStdout, &updateStderr)
		updateOpts.Ref = sha2
		updated, err := Update(context.Background(), proj, updateOpts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if updated.New != sha2 {
			t.Errorf("updated New = %q, want %q", updated.New, sha2)
		}
		if len(updated.Conflicts) != 0 {
			t.Errorf("conflicts = %v, want none", updated.Conflicts)
		}
		got, err := os.ReadFile(filepath.Join(proj, "feature.txt"))
		if err != nil {
			t.Fatalf("updated feature.txt missing: %v", err)
		}
		if string(got) != "feature v2\n" {
			t.Errorf("feature.txt = %q, want template content", got)
		}
		lock, err := turutanconfig.LoadLock(os.DirFS(proj))
		if err != nil {
			t.Fatal(err)
		}
		if lock.ResolvedSHA != sha2 {
			t.Errorf("lock ResolvedSHA = %q, want advanced %q", lock.ResolvedSHA, sha2)
		}
		state, err = turutanconfig.LoadState(os.DirFS(proj))
		if err != nil {
			t.Fatal(err)
		}
		if state.ResolvedCommit != sha2 {
			t.Errorf("state ResolvedCommit = %q, want advanced %q", state.ResolvedCommit, sha2)
		}
		diffs, err = ComputeDiff(context.Background(), proj, DiffOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(diffs) != 0 {
			t.Errorf("post-update drift = %d files, want none", len(diffs))
		}
		if out, code := e2eRun(t, bin, proj, "diff"); code != 0 {
			t.Errorf("turutan diff exit = %d after update, want 0:\n%s", code, out)
		}
	})

	t.Run("min-engine gate enforced", func(t *testing.T) {
		t.Parallel()
		var stdout strings.Builder
		old := testOptions(&stdout)
		old.Engine = "0.0.1"
		if err := Bootstrap(context.Background(), fixture, filepath.Join(t.TempDir(), "proj"), old); err == nil {
			t.Error("bootstrap below min-engine succeeded, want error")
		} else if !strings.Contains(err.Error(), "requires engine") {
			t.Errorf("bootstrap error = %v, want min-engine complaint", err)
		}
		proj := bootstrapProject(t, fixture)
		var updateStdout, updateStderr strings.Builder
		updateOpts := updateTestOptions(&updateStdout, &updateStderr)
		updateOpts.Engine = "0.0.1"
		if _, err := Update(context.Background(), proj, updateOpts); err == nil {
			t.Error("update below min-engine succeeded, want error")
		} else if !strings.Contains(err.Error(), "requires engine") {
			t.Errorf("update error = %v, want min-engine complaint", err)
		}
	})
}

func TestDefaultProfileChangeOffline(t *testing.T) {
	root := e2eModuleRoot(t)
	fixture := filepath.Join(root, "templates", "default")
	if _, err := os.Stat(filepath.Join(fixture, ".turutan.yml")); err != nil {
		t.Skipf("default template fixture not found: %v", err)
	}
	t.Run("profiles input flows to miserc env", func(t *testing.T) {
		t.Parallel()
		proj := e2eBootstrapProfiles(t, fixture)
		if _, err := os.Stat(filepath.Join(proj, ".config", "mise", "config.go.toml")); err != nil {
			t.Fatalf("config.go.toml missing after bootstrap, want conditional files to render: %v", err)
		}
		e2eAssertMisercEnv(t, proj, `env = ["core", "custom"]`)
		e2eUpdateProfiles(t, proj, "go")
		got, err := os.ReadFile(filepath.Join(proj, ".config", "mise", "config.go.toml"))
		if err != nil {
			t.Fatalf("reading added config.go.toml: %v", err)
		}
		if !strings.Contains(string(got), "golangci-lint") {
			t.Errorf("config.go.toml = %q, want go toolchain", got)
		}
		miserc, err := os.ReadFile(filepath.Join(proj, ".config", "miserc.toml"))
		if err != nil {
			t.Fatalf("reading miserc.toml: %v", err)
		}
		if !strings.Contains(string(miserc), `env = ["core", "go", "custom"]`) {
			t.Errorf("miserc.toml = %q, want env [core go custom]", miserc)
		}
		if got := e2eStoredProfiles(t, proj); !slices.Equal(got, []string{"go"}) {
			t.Errorf("stored profiles = %v, want [go]", got)
		}
	})
	t.Run("go profile content follows the profiles input", func(t *testing.T) {
		t.Parallel()
		proj := e2eBootstrapProfiles(t, fixture, "go")
		if _, err := os.Stat(filepath.Join(proj, ".config", "mise", "config.go.toml")); err != nil {
			t.Fatalf("config.go.toml missing after core+go bootstrap: %v", err)
		}
		e2eAssertMisercEnv(t, proj, `env = ["core", "go", "custom"]`)
		e2eUpdateProfiles(t, proj)
		if _, err := os.Stat(filepath.Join(proj, ".config", "mise", "config.go.toml")); err != nil {
			t.Errorf("config.go.toml missing after profiles change, want conditional files to keep rendering: %v", err)
		}
		miserc, err := os.ReadFile(filepath.Join(proj, ".config", "miserc.toml"))
		if err != nil {
			t.Fatalf("reading miserc.toml: %v", err)
		}
		if !strings.Contains(string(miserc), `env = ["core", "custom"]`) {
			t.Errorf("miserc.toml = %q, want env [core custom]", miserc)
		}
		if strings.Contains(string(miserc), `"go"`) {
			t.Errorf("miserc.toml = %q, want no go profile entry", miserc)
		}
		if got := e2eStoredProfiles(t, proj); len(got) != 0 {
			t.Errorf("stored profiles = %v, want empty", got)
		}
	})
}

func TestDefaultProfileChangeBinaryE2E(t *testing.T) {
	root := e2eModuleRoot(t)
	fixture := filepath.Join(root, "templates", "default")
	if _, err := os.Stat(filepath.Join(fixture, ".turutan.yml")); err != nil {
		t.Skipf("default template fixture not found: %v", err)
	}
	bin := e2eBuildTurutan(t, root)
	work := t.TempDir()
	goData := e2eDataFile(t, "profiles:\n  - go\n")
	if out, code := e2eRun(t, bin, work, "bootstrap", fixture, "proj",
		"--data-file", goData, "--defaults", "--non-interactive"); code != 0 {
		t.Fatalf("bootstrap exit = %d, want 0:\n%s", code, out)
	}
	proj := filepath.Join(work, "proj")
	e2eAssertMisercEnv(t, proj, `env = ["core", "go", "custom"]`)
	e2eAssertReadmeProject(t, proj, "proj")
	got, err := os.ReadFile(filepath.Join(proj, ".config", "mise", "config.go.toml"))
	if err != nil {
		t.Fatalf("reading config.go.toml: %v", err)
	}
	if !strings.Contains(string(got), "golangci-lint") {
		t.Errorf("config.go.toml = %q, want go toolchain", got)
	}
	if profiles := e2eStoredProfiles(t, proj); !slices.Equal(profiles, []string{"go"}) {
		t.Errorf("stored profiles = %v, want [go]", profiles)
	}
	coreData := e2eDataFile(t, "profiles: []\n")
	if out, code := e2eRun(t, bin, proj, "update",
		"--data-file", coreData, "--force", "--non-interactive"); code != 0 {
		t.Fatalf("update exit = %d, want 0:\n%s", code, out)
	} else if !strings.Contains(out, "updated") {
		t.Errorf("update output missing summary:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(proj, ".config", "mise", "config.go.toml")); err != nil {
		t.Errorf("config.go.toml missing after profiles change, want conditional files to keep rendering: %v", err)
	}
	if profiles := e2eStoredProfiles(t, proj); len(profiles) != 0 {
		t.Errorf("stored profiles = %v, want empty", profiles)
	}
	e2eAssertMisercEnv(t, proj, `env = ["core", "custom"]`)
	e2eAssertReadmeProject(t, proj, "proj")
}

func TestDefaultBinarySyncE2E(t *testing.T) {
	root := e2eModuleRoot(t)
	fixture := filepath.Join(root, "templates", "default")
	if _, err := os.Stat(filepath.Join(fixture, ".turutan.yml")); err != nil {
		t.Skipf("default template fixture not found: %v", err)
	}
	bin := e2eBuildTurutan(t, root)

	t.Run("diff what matrix", func(t *testing.T) {
		t.Parallel()
		tmpl := filepath.Join(t.TempDir(), "tpl")
		if err := e2eCopyDir(fixture, tmpl); err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		if out, code := e2eRun(t, bin, work, "bootstrap", tmpl, "proj",
			"--defaults", "--non-interactive"); code != 0 {
			t.Fatalf("bootstrap exit = %d, want 0:\n%s", code, out)
		}
		proj := filepath.Join(work, "proj")
		for _, what := range []string{"all", "upstream", "local"} {
			if out, code := e2eRun(t, bin, proj, "diff", "--what", what, "--non-interactive"); code != 0 {
				t.Errorf("diff --what %s exit = %d, want 0 on clean checkout:\n%s", what, code, out)
			} else if !strings.Contains(out, "no drift") {
				t.Errorf("diff --what %s output missing no-drift line:\n%s", what, out)
			}
		}
		if out, code := e2eRun(t, bin, proj, "diff", "--what", "bogus", "--non-interactive"); code == 0 {
			t.Errorf("diff --what bogus exit = 0, want non-zero:\n%s", out)
		} else if code != 1 {
			t.Errorf("diff --what bogus exit = %d, want 1", code)
		}
		anchorRel := filepath.ToSlash(filepath.Join(".config", "mise", "config.toml"))
		anchor := filepath.Join(proj, filepath.FromSlash(anchorRel))
		original, err := os.ReadFile(anchor)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(anchor, append(append([]byte{}, original...), []byte("\n# local edit\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, code := e2eRun(t, bin, proj, "diff", "--non-interactive"); code != 2 {
			t.Errorf("diff exit = %d after local edit, want 2:\n%s", code, out)
		}
		if out, code := e2eRun(t, bin, proj, "diff", "--what", "local", "--non-interactive"); code != 2 {
			t.Errorf("diff --what local exit = %d after local edit, want 2:\n%s", code, out)
		} else if !strings.Contains(out, anchorRel) {
			t.Errorf("diff --what local output missing %s:\n%s", anchorRel, out)
		}
		if out, code := e2eRun(t, bin, proj, "diff", "--what", "upstream", "--non-interactive"); code != 0 {
			t.Errorf("diff --what upstream exit = %d with only local edits, want 0:\n%s", code, out)
		} else if !strings.Contains(out, "no drift") {
			t.Errorf("diff --what upstream output missing no-drift line:\n%s", out)
		}
		if err := os.WriteFile(anchor, original, 0o600); err != nil {
			t.Fatal(err)
		}
		tmplAnchor := filepath.Join(tmpl, filepath.FromSlash(anchorRel))
		tmplOriginal, err := os.ReadFile(tmplAnchor)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmplAnchor, append(append([]byte{}, tmplOriginal...), []byte("\n# upstream change\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, code := e2eRun(t, bin, proj, "diff", "--what", "upstream", "--non-interactive"); code != 2 {
			t.Errorf("diff --what upstream exit = %d after template change, want 2:\n%s", code, out)
		} else if !strings.Contains(out, anchorRel) {
			t.Errorf("diff --what upstream output missing %s:\n%s", anchorRel, out)
		}
		if out, code := e2eRun(t, bin, proj, "diff", "--what", "local", "--non-interactive"); code != 0 {
			t.Errorf("diff --what local exit = %d with only template changes, want 0:\n%s", code, out)
		} else if !strings.Contains(out, "no drift") {
			t.Errorf("diff --what local output missing no-drift line:\n%s", out)
		}
		if _, code := e2eRun(t, bin, proj, "diff", "--non-interactive"); code != 2 {
			t.Errorf("diff exit = %d after template change, want 2", code)
		}
	})

	t.Run("update preview flag", func(t *testing.T) {
		t.Parallel()
		tmpl := filepath.Join(t.TempDir(), "tpl")
		if err := e2eCopyDir(fixture, tmpl); err != nil {
			t.Fatal(err)
		}
		work := t.TempDir()
		if out, code := e2eRun(t, bin, work, "bootstrap", tmpl, "proj",
			"--defaults", "--non-interactive"); code != 0 {
			t.Fatalf("bootstrap exit = %d, want 0:\n%s", code, out)
		}
		proj := filepath.Join(work, "proj")
		if out, code := e2eRun(t, bin, proj, "update", "--preview", "--non-interactive"); code != 0 {
			t.Fatalf("update --preview exit = %d, want 0:\n%s", code, out)
		} else if !strings.Contains(out, "no changes") {
			t.Errorf("update --preview output missing no-changes plan:\n%s", out)
		}
		if err := os.WriteFile(filepath.Join(tmpl, "preview-me.txt"), []byte("preview\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, code := e2eRun(t, bin, proj, "update", "--preview", "--non-interactive"); code != 0 {
			t.Fatalf("update --preview exit = %d, want 0:\n%s", code, out)
		} else if !strings.Contains(out, "preview") || !strings.Contains(out, "preview-me.txt") {
			t.Errorf("update --preview output missing plan for preview-me.txt:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(proj, "preview-me.txt")); !os.IsNotExist(err) {
			t.Error("preview-me.txt exists after --preview, want preview to write nothing")
		}
		if out, code := e2eRun(t, bin, proj, "update", "--force", "--non-interactive"); code != 0 {
			t.Fatalf("update exit = %d, want 0:\n%s", code, out)
		} else if !strings.Contains(out, "updated") {
			t.Errorf("update output missing summary:\n%s", out)
		}
		if got, err := os.ReadFile(filepath.Join(proj, "preview-me.txt")); err != nil {
			t.Errorf("preview-me.txt missing after update: %v", err)
		} else if string(got) != "preview\n" {
			t.Errorf("preview-me.txt = %q, want template content", got)
		}
	})

	t.Run("no profiles command", func(t *testing.T) {
		t.Parallel()
		if out, code := e2eRun(t, bin, t.TempDir(), "profiles", "--non-interactive"); code == 0 {
			t.Errorf("profiles exit = 0, want non-zero for the removed command:\n%s", out)
		}
		if out, code := e2eRun(t, bin, t.TempDir(), "bootstrap", "--help"); code != 0 {
			t.Fatalf("bootstrap --help exit = %d, want 0:\n%s", code, out)
		} else if strings.Contains(out, "--profiles") {
			t.Errorf("bootstrap --help mentions --profiles, want the flag gone:\n%s", out)
		}
	})

	t.Run("check-update quiet when up to date", func(t *testing.T) {
		t.Parallel()
		work := t.TempDir()
		if out, code := e2eRun(t, bin, work, "bootstrap", fixture, "proj",
			"--defaults", "--non-interactive"); code != 0 {
			t.Fatalf("bootstrap exit = %d, want 0:\n%s", code, out)
		}
		proj := filepath.Join(work, "proj")
		if out, code := e2eRun(t, bin, proj, "check-update", "--non-interactive"); code != 0 {
			t.Fatalf("check-update exit = %d, want 0 when up to date:\n%s", code, out)
		} else {
			if !strings.Contains(out, "up to date") {
				t.Errorf("check-update output missing up-to-date line:\n%s", out)
			}
			if strings.Contains(out, "offers new profiles") {
				t.Errorf("check-update output mentions legacy profile offers:\n%s", out)
			}
		}
	})
}

func e2eAssertMisercEnv(t *testing.T, proj, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj, ".config", "miserc.toml"))
	if err != nil {
		t.Fatalf("reading miserc.toml: %v", err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("miserc.toml = %q, want it to contain %q", data, want)
	}
}

func e2eAssertReadmeProject(t *testing.T, proj, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj, ".config", "turutan", "README.md"))
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("README.md missing %q", want)
	}
}

func e2eBootstrapProfiles(t *testing.T, fixture string, profiles ...string) string {
	t.Helper()
	proj := filepath.Join(t.TempDir(), "proj")
	var stdout strings.Builder
	opts := testOptions(&stdout)
	if len(profiles) > 0 {
		opts.DataFile = profilesDataFile(t, profiles...)
	}
	if err := Bootstrap(context.Background(), fixture, proj, opts); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	return proj
}

func e2eUpdateProfiles(t *testing.T, projectDir string, profiles ...string) {
	t.Helper()
	var stdout, stderr strings.Builder
	opts := updateTestOptions(&stdout, &stderr)
	opts.Force = true
	opts.NonInteractive = true
	opts.DataFile = profilesDataFile(t, profiles...)
	if _, err := Update(context.Background(), projectDir, opts); err != nil {
		t.Fatalf("Update error: %v", err)
	}
}

func e2eStoredProfiles(t *testing.T, projectDir string) []string {
	t.Helper()
	state, err := turutanconfig.LoadState(os.DirFS(projectDir))
	if err != nil {
		t.Fatal(err)
	}
	if state.Data == nil || state.Data["profiles"] == nil {
		return nil
	}
	return normalizeArrayValue(state.Data["profiles"])
}

func e2eDataFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func e2eModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	return root
}

func e2eBuildTurutan(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "turutan")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/turutan")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building turutan: %v\n%s", err, out)
	}
	return bin
}

func e2eRun(t *testing.T, bin, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), exitErr.ExitCode()
		}
		t.Fatalf("running turutan %v: %v\n%s", args, err, stderr.String())
	}
	return stdout.String(), 0
}

func e2eInitRepo(t *testing.T, dir string) {
	t.Helper()
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
}

func e2eCopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
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
			return fmt.Errorf("copying %q: non-regular file unsupported", rel)
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

func e2eCompareTrees(t *testing.T, local, remote string) {
	t.Helper()
	skipped := map[string]bool{
		turutanconfig.StateFileName: true,
		turutanconfig.LockFileName:  true,
	}
	collect := func(root string) map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if skipped[rel] {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[rel] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	localFiles, remoteFiles := collect(local), collect(remote)
	var diffs []string
	for rel, content := range localFiles {
		other, ok := remoteFiles[rel]
		if !ok {
			diffs = append(diffs, "missing in remote: "+rel)
			continue
		}
		if other != content {
			diffs = append(diffs, "content differs: "+rel)
		}
	}
	for rel := range remoteFiles {
		if _, ok := localFiles[rel]; !ok {
			diffs = append(diffs, "missing in local: "+rel)
		}
	}
	if len(diffs) > 0 {
		t.Errorf("remote parity drift (%d paths):\n  %s", len(diffs), strings.Join(diffs, "\n  "))
	}
}
