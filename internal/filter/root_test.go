// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileWithinRoot(t *testing.T) {
	t.Run("round trip inside root", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteFileWithinRoot(dir, "a/b.txt", []byte("hello\n"), 0o644); err != nil {
			t.Fatalf("WriteFileWithinRoot error: %v", err)
		}
		got, err := ReadFileWithinRoot(dir, "a/b.txt")
		if err != nil {
			t.Fatalf("ReadFileWithinRoot error: %v", err)
		}
		if string(got) != "hello\n" {
			t.Errorf("content = %q, want %q", got, "hello\n")
		}
	})
	t.Run("lexical escape refused", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := ReadFileWithinRoot(dir, "../escape.txt"); err == nil {
			t.Error("ReadFileWithinRoot(../escape) succeeded, want error")
		}
		if err := WriteFileWithinRoot(dir, "../escape.txt", []byte("x"), 0o644); err == nil {
			t.Error("WriteFileWithinRoot(../escape) succeeded, want error")
		}
		if err := RemoveWithinRoot(dir, "../escape.txt"); err == nil {
			t.Error("RemoveWithinRoot(../escape) succeeded, want error")
		}
	})
	t.Run("escaping symlink refused", func(t *testing.T) {
		dir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "link.txt")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := StatWithinRoot(dir, "link.txt"); err != nil {
			t.Fatalf("StatWithinRoot(link) error: %v", err)
		}
		if _, err := ReadFileWithinRoot(dir, "link.txt"); err == nil {
			t.Error("ReadFileWithinRoot(escaping symlink) succeeded, want error")
		}
	})
	t.Run("absolute path refused", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := ReadFileWithinRoot(dir, "/etc/hostname"); err == nil {
			t.Error("ReadFileWithinRoot(absolute) succeeded, want error")
		}
	})
}

func TestRemoveWithinRoot(t *testing.T) {
	t.Run("removes inside root", func(t *testing.T) {
		dir := t.TempDir()
		if err := WriteFileWithinRoot(dir, "gone.txt", []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RemoveWithinRoot(dir, "gone.txt"); err != nil {
			t.Fatalf("RemoveWithinRoot error: %v", err)
		}
		if _, err := ReadFileWithinRoot(dir, "gone.txt"); err == nil {
			t.Error("file still readable after remove")
		}
	})
}
