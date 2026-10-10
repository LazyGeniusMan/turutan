// SPDX-License-Identifier: Apache-2.0

package template

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeLocalFixture(t *testing.T, parent, name string, withGit bool) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if withGit {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseSourceSpecExamples(t *testing.T) {
	workdir := t.TempDir()
	makeLocalFixture(t, workdir, "local", false)
	if err := os.MkdirAll(filepath.Join(filepath.Dir(workdir), "tpl"), 0o755); err != nil {
		t.Fatal(err)
	}
	abs := makeLocalFixture(t, workdir, "abs", false)
	makeLocalFixture(t, workdir, filepath.Join("mono", "services", "api"), false)
	gitDir := makeLocalFixture(t, workdir, "withgit", true)
	t.Chdir(workdir)

	tests := []struct {
		name    string
		raw     string
		kind    SourceKind
		repo    string
		subpath string
		ref     string
		depth   int
	}{
		{
			name: "remote git with subpath and tag ref",
			raw:  "git::https://github.com/org/web.git//react?ref=v1.2.0",
			kind: KindRemoteGit, repo: "https://github.com/org/web.git", subpath: "react", ref: "v1.2.0", depth: DefaultDepth,
		},
		{
			name: "scp-like ssh with nested subpath",
			raw:  "git@github.com:org/web.git//web/react?ref=main",
			kind: KindRemoteGit, repo: "git@github.com:org/web.git", subpath: "web/react", ref: "main", depth: DefaultDepth,
		},
		{
			name: "remote git with semver constraint ref",
			raw:  "https://github.com/org/mono.git//services/api?ref=^2.1",
			kind: KindRemoteGit, repo: "https://github.com/org/mono.git", subpath: "services/api", ref: "^2.1", depth: DefaultDepth,
		},
		{
			name: "relative local dir",
			raw:  "./local",
			kind: KindFilesystem, repo: "./local", subpath: "", ref: "", depth: DefaultDepth,
		},
		{
			name: "parent local dir",
			raw:  "../tpl",
			kind: KindFilesystem, repo: "../tpl", subpath: "", ref: "", depth: DefaultDepth,
		},
		{
			name: "absolute local dir",
			raw:  abs,
			kind: KindFilesystem, repo: abs, subpath: "", ref: "", depth: DefaultDepth,
		},
		{
			name: "file URL",
			raw:  "file://" + abs,
			kind: KindFilesystem, repo: abs, subpath: "", ref: "", depth: DefaultDepth,
		},
		{
			name: "filesystem with subpath",
			raw:  "./mono//services/api",
			kind: KindFilesystem, repo: "./mono", subpath: "services/api", ref: "", depth: DefaultDepth,
		},
		{
			name: "local git worktree",
			raw:  gitDir,
			kind: KindLocalGit, repo: gitDir, subpath: "", ref: "", depth: DefaultDepth,
		},
		{
			name: "local git with subpath and ref",
			raw:  gitDir + "//sub?ref=main",
			kind: KindLocalGit, repo: gitDir, subpath: "sub", ref: "main", depth: DefaultDepth,
		},
		{
			name: "explicit depth",
			raw:  "git::https://github.com/org/web.git//react?ref=v1.2.0&depth=5",
			kind: KindRemoteGit, repo: "https://github.com/org/web.git", subpath: "react", ref: "v1.2.0", depth: 5,
		},
		{
			name: "default depth is one",
			raw:  "git::https://github.com/org/web.git",
			kind: KindRemoteGit, repo: "https://github.com/org/web.git", subpath: "", ref: "", depth: DefaultDepth,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSource(tt.raw)
			if err != nil {
				t.Fatalf("ParseSource(%q) error: %v", tt.raw, err)
			}
			if got.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.kind)
			}
			if got.Repo != tt.repo {
				t.Errorf("Repo = %q, want %q", got.Repo, tt.repo)
			}
			if got.Subpath != tt.subpath {
				t.Errorf("Subpath = %q, want %q", got.Subpath, tt.subpath)
			}
			if got.RequestedRef != tt.ref {
				t.Errorf("RequestedRef = %q, want %q", got.RequestedRef, tt.ref)
			}
			if got.Depth != tt.depth {
				t.Errorf("Depth = %d, want %d", got.Depth, tt.depth)
			}
		})
	}
}

func TestParseSourceFailures(t *testing.T) {
	workdir := t.TempDir()
	plain := makeLocalFixture(t, workdir, "plain", false)
	t.Chdir(workdir)

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty source", raw: ""},
		{name: "bare git prefix", raw: "git::"},
		{name: "nonexistent local path", raw: "./does-not-exist"},
		{name: "unknown query parameter", raw: "git::https://github.com/org/web.git?bogus=1"},
		{name: "zero depth", raw: "git::https://github.com/org/web.git?depth=0"},
		{name: "negative depth", raw: "git::https://github.com/org/web.git?depth=-3"},
		{name: "non-numeric depth", raw: "git::https://github.com/org/web.git?depth=full"},
		{name: "empty subpath", raw: "git::https://github.com/org/web.git//"},
		{name: "escaping subpath", raw: "git::https://github.com/org/web.git//../evil"},
		{name: "forced git on plain dir", raw: "git::" + plain},
		{name: "unsupported scheme", raw: "ftp://example.com/repo.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseSource(tt.raw); err == nil {
				t.Errorf("ParseSource(%q) succeeded, want error", tt.raw)
			}
		})
	}
}

func TestParseSourceAtRef(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantErr  string
		wantRepo string
	}{
		{name: "git prefix at ref", raw: "git::https://github.com/org/web.git@main", wantErr: "?ref="},
		{name: "bare https at ref", raw: "https://github.com/org/web.git@main", wantErr: "?ref="},
		{name: "at ref with subpath", raw: "git::https://github.com/org/web.git@v1.2.0//react", wantErr: "?ref="},
		{name: "ssh scheme at ref", raw: "ssh://git@github.com/org/web.git@main", wantErr: "?ref="},
		{name: "scp-like at ref", raw: "git@github.com:org/web.git@main", wantErr: "?ref="},
		{name: "forced git scp-like at ref", raw: "git::git@github.com:org/web.git@main", wantErr: "?ref="},
		{name: "scp-like at ref with subpath", raw: "git@github.com:org/web.git@v1.2.0//react", wantErr: "?ref="},
		{
			name:     "userinfo remote still parses",
			raw:      "https://" + "user" + ":" + "redacted" + "@github.com/org/web.git//sub?ref=main",
			wantRepo: "https://user:redacted@github.com/org/web.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			got, err := ParseSource(tt.raw)
			if tt.wantErr != "" {
				if !is.Error(err) {
					return
				}
				is.Contains(err.Error(), tt.wantErr)
				return
			}
			if !is.NoError(err) {
				return
			}
			is.Equal(tt.wantRepo, got.Repo)
			is.Equal("sub", got.Subpath)
			is.Equal("main", got.RequestedRef)
		})
	}
}

func TestParseSourceDefaultAlias(t *testing.T) {
	t.Run("default alias resolves to built-in remote", func(t *testing.T) {
		got, err := ParseSource("default")
		if err != nil {
			t.Fatalf("ParseSource(default) error: %v", err)
		}
		want := DefaultSource()
		if got.Kind != want.Kind || got.Repo != want.Repo || got.Subpath != want.Subpath || got.RequestedRef != want.RequestedRef {
			t.Errorf("ParseSource(default) = %+v, want %+v", got, want)
		}
	})
	t.Run("default source is remote git with stable ref", func(t *testing.T) {
		src := DefaultSource()
		if src.Kind != KindRemoteGit {
			t.Errorf("default Kind = %q, want remote-git", src.Kind)
		}
		if src.RequestedRef != DefaultRef {
			t.Errorf("default RequestedRef = %q, want %q", src.RequestedRef, DefaultRef)
		}
		if !IsDefaultSource(src) {
			t.Error("IsDefaultSource(DefaultSource()) = false, want true")
		}
	})
	t.Run("non-default remote is not default", func(t *testing.T) {
		src, err := ParseSource("git::https://github.com/org/web.git//react?ref=v1.2.0")
		if err != nil {
			t.Fatal(err)
		}
		if IsDefaultSource(src) {
			t.Error("IsDefaultSource(other) = true, want false")
		}
	})
}

func TestResolveAlias(t *testing.T) {
	t.Run("empty resolves to default", func(t *testing.T) {
		got, err := ResolveAlias("")
		if err != nil {
			t.Fatalf("ResolveAlias empty error: %v", err)
		}
		if !IsDefaultSource(got) {
			t.Errorf("ResolveAlias(\"\") = %+v, want default source", got)
		}
	})
	t.Run("default alias resolves to default", func(t *testing.T) {
		got, err := ResolveAlias(DefaultAlias)
		if err != nil {
			t.Fatalf("ResolveAlias(default) error: %v", err)
		}
		if !IsDefaultSource(got) {
			t.Errorf("ResolveAlias(default) = %+v, want default source", got)
		}
	})
	t.Run("other input parses normally", func(t *testing.T) {
		got, err := ResolveAlias("git::https://github.com/org/web.git//react?ref=v1.2.0")
		if err != nil {
			t.Fatalf("ResolveAlias(other) error: %v", err)
		}
		if IsDefaultSource(got) {
			t.Errorf("ResolveAlias(other) = %+v, want non-default source", got)
		}
	})
}
