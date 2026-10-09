// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

// exitCodeOf maps a command error to its process exit code: nil is 0,
// scriptable outcomes keep their code, every other failure is 1.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	return 1
}

// bootstrapTestProject scaffolds a filesystem project from files and
// returns the template source dir and the project dir.
func bootstrapTestProject(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	src := writeTemplate(t, files)
	dst := filepath.Join(t.TempDir(), "proj")
	var stdout bytes.Buffer
	opts := scaffold.Options{
		NonInteractive: true,
		Defaults:       true,
		Engine:         "0.1.0",
		Stdout:         &stdout,
		Stderr:         &bytes.Buffer{},
		Stdin:          bytes.NewReader(nil),
	}
	if err := scaffold.Bootstrap(src, dst, opts); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	return src, dst
}

// runInProject chdirs into dir for the test, restoring afterward.
func runInProject(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

func TestCheckUpdateCmdExitCodes(t *testing.T) {
	t.Run("exit 0 when up to date", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newCheckUpdateCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("check-update error: %v (exit %d)", err, exitCodeOf(err))
		}
		if got := stdout.String(); !strings.Contains(got, "up to date") {
			t.Errorf("stdout = %q, want up-to-date line", got)
		}
	})
	t.Run("exit 2 when template moved", func(t *testing.T) {
		src, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newCheckUpdateCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		if exitCodeOf(err) != 2 {
			t.Fatalf("check-update err = %v (exit %d), want exit 2", err, exitCodeOf(err))
		}
		if got := stdout.String(); !strings.Contains(got, "update available") {
			t.Errorf("stdout = %q, want update-available line", got)
		}
	})
	t.Run("exit 1 without state", func(t *testing.T) {
		runInProject(t, t.TempDir())
		cmd := newCheckUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		if exitCodeOf(err) != 1 {
			t.Errorf("check-update err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
	t.Run("exit 1 with ref on filesystem", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newCheckUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--ref", "v1"})
		err := cmd.Execute()
		if exitCodeOf(err) != 1 {
			t.Errorf("check-update err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil is success", err: nil, want: 0},
		{name: "drift is scriptable", err: &exitError{code: exitDriftOrAvailable, msg: "x"}, want: 2},
		{name: "runtime error is 1", err: errNotImplemented, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
			if got := exitCodeOf(tt.err); got != tt.want {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
