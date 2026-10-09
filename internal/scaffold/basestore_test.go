// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func TestBasePath(t *testing.T) {
	t.Run("empty identity rejected", func(t *testing.T) {
		if _, err := BasePath(t.TempDir(), ""); err == nil {
			t.Error("BasePath with empty sha succeeded, want error")
		}
	})
	t.Run("escaping identity rejected", func(t *testing.T) {
		if _, err := BasePath(t.TempDir(), "../escape"); err == nil {
			t.Error("BasePath with escaping sha succeeded, want error")
		}
	})
	t.Run("sha stays in store", func(t *testing.T) {
		dir := t.TempDir()
		got, err := BasePath(dir, "abc123")
		if err != nil {
			t.Fatalf("BasePath error: %v", err)
		}
		want := filepath.Join(dir, ".turutan", "templates", "abc123")
		if got != want {
			t.Errorf("BasePath = %q, want %q", got, want)
		}
	})
}

func TestStoreBaseRoundTrip(t *testing.T) {
	t.Run("stores regular files and reads them back", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{
			"a.txt":     "hello\n",
			"sub/b.txt": "world\n",
		})
		projectDir := t.TempDir()
		if err := StoreBase(projectDir, "sha1", src); err != nil {
			t.Fatalf("StoreBase error: %v", err)
		}
		if !HasBase(projectDir, "sha1") {
			t.Fatal("HasBase = false after StoreBase")
		}
		if HasBase(projectDir, "other") {
			t.Error("HasBase(other) = true, want false")
		}
		got, err := ReadBaseFile(projectDir, "sha1", "sub/b.txt")
		if err != nil {
			t.Fatalf("ReadBaseFile error: %v", err)
		}
		if string(got) != "world\n" {
			t.Errorf("base content = %q, want %q", got, "world\n")
		}
	})
	t.Run("skips metadata symlinks and special files", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"a.txt": "x\n"})
		if err := os.MkdirAll(filepath.Join(src, ".turutan"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, ".turutan", "junk"), []byte("j"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(src, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(src, "evil.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		projectDir := t.TempDir()
		if err := StoreBase(projectDir, "sha2", src); err != nil {
			t.Fatalf("StoreBase error: %v", err)
		}
		base, err := BasePath(projectDir, "sha2")
		if err != nil {
			t.Fatal(err)
		}
		for _, absent := range []string{".turutan/junk", ".git", "evil.txt"} {
			if _, err := os.Lstat(filepath.Join(base, filepath.FromSlash(absent))); err == nil {
				t.Errorf("base store contains %q, want it skipped", absent)
			}
		}
	})
}

func TestBootstrapStoresBase(t *testing.T) {
	t.Run("base exists after bootstrap", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if !HasBase(projectDir, state.ResolvedCommit) {
			t.Errorf("no base copy for resolved %q after bootstrap", state.ResolvedCommit)
		}
		got, err := ReadBaseFile(projectDir, state.ResolvedCommit, "hello.txt")
		if err != nil {
			t.Fatalf("ReadBaseFile error: %v", err)
		}
		if string(got) != "v1\n" {
			t.Errorf("base content = %q, want %q", got, "v1\n")
		}
	})
}

// threeWayFixture bootstraps a filesystem template holding both.txt, then
// applies a local edit and a template-side edit.
func threeWayFixture(t *testing.T, base, localEdit, templateEdit string) string {
	t.Helper()
	src := makeTemplate(t, "", map[string]string{"both.txt": base})
	projectDir := bootstrapProject(t, src)
	if err := os.WriteFile(filepath.Join(projectDir, "both.txt"), []byte(localEdit), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "both.txt"), []byte(templateEdit), 0o600); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

func TestUpdateThreeWay(t *testing.T) {
	const base = "line1\nline2\nline3\nline4\n"
	t.Run("disjoint edits merge cleanly", func(t *testing.T) {
		projectDir := threeWayFixture(t, base, "LOCAL1\nline2\nline3\nline4\n", "line1\nline2\nline3\nTEMPLATE4\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Conflicts) != 0 {
			t.Errorf("Conflicts = %v, want none", result.Conflicts)
		}
		got, err := os.ReadFile(filepath.Join(projectDir, "both.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "LOCAL1\nline2\nline3\nTEMPLATE4\n" {
			t.Errorf("merged = %q, want disjoint union", got)
		}
	})
	t.Run("overlapping edits conflict inline", func(t *testing.T) {
		projectDir := threeWayFixture(t, base, "line1\nLOCAL2\nline3\nline4\n", "line1\nTEMPLATE2\nline3\nline4\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Conflicts) != 1 || result.Conflicts[0] != "both.txt" {
			t.Errorf("Conflicts = %v, want [both.txt]", result.Conflicts)
		}
		got, err := os.ReadFile(filepath.Join(projectDir, "both.txt"))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"<<<<<<< local", "=======", ">>>>>>> template-"} {
			if !strings.Contains(string(got), marker) {
				t.Errorf("merged file lacks %q:\n%s", marker, got)
			}
		}
	})
	t.Run("template-only change takes new", func(t *testing.T) {
		projectDir := threeWayFixture(t, base, base, "line1\nline2\nline3\nTEMPLATE4\n")
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		if _, err := Update(projectDir, opts); err != nil {
			t.Fatalf("Update error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(projectDir, "both.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "line1\nline2\nline3\nTEMPLATE4\n" {
			t.Errorf("merged = %q, want template content", got)
		}
	})
}

func TestUpdateOverlayFallback(t *testing.T) {
	t.Run("missing store falls back to overlay", func(t *testing.T) {
		projectDir := threeWayFixture(t,
			"line1\nline2\n",
			"line1\nLOCAL2\n",
			"line1\nTEMPLATE2\n")
		// Pre-M5 projects have no base store: drop it entirely.
		if err := os.RemoveAll(filepath.Join(projectDir, ".turutan", "templates")); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Conflicts) != 1 {
			t.Errorf("Conflicts = %v, want one overlay conflict", result.Conflicts)
		}
		got, err := os.ReadFile(filepath.Join(projectDir, "both.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "<<<<<<< local") {
			t.Errorf("fallback file lacks inline markers:\n%s", got)
		}
		// The fallback run stores the new base, upgrading the next update.
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		if !HasBase(projectDir, state.ResolvedCommit) {
			t.Error("fallback update stored no new base")
		}
	})
	t.Run("missing base entry for one file uses overlay", func(t *testing.T) {
		projectDir := threeWayFixture(t,
			"line1\nline2\n",
			"line1\nLOCAL2\n",
			"line1\nTEMPLATE2\n")
		state, err := config.LoadState(os.DirFS(projectDir))
		if err != nil {
			t.Fatal(err)
		}
		base, err := BasePath(projectDir, state.ResolvedCommit)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(base, "both.txt")); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr strings.Builder
		opts := updateTestOptions(&stdout, &stderr)
		opts.Force = true
		result, err := Update(projectDir, opts)
		if err != nil {
			t.Fatalf("Update error: %v", err)
		}
		if len(result.Conflicts) != 1 {
			t.Errorf("Conflicts = %v, want one overlay conflict", result.Conflicts)
		}
		if !strings.Contains(stderr.String(), "no base copy") {
			t.Errorf("stderr lacks the overlay-fallback note: %q", stderr.String())
		}
	})
}

func TestBootstrapSymlinkAndSpecial(t *testing.T) {
	t.Run("escaping symlink refused", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"ok.txt": "ok\n"})
		outside := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(src, "evil.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		if err := Bootstrap(src, target, testOptions(&stdout)); err == nil {
			t.Error("Bootstrap with escaping symlink succeeded, want error")
		} else if !strings.Contains(err.Error(), "escapes") && !strings.Contains(err.Error(), "symlink") {
			t.Errorf("error = %q, want a symlink-escape refusal", err)
		}
	})
	t.Run("in-root symlink dereferenced", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"real.txt": "content\n"})
		if err := os.Symlink(filepath.Join(src, "real.txt"), filepath.Join(src, "link.txt")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		projectDir := bootstrapProject(t, src)
		got, err := os.ReadFile(filepath.Join(projectDir, "link.txt"))
		if err != nil {
			t.Fatalf("reading dereferenced link: %v", err)
		}
		if string(got) != "content\n" {
			t.Errorf("link content = %q, want %q", got, "content\n")
		}
	})
	t.Run("fifo skipped", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"ok.txt": "ok\n"})
		if !makeTestFifo(t, filepath.Join(src, "pipe")) {
			t.Skip("fifos unavailable on this platform")
		}
		projectDir := bootstrapProject(t, src)
		if _, err := os.Lstat(filepath.Join(projectDir, "pipe")); err == nil {
			t.Error("fifo copied into project, want it skipped")
		}
		if _, err := os.Stat(filepath.Join(projectDir, "ok.txt")); err != nil {
			t.Errorf("regular file missing after fifo skip: %v", err)
		}
	})
	t.Run("absolute skip entry rejected", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"ok.txt": "ok\n"})
		target := filepath.Join(t.TempDir(), "proj")
		var stdout strings.Builder
		opts := testOptions(&stdout)
		opts.Skip = []string{"/abs/path"}
		if err := Bootstrap(src, target, opts); err == nil {
			t.Error("Bootstrap with absolute --skip succeeded, want error")
		}
	})
}
