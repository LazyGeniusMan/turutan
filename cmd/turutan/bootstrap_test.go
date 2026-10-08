// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeTemplate writes a minimal on-disk template for CLI tests.
func writeTemplate(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBootstrapCmd(t *testing.T) {
	oldNonInteractive := nonInteractive
	nonInteractive = true
	defer func() { nonInteractive = oldNonInteractive }()

	t.Run("bootstraps from filesystem with defaults", func(t *testing.T) {
		src := writeTemplate(t, map[string]string{
			".turutan.yml": "min-engine: \">=0.1.0\"\n",
			"go.mod.tmpl":  "module {{.project_name}}\n",
		})
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newBootstrapCmd()
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{src, dst, "--defaults"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("bootstrap error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "module proj\n" {
			t.Errorf("go.mod = %q, want defaulted module", got)
		}
	})
	t.Run("invalid conflict mode fails", func(t *testing.T) {
		cmd := newBootstrapCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"./whatever", "--conflict", "merge"})
		if err := cmd.Execute(); err == nil {
			t.Error("bootstrap with bad --conflict succeeded, want error")
		}
	})
	t.Run("missing answers fail non-interactively", func(t *testing.T) {
		src := writeTemplate(t, map[string]string{"go.mod.tmpl": "module {{.project_name}}\n"})
		cmd := newBootstrapCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{src, filepath.Join(t.TempDir(), "p")})
		if err := cmd.Execute(); err == nil {
			t.Error("bootstrap without --defaults/--answers-file succeeded, want error")
		}
	})
}
