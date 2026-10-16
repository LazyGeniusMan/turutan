// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	gitgo "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

type stubGitOperator struct {
	GitOperator
	cloneFrom string
	clonedURL string
	clonedRef string
	captured  string
	pushed    []string
}

func (s *stubGitOperator) Clone(_ context.Context, url, ref, dir string, _ int) (string, error) {
	s.clonedURL = url
	s.clonedRef = ref
	if err := copyDirAll(s.cloneFrom, dir); err != nil {
		return "", err
	}
	return "abc123", nil
}

func (s *stubGitOperator) Push(_ context.Context, dir, ref string, _ URI, _ AuthConfig) error {
	s.pushed = append(s.pushed, ref)
	if s.captured != "" {
		return CopyTree(dir, s.captured)
	}
	return nil
}

func copyDirAll(srcDir, destDir string) error {
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, full)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target, err := filter.SafeJoin(destDir, rel)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o750) // #nosec G122 -- test-only copy; target is SafeJoin-pinned to destDir under t.TempDir()
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(full) // #nosec G304, G122 -- test-only copy under t.TempDir() walking srcDir with symlinks skipped
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) // #nosec G703, G122 -- test-only copy; target is SafeJoin-pinned to destDir under t.TempDir()
	})
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	repo, err := gitgo.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit error: %v", err)
	}
	if err := disableGitSigning(repo, dir); err != nil {
		t.Fatalf("disable signing error: %v", err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree error: %v", err)
	}
	if err := worktree.AddWithOptions(&gitgo.AddOptions{All: true}); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	signature := &object.Signature{Name: "test", Email: "test@localhost", When: time.Now()}
	if _, err := worktree.Commit("fixture", &gitgo.CommitOptions{Author: signature}); err != nil {
		t.Fatalf("Commit error: %v", err)
	}
}

func TestGitBackendBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	remote := t.TempDir()
	oldSub := filepath.Join(remote, "templates", "demo")
	if err := os.MkdirAll(oldSub, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldSub, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, remote)

	src := t.TempDir()
	writeFixtureTemplate(t, src)
	wantDigest, err := DigestDir(src)
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubGitOperator{GitOperator: defaultGitOperator{}, cloneFrom: remote, captured: t.TempDir()}
	backend := NewGitBackend(stub)
	if backend.Scheme() != SchemeGit {
		t.Fatalf("Scheme() = %q, want git", backend.Scheme())
	}
	uri := mustParseURI(t, "git+https://example.com/org/repo.git?ref=templates/demo/1.0.0&subpath=templates/demo")
	digest, err := backend.Bundle(ctx, src, uri, AuthConfig{})
	if err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	if digest != wantDigest {
		t.Fatalf("Bundle digest = %q, want %q", digest, wantDigest)
	}
	if stub.clonedURL != "https://example.com/org/repo.git" {
		t.Fatalf("cloned %q, want the uri repository", stub.clonedURL)
	}
	if len(stub.pushed) != 1 || stub.pushed[0] != "templates/demo/1.0.0" {
		t.Fatalf("pushed tags %q, want the bundle tag", stub.pushed)
	}
	archive := filepath.Join(stub.captured, "templates", "demo", "releases", "1.0.0.tar.gz")
	data, err := os.ReadFile(archive) // #nosec G304 -- archive is joinSubpath-pinned under the stub capture dir
	if err != nil {
		t.Fatalf("release archive missing: %v", err)
	}
	dest := t.TempDir()
	if err := ExtractBundle(bytes.NewReader(data), dest); err != nil {
		t.Fatalf("ExtractBundle error: %v", err)
	}
	assertSameTree(t, src, dest)
	if _, err := os.Stat(filepath.Join(stub.captured, "templates", "demo", "stale.txt")); err != nil {
		t.Fatalf("unrelated subpath content should survive archive bundling: %v", err)
	}
}

func TestGitBackendFetch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	remote := t.TempDir()
	sub := filepath.Join(remote, "templates", "demo")
	if err := os.MkdirAll(filepath.Join(sub, "releases"), 0o750); err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	writeFixtureTemplate(t, fixture)
	blob, wantDigest, err := BundleBytes(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "releases", "1.0.0.tar.gz"), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, remote)
	stub := &stubGitOperator{GitOperator: defaultGitOperator{}, cloneFrom: remote}
	backend := NewGitBackend(stub)
	uri := mustParseURI(t, "git+https://example.com/org/repo.git?ref=templates/demo/1.0.0&subpath=templates/demo")
	dest := t.TempDir()
	if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if stub.clonedRef != "templates/demo/1.0.0" {
		t.Fatalf("cloned ref %q, want the uri ref", stub.clonedRef)
	}
	assertSameTree(t, fixture, dest)
	if got, err := DigestDir(dest); err != nil || got != wantDigest {
		t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, wantDigest)
	}
}

func TestCommitTagGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFixtureTemplate(t, dir)
	initGitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "turutan.yml"), []byte("name: v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sha, err := commitTagGitDir(dir, "templates/demo/2.0.0")
	if err != nil {
		t.Fatalf("commitTagGitDir error: %v", err)
	}
	if sha == "" {
		t.Fatalf("commitTagGitDir returned an empty sha")
	}
	repo, err := gitgo.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := repo.Tag("templates/demo/2.0.0")
	if err != nil {
		t.Fatalf("tag was not created: %v", err)
	}
	if tag.Name().Short() != "templates/demo/2.0.0" {
		t.Fatalf("tag name = %q", tag.Name().Short())
	}
}

func TestGitBackendBadTag(t *testing.T) {
	t.Parallel()
	if err := validGitTag("has space"); err == nil {
		t.Fatalf("validGitTag accepted a tag with a space")
	}
	if err := validGitTag(""); err == nil {
		t.Fatalf("validGitTag accepted an empty ref")
	}
	if err := validGitTag("templates/demo/1.0.0"); err != nil {
		t.Fatalf("validGitTag rejected a good tag: %v", err)
	}
}
