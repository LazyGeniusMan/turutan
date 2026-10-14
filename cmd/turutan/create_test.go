// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateCmd(t *testing.T) {
	t.Run("scaffolds author project", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "my-template")
		cmd := newCreateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{dir})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("create error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".turutan.yml")); err != nil {
			t.Errorf("created project is missing .turutan.yml: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
			t.Errorf("created project is missing README.md: %v", err)
		}
	})
	t.Run("refuses non-empty without force", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "mine.txt"), []byte("mine"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := newCreateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{dir})
		if err := cmd.Execute(); err == nil {
			t.Error("create into non-empty dir succeeded, want error")
		}
		force := newCreateCmd()
		force.SetOut(&bytes.Buffer{})
		force.SetErr(&bytes.Buffer{})
		force.SetArgs([]string{dir, "--force"})
		if err := force.Execute(); err != nil {
			t.Fatalf("create --force error: %v", err)
		}
	})
}
