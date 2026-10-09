// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func TestDiffCmdExitCodes(t *testing.T) {
	t.Run("exit 0 with no drift", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newDiffCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("diff error: %v (exit %d)", err, exitCodeOf(err))
		}
		if got := stdout.String(); !strings.Contains(got, "no drift") {
			t.Errorf("stdout = %q, want no-drift line", got)
		}
	})
	t.Run("exit 2 on local edit with unified body", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "aaa\nbbb\nccc\nddd\n"})
		if err := os.WriteFile(filepath.Join(dst, "hello.txt"), []byte("aaa\nBBB\nccc\nddd\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newDiffCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		if exitCodeOf(err) != 2 {
			t.Fatalf("diff err = %v (exit %d), want exit 2", err, exitCodeOf(err))
		}
		got := stdout.String()
		for _, want := range []string{"@@", "--- a/hello.txt", "+++ b/hello.txt", "-BBB", "+bbb"} {
			if !strings.Contains(got, want) {
				t.Errorf("stdout missing %q:\n%s", want, got)
			}
		}
	})
	t.Run("exit 2 on template change", func(t *testing.T) {
		src, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newDiffCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); exitCodeOf(err) != 2 {
			t.Errorf("diff err = %v (exit %d), want exit 2", err, exitCodeOf(err))
		}
	})
	t.Run("exit 1 without state", func(t *testing.T) {
		runInProject(t, t.TempDir())
		cmd := newDiffCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		if exitCodeOf(err) != 1 {
			t.Errorf("diff err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
	t.Run("no-pager flag accepted off TTY", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(dst, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newDiffCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--no-pager"})
		if err := cmd.Execute(); exitCodeOf(err) != 2 {
			t.Errorf("diff err = %v (exit %d), want exit 2", err, exitCodeOf(err))
		}
		if !strings.Contains(stdout.String(), "@@") {
			t.Errorf("stdout = %q, want plain unified diff", stdout.String())
		}
	})
	t.Run("lock untouched by diff", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(dst, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(dst, config.LockFileName))
		if err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newDiffCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		_ = cmd.Execute()
		after, err := os.ReadFile(filepath.Join(dst, config.LockFileName))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Error("diff mutated .turutan.lock")
		}
	})
}
