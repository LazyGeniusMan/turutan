// SPDX-License-Identifier: Apache-2.0

package template

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	transport "github.com/LazyGeniusMan/turutan/internal/git"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// initGitRepo creates a real git repository with one commit for offline
// fetch tests. It disables signing so hosts with commit.gpgSign pass.
func initGitRepo(t *testing.T, path string) string {
	t.Helper()
	repo, err := git.PlainInit(path, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Commit.GpgSign = config.OptBoolFalse
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "sub", "file.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("sub/file.txt"); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "turutan-test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return hash.String()
}

func TestFetchFilesystem(t *testing.T) {
	t.Run("whole tree", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
			t.Fatal(err)
		}
		src, err := ParseSource(root)
		if err != nil {
			t.Fatal(err)
		}
		if src.Kind != KindFilesystem {
			t.Fatalf("Kind = %q, want filesystem", src.Kind)
		}
		fetched, err := Fetch(src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		if fetched.Cleanup != nil {
			t.Error("filesystem fetch should not need cleanup")
		}
		if fetched.Dir != root {
			t.Errorf("Dir = %q, want %q", fetched.Dir, root)
		}
		if fetched.ResolvedCommit != "" {
			t.Errorf("ResolvedCommit = %q, want empty for filesystem", fetched.ResolvedCommit)
		}
	})
	t.Run("subpath scoped", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "mono", "api")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		src, err := ParseSource(filepath.Join(root, "mono") + "//api")
		if err != nil {
			t.Fatal(err)
		}
		fetched, err := Fetch(src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		if fetched.Dir != sub {
			t.Errorf("Dir = %q, want %q", fetched.Dir, sub)
		}
	})
	t.Run("ref rejected for filesystem", func(t *testing.T) {
		root := t.TempDir()
		src, err := ParseSource(root)
		if err != nil {
			t.Fatal(err)
		}
		src.RequestedRef = "main"
		if _, err := Fetch(src); err == nil {
			t.Error("filesystem fetch with ref succeeded, want error")
		}
	})
	t.Run("escaping symlink subpath refused", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		src, err := ParseSource(filepath.Join(root, "link"))
		if err != nil {
			t.Fatal(err)
		}
		// Bypass the parser (it sees a plain dir through the link) by
		// forcing the filesystem kind with a symlinked subpath.
		src.Kind = KindFilesystem
		src.Repo = root
		src.Subpath = "link"
		if _, err := Fetch(src); err == nil {
			t.Error("fetch through escaping symlink succeeded, want refusal")
		}
	})
}

func TestFetchLocalGit(t *testing.T) {
	t.Run("materializes HEAD without mutating source", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		want := initGitRepo(t, repoDir)
		src, err := ParseSource(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		if src.Kind != KindLocalGit {
			t.Fatalf("Kind = %q, want local-git", src.Kind)
		}
		fetched, err := Fetch(src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		if fetched.Cleanup == nil {
			t.Fatal("git fetch should return cleanup")
		}
		defer fetched.Cleanup()
		if fetched.ResolvedCommit != want {
			t.Errorf("ResolvedCommit = %q, want %q", fetched.ResolvedCommit, want)
		}
		content, err := os.ReadFile(filepath.Join(fetched.Dir, "sub", "file.txt"))
		if err != nil {
			t.Fatalf("reading fetched file: %v", err)
		}
		if string(content) != "hi\n" {
			t.Errorf("fetched content = %q, want %q", content, "hi\n")
		}
	})
	t.Run("subpath scoped", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, repoDir)
		src, err := ParseSource(repoDir + "//sub")
		if err != nil {
			t.Fatal(err)
		}
		fetched, err := Fetch(src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		defer fetched.Cleanup()
		if _, err := os.Stat(filepath.Join(fetched.Dir, "file.txt")); err != nil {
			t.Errorf("expected file in subpath-scoped fetch: %v", err)
		}
	})
}

func TestFetchRemoteNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	t.Run("resolves default ref", func(t *testing.T) {
		sha, err := transport.ResolveRemoteRef(DefaultSource().Repo, DefaultRef)
		if err != nil {
			t.Skipf("network unavailable: %v", err)
		}
		if len(sha) != 40 {
			t.Errorf("resolved SHA = %q, want 40-hex", sha)
		}
	})
}

func TestFetchDefaultCacheOffline(t *testing.T) {
	t.Run("warm cache serves full-SHA default without network", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")
		sha := "8f5d1c3e2e599cc2fdd5dca7f6afe075305b8ddb"
		cached := filepath.Join(home, ".cache", "turutan", "default", sha)
		if err := os.MkdirAll(cached, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cached, "go.mod"), []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		src := DefaultSource()
		src.RequestedRef = sha
		fetched, err := Fetch(src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		if fetched.Cleanup != nil {
			defer fetched.Cleanup()
		}
		if fetched.Dir != cached {
			t.Errorf("Dir = %q, want cache dir %q", fetched.Dir, cached)
		}
		if fetched.ResolvedCommit != sha {
			t.Errorf("ResolvedCommit = %q, want %q", fetched.ResolvedCommit, sha)
		}
	})
}
