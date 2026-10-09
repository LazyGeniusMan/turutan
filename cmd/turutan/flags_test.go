// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// verboseTemplate is a minimal renderable template for flag tests.
func verboseTemplate(t *testing.T) string {
	t.Helper()
	return writeTemplate(t, map[string]string{
		".turutan.yml": "min-engine: \">=0.1.0\"\n",
		"go.mod.tmpl":  "module {{.project_name}}\n",
	})
}

func TestVerboseBootstrapLogs(t *testing.T) {
	oldNonInteractive, oldVerbose := nonInteractive, verbose
	nonInteractive, verbose = true, true
	defer func() { nonInteractive, verbose = oldNonInteractive, oldVerbose }()

	cmd := newBootstrapCmd()
	cmd.SetOut(&bytes.Buffer{})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{verboseTemplate(t), filepath.Join(t.TempDir(), "proj"), "--defaults"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bootstrap error: %v", err)
	}
	for _, want := range []string{"bootstrap: source", "bootstrap: fetched", "bootstrap: wrote"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("verbose stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestQuietBootstrapSilent(t *testing.T) {
	oldNonInteractive, oldVerbose := nonInteractive, verbose
	nonInteractive, verbose = true, false
	defer func() { nonInteractive, verbose = oldNonInteractive, oldVerbose }()

	cmd := newBootstrapCmd()
	cmd.SetOut(&bytes.Buffer{})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{verboseTemplate(t), filepath.Join(t.TempDir(), "proj"), "--defaults"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bootstrap error: %v", err)
	}
	if got := strings.TrimSpace(stderr.String()); got != "" {
		t.Errorf("quiet stderr = %q, want empty", got)
	}
}

func TestVerboseCheckUpdateLogs(t *testing.T) {
	oldNonInteractive, oldVerbose := nonInteractive, verbose
	nonInteractive, verbose = true, true
	defer func() { nonInteractive, verbose = oldNonInteractive, oldVerbose }()

	_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
	runInProject(t, dst)
	cmd := newCheckUpdateCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("check-update error: %v", err)
	}
	for _, want := range []string{"check-update: stored", "check-update: fresh"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("verbose stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestNoColorDiffPlain(t *testing.T) {
	oldNonInteractive, oldNoColor, oldVerbose := nonInteractive, noColor, verbose
	nonInteractive, noColor, verbose = true, true, false
	defer func() { nonInteractive, noColor, verbose = oldNonInteractive, oldNoColor, oldVerbose }()

	_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "aaa\nbbb\nccc\nddd\n"})
	if err := os.WriteFile(filepath.Join(dst, "hello.txt"), []byte("aaa\nBBB\nccc\nddd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runInProject(t, dst)
	cmd := newDiffCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); exitCodeOf(err) != 2 {
		t.Fatalf("diff err = %v (exit %d), want exit 2", err, exitCodeOf(err))
	}
	if strings.Contains(stdout.String(), "\x1b") {
		t.Errorf("diff stdout with --no-color contains ANSI escapes:\n%q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "@@") {
		t.Errorf("diff stdout = %q, want unified body", stdout.String())
	}
}
