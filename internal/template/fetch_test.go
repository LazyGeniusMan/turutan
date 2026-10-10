// SPDX-License-Identifier: Apache-2.0

package template

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	transport "github.com/LazyGeniusMan/turutan/internal/git"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing/object"
)

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

func commitSymlink(t *testing.T, repoDir, name, target string) {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repoDir, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("add symlink "+name, &git.CommitOptions{
		Author: &object.Signature{Name: "turutan-test", Email: "test@example.com", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
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
		fetched, err := Fetch(context.Background(), src)
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
		fetched, err := Fetch(context.Background(), src)
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
		if _, err := Fetch(context.Background(), src); err == nil {
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
		src.Kind = KindFilesystem
		src.Repo = root
		src.Subpath = "link"
		if _, err := Fetch(context.Background(), src); err == nil {
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
		fetched, err := Fetch(context.Background(), src)
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
		fetched, err := Fetch(context.Background(), src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		defer fetched.Cleanup()
		if _, err := os.Stat(filepath.Join(fetched.Dir, "file.txt")); err != nil {
			t.Errorf("expected file in subpath-scoped fetch: %v", err)
		}
	})
	t.Run("symlink outside clone refused", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, repoDir)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		commitSymlink(t, repoDir, "escape", outside)
		src, err := ParseSource(repoDir + "//escape")
		if err != nil {
			t.Fatal(err)
		}
		fetched, err := Fetch(context.Background(), src)
		if err == nil {
			if fetched.Cleanup != nil {
				defer fetched.Cleanup()
			}
			t.Fatal("fetch through symlink escaping clone succeeded, want refusal")
		}
	})
	t.Run("symlink inside clone still works", func(t *testing.T) {
		repoDir := filepath.Join(t.TempDir(), "repo")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		initGitRepo(t, repoDir)
		if err := os.MkdirAll(filepath.Join(repoDir, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "real", "file.txt"), []byte("inside\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainOpen(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		worktree, err := repo.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(repoDir, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := worktree.Add("real/file.txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := worktree.Add("link"); err != nil {
			t.Fatal(err)
		}
		if _, err := worktree.Commit("add inside symlink", &git.CommitOptions{
			Author: &object.Signature{Name: "turutan-test", Email: "test@example.com", When: time.Now()},
		}); err != nil {
			t.Fatal(err)
		}
		src, err := ParseSource(repoDir + "//link")
		if err != nil {
			t.Fatal(err)
		}
		fetched, err := Fetch(context.Background(), src)
		if err != nil {
			t.Fatalf("Fetch error: %v", err)
		}
		defer fetched.Cleanup()
		content, err := os.ReadFile(filepath.Join(fetched.Dir, "file.txt"))
		if err != nil {
			t.Fatalf("reading through inside symlink: %v", err)
		}
		if string(content) != "inside\n" {
			t.Errorf("content = %q, want %q", content, "inside\n")
		}
	})
}

func commitTaggedFile(t *testing.T, repoDir, name, content, message, tag string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(name); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{Name: "turutan-test", Email: "test@example.com", When: time.Now()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTag(tag, hash, nil); err != nil {
		t.Fatalf("creating tag %q: %v", tag, err)
	}
	return hash.String()
}

func TestFetchLocalGitSemver(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantTag string
		wantErr bool
	}{
		{name: "exact tag resolves", ref: "v1.2.0", wantTag: "v1.2.0"},
		{name: "caret range picks highest v1", ref: "^1.0", wantTag: "v1.5.0"},
		{name: "caret major picks v2", ref: "^2.0", wantTag: "v2.1.0"},
		{name: "no satisfying tag errors", ref: "^3.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoDir := filepath.Join(t.TempDir(), "repo")
			if err := os.MkdirAll(repoDir, 0o755); err != nil {
				t.Fatal(err)
			}
			sha1 := initGitRepo(t, repoDir)
			repo, err := git.PlainOpen(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			head, err := repo.Head()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreateTag("v1.2.0", head.Hash(), nil); err != nil {
				t.Fatal(err)
			}
			sha2 := commitTaggedFile(t, repoDir, "v1.txt", "v1.5\n", "second", "v1.5.0")
			sha3 := commitTaggedFile(t, repoDir, "v2.txt", "v2.1\n", "third", "v2.1.0")
			wantByTag := map[string]string{"v1.2.0": sha1, "v1.5.0": sha2, "v2.1.0": sha3}
			src, err := ParseSource(repoDir + "?ref=" + tt.ref)
			if err != nil {
				t.Fatal(err)
			}
			if src.Kind != KindLocalGit {
				t.Fatalf("Kind = %q, want local-git", src.Kind)
			}
			fetched, err := Fetch(context.Background(), src)
			if tt.wantErr {
				if err == nil {
					if fetched.Cleanup != nil {
						defer fetched.Cleanup()
					}
					t.Errorf("Fetch with ref %q succeeded, want error", tt.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("Fetch with ref %q error: %v", tt.ref, err)
			}
			defer fetched.Cleanup()
			want := wantByTag[tt.wantTag]
			if fetched.ResolvedCommit != want {
				t.Errorf("ResolvedCommit = %q, want %q (tag %s)", fetched.ResolvedCommit, want, tt.wantTag)
			}
		})
	}
}

func TestFetchRemoteNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	t.Run("resolves default ref", func(t *testing.T) {
		sha, err := transport.ResolveRemoteRef(context.Background(), DefaultSource().Repo, DefaultRef)
		if err != nil {
			t.Skipf("network unavailable: %v", err)
		}
		if len(sha) != 40 {
			t.Errorf("resolved SHA = %q, want 40-hex", sha)
		}
	})
}

func TestFetchDefaultCacheOfflineFloatingTag(t *testing.T) {
	const bogusRef = "turutan-test-no-such-ref"
	t.Run("warm cache serves floating tag when resolve fails", func(t *testing.T) {
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
		src.RequestedRef = bogusRef
		fetched, err := Fetch(context.Background(), src)
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
	t.Run("cold cache preserves offline error verbatim", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")
		src := DefaultSource()
		src.RequestedRef = bogusRef
		_, err := Fetch(context.Background(), src)
		if err == nil {
			t.Fatal("Fetch succeeded with cold cache, want offline error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "default template unavailable offline") {
			t.Errorf("error = %q, want it to contain %q", msg, "default template unavailable offline")
		}
		if !strings.Contains(msg, bogusRef) {
			t.Errorf("error = %q, want it to name ref %q", msg, bogusRef)
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
		fetched, err := Fetch(context.Background(), src)
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
