//go:build integration

// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	gitpkg "github.com/go-git/go-git/v6"
	gitconfig "github.com/go-git/go-git/v6/config"

	turutanconfig "github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

// TestDefaultSelfHostE2E is the M4 dogfood gate (spec §§8, 8.2): the repo
// self-hosts through the default template. It bootstraps from the local
// ./templates/default fixture (offline), asserts a clean checkout shows
// no drift through the real binary (`turutan diff` exits 0), checks
// byte-identical parity against the remote-default render at the floating
// stable ref (network step, skipped in -short mode and while the template
// release tag is unpublished), and runs the full cycle in t.TempDir():
// check-update clean → local edit → diff drift → template commit →
// update --ref → diff clean with the lock identity advanced. The
// min-engine gate is enforced throughout.
func TestDefaultSelfHostE2E(t *testing.T) {
	root := e2eModuleRoot(t)
	fixture := filepath.Join(root, "templates", "default")
	if _, err := os.Stat(filepath.Join(fixture, ".turutan.yml")); err != nil {
		t.Skipf("default template fixture not found: %v", err)
	}
	bin := e2eBuildTurutan(t, root)

	t.Run("clean checkout has no drift", func(t *testing.T) {
		proj := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(fixture, proj, testOptions(&stdout)); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		state, err := turutanconfig.LoadState(os.DirFS(proj))
		if err != nil {
			t.Fatal(err)
		}
		if state.TemplateLicense != turutanconfig.TemplateLicenseMIT0 {
			t.Errorf("TemplateLicense = %q, want MIT-0", state.TemplateLicense)
		}
		if state.Engine != "turutan/0.1.0" {
			t.Errorf("Engine = %q, want turutan/0.1.0", state.Engine)
		}
		diffs, err := ComputeDiff(proj, DiffOptions{})
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
		if testing.Short() {
			t.Skip("network parity skipped in -short mode")
		}
		repo := "https://github.com/LazyGeniusMan/turutan.git"
		if _, err := git.ResolveRemoteRef(repo, template.DefaultRef); err != nil {
			t.Skipf("default template ref %q unresolvable (unpublished or offline): %v", template.DefaultRef, err)
		}
		answersFile := filepath.Join(t.TempDir(), "answers.yml")
		if err := os.WriteFile(answersFile, []byte("project_name: parity\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := func(stdout *strings.Builder) Options {
			o := testOptions(stdout)
			o.AnswersFile = answersFile
			return o
		}
		local := filepath.Join(t.TempDir(), "local")
		var localOut strings.Builder
		if err := Bootstrap(fixture, local, opts(&localOut)); err != nil {
			t.Fatalf("local bootstrap error: %v", err)
		}
		remoteSrc := "git::" + repo + "//templates/default?ref=" + template.DefaultRef
		remote := filepath.Join(t.TempDir(), "remote")
		var remoteOut strings.Builder
		if err := Bootstrap(remoteSrc, remote, opts(&remoteOut)); err != nil {
			t.Fatalf("remote bootstrap error: %v", err)
		}
		e2eCompareTrees(t, local, remote)
	})

	t.Run("full update cycle", func(t *testing.T) {
		tmpl := filepath.Join(t.TempDir(), "tpl")
		if err := e2eCopyDir(fixture, tmpl); err != nil {
			t.Fatal(err)
		}
		e2eInitRepo(t, tmpl)
		sha1 := commitAll(t, tmpl, "template v1")

		proj := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(tmpl, proj, testOptions(&stdout)); err != nil {
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
		result, err := CheckUpdate(proj, checkUpdateOptions(&updateOut))
		if err != nil {
			t.Fatal(err)
		}
		if result.Available {
			t.Error("Available = true on clean checkout, want false")
		}
		if result.Old != sha1 || result.New != sha1 {
			t.Errorf("identity = %q -> %q, want %q -> %q", result.Old, result.New, sha1, sha1)
		}

		gomodPath := filepath.Join(proj, "go.mod")
		original, err := os.ReadFile(gomodPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(gomodPath, append(append([]byte{}, original...), []byte("\n// local edit\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		diffs, err := ComputeDiff(proj, DiffOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(diffs) == 0 {
			t.Error("no drift after local edit, want drift")
		}
		if _, code := e2eRun(t, bin, proj, "diff"); code != 2 {
			t.Errorf("turutan diff exit = %d after local edit, want 2", code)
		}
		if err := os.WriteFile(gomodPath, original, 0o600); err != nil {
			t.Fatal(err)
		}

		writeFiles(t, tmpl, map[string]string{"feature.txt": "feature v2\n"})
		sha2 := commitAll(t, tmpl, "template v2")
		if sha2 == sha1 {
			t.Fatal("second template commit matches first, want advance")
		}
		result, err = CheckUpdate(proj, checkUpdateOptions(&updateOut))
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
		updated, err := Update(proj, updateOpts)
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
		diffs, err = ComputeDiff(proj, DiffOptions{})
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
		var stdout strings.Builder
		old := testOptions(&stdout)
		old.Engine = "0.0.1"
		if err := Bootstrap(fixture, filepath.Join(t.TempDir(), "proj"), old); err == nil {
			t.Error("bootstrap below min-engine succeeded, want error")
		} else if !strings.Contains(err.Error(), "requires engine") {
			t.Errorf("bootstrap error = %v, want min-engine complaint", err)
		}
		proj := bootstrapProject(t, fixture)
		var updateStdout, updateStderr strings.Builder
		updateOpts := updateTestOptions(&updateStdout, &updateStderr)
		updateOpts.Engine = "0.0.1"
		if _, err := Update(proj, updateOpts); err == nil {
			t.Error("update below min-engine succeeded, want error")
		} else if !strings.Contains(err.Error(), "requires engine") {
			t.Errorf("update error = %v, want min-engine complaint", err)
		}
	})
}

// e2eModuleRoot returns the repo root derived from this file's path.
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

// e2eBuildTurutan compiles the CLI once for binary-level exit-code
// assertions (diff 0 clean / 2 drift).
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

// e2eRun executes the built binary in dir and returns stdout plus the
// process exit code.
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

// e2eInitRepo turns dir into an unsigned-commit git repo for local-git
// template sources.
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

// e2eCopyDir replicates the src tree at dst (dirs plus regular files with
// modes); the default-template fixture holds no symlinks or specials.
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

// e2eCompareTrees fails when the local-fixture and remote renders differ
// in any file other than the source-specific state and lock records.
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
