// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

func advertisedFixture() []Ref {
	return []Ref{
		{Name: "HEAD", Target: "refs/heads/main"},
		{Name: "refs/heads/main", Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{Name: "refs/heads/master", Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{Name: "refs/tags/v1.2.0", Hash: "cccccccccccccccccccccccccccccccccccccccc"},
		{Name: "refs/tags/v2.1.0", Hash: "dddddddddddddddddddddddddddddddddddddddd"},
	}
}

func TestResolveRef(t *testing.T) {
	const url = "https://example.com/org/web.git"
	full := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name       string
		expr       string
		advertised []Ref
		want       string
		wantErr    bool
	}{
		{name: "empty selects HEAD target", expr: "", advertised: advertisedFixture(), want: full},
		{
			name: "empty falls back to main",
			expr: "",
			advertised: []Ref{
				{Name: "refs/heads/main", Hash: full},
			},
			want: full,
		},
		{
			name: "empty falls back to master",
			expr: "",
			advertised: []Ref{
				{Name: "refs/heads/other", Hash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
				{Name: "refs/heads/master", Hash: full},
			},
			want: full,
		},
		{name: "exact branch", expr: "master", advertised: advertisedFixture(), want: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "exact tag", expr: "v2.1.0", advertised: advertisedFixture(), want: "dddddddddddddddddddddddddddddddddddddddd"},
		{name: "full SHA passes through", expr: full, advertised: advertisedFixture(), want: full},
		{name: "short SHA resolves", expr: "aaaaaaa", advertised: advertisedFixture(), want: full},
		{name: "semver picks highest satisfying tag", expr: "^1.0", advertised: advertisedFixture(), want: "cccccccccccccccccccccccccccccccccccccccc"},
		{name: "semver caret major picks v2", expr: "^2.0", advertised: advertisedFixture(), want: "dddddddddddddddddddddddddddddddddddddddd"},
		{name: "unknown ref errors", expr: "nope", advertised: advertisedFixture(), wantErr: true},
		{name: "no default branch errors", expr: "", advertised: []Ref{{Name: "refs/heads/other", Hash: full}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveRef(url, tt.expr, tt.advertised)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveRef(%q) succeeded, want error", tt.expr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveRef(%q) error: %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("ResolveRef(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestResolveRefAmbiguousShortSHA(t *testing.T) {
	t.Run("ambiguous prefix is not resolved", func(t *testing.T) {
		advertised := []Ref{
			{Name: "refs/heads/a", Hash: "abcdef000000000000000000000000000000000000"},
			{Name: "refs/heads/b", Hash: "abcdef111111111111111111111111111111111111"},
		}
		if _, err := ResolveRef("https://example.com/r.git", "abcdef", advertised); err == nil {
			t.Error("ambiguous short SHA should not resolve")
		}
	})
}

func commitFile(t *testing.T, repoPath, name, content, message string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("opening repo %q: %v", repoPath, err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, name), []byte(content), 0o600); err != nil {
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
	return hash.String()
}

func initRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	disableGPGSign(t, repo)
	sha := commitFile(t, dir, "README.md", "hello\n", "initial")
	return dir, sha
}

func disableGPGSign(t *testing.T, repo *git.Repository) {
	t.Helper()
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("reading repo config: %v", err)
	}
	cfg.Commit.GpgSign = config.OptBoolFalse
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("disabling gpgSign: %v", err)
	}
}

func TestIsGitRepo(t *testing.T) {
	t.Run("worktree repo detected", func(t *testing.T) {
		dir, _ := initRepo(t)
		if !IsGitRepo(dir) {
			t.Errorf("IsGitRepo(%q) = false, want true", dir)
		}
	})
	t.Run("bare repo detected", func(t *testing.T) {
		dir := t.TempDir()
		bare := filepath.Join(dir, "repo.git")
		if _, err := git.PlainInit(bare, true); err != nil {
			t.Fatalf("init bare repo: %v", err)
		}
		if !IsGitRepo(bare) {
			t.Errorf("IsGitRepo(%q) = false, want true for bare repo", bare)
		}
	})
	t.Run("plain dir rejected", func(t *testing.T) {
		if IsGitRepo(t.TempDir()) {
			t.Error("IsGitRepo(plain dir) = true, want false")
		}
	})
}

func TestCloneFromLocalPath(t *testing.T) {
	t.Run("clone checks out HEAD by default", func(t *testing.T) {
		src, want := initRepo(t)
		dst := filepath.Join(t.TempDir(), "clone")
		got, err := Clone(context.Background(), src, "", dst, ShallowDepth)
		if err != nil {
			t.Fatalf("Clone error: %v", err)
		}
		if got != want {
			t.Errorf("Clone SHA = %q, want %q", got, want)
		}
		content, err := os.ReadFile(filepath.Join(dst, "README.md"))
		if err != nil {
			t.Fatalf("reading cloned file: %v", err)
		}
		if string(content) != "hello\n" {
			t.Errorf("cloned content = %q, want %q", content, "hello\n")
		}
	})
	t.Run("clone resolves branch ref", func(t *testing.T) {
		src, _ := initRepo(t)
		second := commitFile(t, src, "second.txt", "two\n", "second")
		dst := filepath.Join(t.TempDir(), "clone")
		got, err := Clone(context.Background(), src, "master", dst, ShallowDepth)
		if err != nil {
			if strings.Contains(err.Error(), "resolving ref") {
				got, err = Clone(context.Background(), src, "main", dst, ShallowDepth)
			}
			if err != nil {
				t.Fatalf("Clone with branch ref error: %v", err)
			}
		}
		if got != second {
			t.Errorf("Clone SHA = %q, want %q", got, second)
		}
	})
	t.Run("unknown ref fails clearly", func(t *testing.T) {
		src, _ := initRepo(t)
		if _, err := Clone(context.Background(), src, "does-not-exist", filepath.Join(t.TempDir(), "clone"), ShallowDepth); err == nil {
			t.Error("Clone with unknown ref succeeded, want error")
		}
	})
}

func TestResolveLocal(t *testing.T) {
	t.Run("empty resolves HEAD", func(t *testing.T) {
		dir, want := initRepo(t)
		got, err := ResolveLocal(dir, "")
		if err != nil {
			t.Fatalf("ResolveLocal error: %v", err)
		}
		if got != want {
			t.Errorf("ResolveLocal = %q, want %q", got, want)
		}
	})
	t.Run("unknown ref errors", func(t *testing.T) {
		dir, _ := initRepo(t)
		if _, err := ResolveLocal(dir, "does-not-exist"); err == nil {
			t.Error("ResolveLocal with unknown ref succeeded, want error")
		}
	})
	t.Run("non-repo errors", func(t *testing.T) {
		if _, err := ResolveLocal(t.TempDir(), ""); err == nil {
			t.Error("ResolveLocal on plain dir succeeded, want error")
		}
	})
	t.Run("bare repo resolves HEAD", func(t *testing.T) {
		dir, want := initRepo(t)
		bare := filepath.Join(dir, ".git")
		if isBare, err := IsBare(bare); err != nil || !isBare {
			t.Fatalf("IsBare(.git) = %v, %v; want true, nil", isBare, err)
		}
		got, err := ResolveLocal(bare, "")
		if err != nil {
			t.Fatalf("ResolveLocal on bare repo error: %v", err)
		}
		if got != want {
			t.Errorf("ResolveLocal on bare repo = %q, want %q", got, want)
		}
	})
}

func tagHEAD(t *testing.T, repoPath, name string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatalf("opening repo %q: %v", repoPath, err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("reading HEAD: %v", err)
	}
	if _, err := repo.CreateTag(name, head.Hash(), nil); err != nil {
		t.Fatalf("creating tag %q: %v", name, err)
	}
	return head.Hash().String()
}

func semverFixture(t *testing.T) (string, map[string]string) {
	t.Helper()
	dir, _ := initRepo(t)
	sha1 := tagHEAD(t, dir, "v1.2.0")
	hash2 := commitFile(t, dir, "v1.txt", "v1.5\n", "second")
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateTag("v1.5.0", plumbing.NewHash(hash2), nil); err != nil {
		t.Fatalf("creating tag v1.5.0: %v", err)
	}
	hash3 := commitFile(t, dir, "v2.txt", "v2.1\n", "third")
	if _, err := repo.CreateTag("v2.1.0", plumbing.NewHash(hash3), nil); err != nil {
		t.Fatalf("creating tag v2.1.0: %v", err)
	}
	return dir, map[string]string{"v1.2.0": sha1, "v1.5.0": hash2, "v2.1.0": hash3}
}

func TestResolveLocalSemver(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantTag string
		wantErr bool
	}{
		{name: "exact tag resolves", expr: "v1.2.0", wantTag: "v1.2.0"},
		{name: "caret range picks highest satisfying in major 1", expr: "^1.0", wantTag: "v1.5.0"},
		{name: "caret major picks v2", expr: "^2.0", wantTag: "v2.1.0"},
		{name: "tilde range picks patch within minor", expr: "~1.2.0", wantTag: "v1.2.0"},
		{name: "no satisfying tag errors", expr: "^3.0", wantErr: true},
		{name: "invalid constraint fails fast", expr: "does-not-exist", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, wantByTag := semverFixture(t)
			got, err := ResolveLocal(dir, tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveLocal(%q) succeeded, want error", tt.expr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveLocal(%q) error: %v", tt.expr, err)
			}
			want := wantByTag[tt.wantTag]
			if got != want {
				t.Errorf("ResolveLocal(%q) = %q, want %q (tag %s)", tt.expr, got, want, tt.wantTag)
			}
		})
	}
	t.Run("HEAD fallback unaffected by tags", func(t *testing.T) {
		dir, _ := semverFixture(t)
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		head, err := repo.Head()
		if err != nil {
			t.Fatal(err)
		}
		want := head.Hash().String()
		got, err := ResolveLocal(dir, "")
		if err != nil {
			t.Fatalf("ResolveLocal(HEAD) error: %v", err)
		}
		if got != want {
			t.Errorf("ResolveLocal(HEAD) = %q, want %q", got, want)
		}
	})
	t.Run("branch still resolves with tags present", func(t *testing.T) {
		dir, _ := semverFixture(t)
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		head, err := repo.Head()
		if err != nil {
			t.Fatal(err)
		}
		branch := head.Name().Short()
		got, err := ResolveLocal(dir, branch)
		if err != nil {
			t.Fatalf("ResolveLocal(%q) error: %v", branch, err)
		}
		if got != head.Hash().String() {
			t.Errorf("ResolveLocal(%q) = %q, want %q", branch, got, head.Hash().String())
		}
	})
}

func TestIsBare(t *testing.T) {
	t.Run("worktree is not bare", func(t *testing.T) {
		dir, _ := initRepo(t)
		if bare, err := IsBare(dir); err != nil || bare {
			t.Errorf("IsBare(worktree) = %v, %v; want false, nil", bare, err)
		}
	})
	t.Run("bare layout is bare", func(t *testing.T) {
		dir, _ := initRepo(t)
		if bare, err := IsBare(filepath.Join(dir, ".git")); err != nil || !bare {
			t.Errorf("IsBare(.git) = %v, %v; want true, nil", bare, err)
		}
	})
	t.Run("plain dir errors", func(t *testing.T) {
		if _, err := IsBare(t.TempDir()); err == nil {
			t.Error("IsBare on plain dir succeeded, want error")
		}
	})
}

func TestRedacted(t *testing.T) {
	t.Run("https userinfo stripped", func(t *testing.T) {
		got := redacted("https://token123@github.com/org/web.git")
		if strings.Contains(got, "token123") {
			t.Errorf("redacted URL still contains credentials: %q", got)
		}
		if !strings.Contains(got, "github.com/org/web.git") {
			t.Errorf("redacted URL lost the repo path: %q", got)
		}
	})
	t.Run("plain URL unchanged", func(t *testing.T) {
		if got := redacted("https://github.com/org/web.git"); got != "https://github.com/org/web.git" {
			t.Errorf("redacted = %q, want unchanged", got)
		}
	})
}

func TestIsScpLike(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "scp-like", in: "git@github.com:org/web.git", want: true},
		{name: "custom user", in: "deploy@example.com:org/web.git", want: true},
		{name: "ssh scheme is not scp-like", in: "ssh://git@example.com/org/web.git", want: false},
		{name: "https is not scp-like", in: "https://github.com/org/web.git", want: false},
		{name: "slash before at is local", in: "a/b@c:d", want: false},
		{name: "missing path", in: "git@github.com:", want: false},
		{name: "no colon", in: "git@github.com", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsScpLike(tt.in); got != tt.want {
				t.Errorf("IsScpLike(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestListRefsNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("network test skipped in -short mode")
	}
	t.Run("lists default template refs", func(t *testing.T) {
		refs, err := ListRefs(context.Background(), "https://github.com/LazyGeniusMan/turutan.git")
		if err != nil {
			t.Skipf("network unavailable: %v", err)
		}
		if len(refs) == 0 {
			t.Error("ListRefs returned no refs")
		}
	})
}
