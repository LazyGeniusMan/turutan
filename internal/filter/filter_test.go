// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{name: "double-star git dir", pattern: "**/.git/**", path: "a/.git/config", want: true},
		{name: "double-star suffix", pattern: "**/node_modules/**", path: "node_modules/pkg/index.js", want: true},
		{name: "turutan meta dir", pattern: ".turutan/**", path: ".turutan/templates/abc", want: true},
		{name: "no match", pattern: "config/local.*.yml", path: "config/remote.yml", want: false},
		{name: "single star segment", pattern: "config/local.*.yml", path: "config/local.dev.yml", want: true},
		{name: "star does not cross separator", pattern: "data/*.json", path: "data/a/b.json", want: false},
		{name: "double star crosses separators", pattern: "data/**", path: "data/a/b.json", want: true},
		{name: "exact file", pattern: "go.mod", path: "go.mod", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Match(tt.pattern, tt.path)
			if err != nil {
				t.Fatalf("Match(%q, %q) error: %v", tt.pattern, tt.path, err)
			}
			if got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchInvalidPattern(t *testing.T) {
	t.Run("unclosed class", func(t *testing.T) {
		if _, err := Match("[unclosed", "x"); err == nil {
			t.Error("Match with invalid pattern should return an error")
		}
	})
}

func TestMatchAny(t *testing.T) {
	t.Run("empty patterns never match", func(t *testing.T) {
		got, err := MatchAny(nil, "go.mod")
		if err != nil {
			t.Fatalf("MatchAny error: %v", err)
		}
		if got {
			t.Error("MatchAny(nil, ...) = true, want false")
		}
	})
	t.Run("second pattern matches", func(t *testing.T) {
		got, err := MatchAny([]string{"*.txt", "data/**"}, "data/a.json")
		if err != nil {
			t.Fatalf("MatchAny error: %v", err)
		}
		if !got {
			t.Error("MatchAny should be true when the second pattern matches")
		}
	})
}

func TestToSlash(t *testing.T) {
	t.Run("separators normalized", func(t *testing.T) {
		got := ToSlash(filepath.Join("a", "b", "c"))
		if got != "a/b/c" {
			t.Errorf("ToSlash = %q, want %q", got, "a/b/c")
		}
	})
}

func TestSafeJoin(t *testing.T) {
	root := string(filepath.Separator) + filepath.Join("tmp", "root")
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "plain relative", path: "go.mod", wantErr: false},
		{name: "nested relative", path: filepath.Join("a", "b.txt"), wantErr: false},
		{name: "dot is root", path: ".", wantErr: false},
		{name: "empty is root", path: "", wantErr: false},
		{name: "parent escape refused", path: "../evil", wantErr: true},
		{name: "nested parent escape refused", path: filepath.Join("a", "..", "..", "evil"), wantErr: true},
		{name: "absolute refused", path: filepath.Join(string(filepath.Separator), "etc", "passwd"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeJoin(root, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SafeJoin(%q, %q) succeeded, want refusal", root, tt.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("SafeJoin(%q, %q) error: %v", root, tt.path, err)
			}
			if err := EnsureWithinRoot(root, got); err != nil {
				t.Errorf("SafeJoin result %q escapes root: %v", got, err)
			}
		})
	}
}

func TestEnsureWithinRootRefusesEscapingSymlink(t *testing.T) {
	t.Run("symlink outside root refused", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		secret := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(secret, link); err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatal(err)
		}
		if err := EnsureWithinRoot(root, resolved); err == nil {
			t.Error("EnsureWithinRoot accepted a symlink target outside root")
		}
	})
	t.Run("symlink inside root allowed", func(t *testing.T) {
		root := t.TempDir()
		inner := filepath.Join(root, "inner.txt")
		if err := os.WriteFile(inner, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(inner, link); err != nil {
			t.Fatal(err)
		}
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatal(err)
		}
		if err := EnsureWithinRoot(root, resolved); err != nil {
			t.Errorf("EnsureWithinRoot refused an inside-root symlink: %v", err)
		}
	})
}
