// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecureJoin(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "plain relative joins", path: "a/b.txt"},
		{name: "dot stays in root", path: "."},
		{name: "empty stays in root", path: ""},
		{name: "nested dots inside root join", path: "a/./b.txt"},
		{name: "absolute refused", path: "/etc/passwd", wantErr: true},
		{name: "parent escape refused", path: "../escape", wantErr: true},
		{name: "nested escape refused", path: "a/../../escape", wantErr: true},
		{name: "bare parent refused", path: "..", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SecureJoin(root, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SecureJoin(%q) = %q, want error", tt.path, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SecureJoin(%q) error: %v", tt.path, err)
			}
			if err := EnsureWithinRoot(root, got); err != nil {
				t.Errorf("SecureJoin(%q) = %q escapes root: %v", tt.path, got, err)
			}
		})
	}
}

func TestValidatePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{name: "recursive git ignore", pattern: "**/.git/**"},
		{name: "node modules ignore", pattern: "**/node_modules/**"},
		{name: "turutan dir", pattern: ".turutan/**"},
		{name: "preserve with class", pattern: "config/local.*.yml"},
		{name: "preserve tree", pattern: "data/**"},
		{name: "empty refused", pattern: "", wantErr: true},
		{name: "absolute refused", pattern: "/etc/**", wantErr: true},
		{name: "parent escape refused", pattern: "../secret/**", wantErr: true},
		{name: "nested escape refused", pattern: "a/../../b", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePattern(tt.pattern)
			if tt.wantErr && err == nil {
				t.Errorf("ValidatePattern(%q) succeeded, want error", tt.pattern)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePattern(%q) error: %v", tt.pattern, err)
			}
		})
	}
}

func TestValidateGlobs(t *testing.T) {
	t.Run("empty list valid", func(t *testing.T) {
		if err := ValidateGlobs(nil); err != nil {
			t.Errorf("ValidateGlobs(nil) error: %v", err)
		}
	})
	t.Run("one bad entry fails", func(t *testing.T) {
		if err := ValidateGlobs([]string{"data/**", "../escape"}); err == nil {
			t.Error("ValidateGlobs with escaping entry succeeded, want error")
		}
	})
}

func TestIsSpecialFile(t *testing.T) {
	t.Run("regular file is not special", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f.txt")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if IsSpecialFile(info) {
			t.Error("IsSpecialFile(regular) = true, want false")
		}
	})
	t.Run("directory is not special", func(t *testing.T) {
		info, err := os.Lstat(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if IsSpecialFile(info) {
			t.Error("IsSpecialFile(dir) = true, want false")
		}
	})
	t.Run("symlink is not special", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.txt")
		if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatal(err)
		}
		if IsSpecialFile(info) {
			t.Error("IsSpecialFile(symlink) = true, want false (symlinks are resolved, not skipped as special)")
		}
	})
	t.Run("fifo is special", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "pipe")
		if err := makeFifo(path); err != nil {
			t.Skipf("fifos unavailable: %v", err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !IsSpecialFile(info) {
			t.Error("IsSpecialFile(fifo) = false, want true")
		}
	})
}
