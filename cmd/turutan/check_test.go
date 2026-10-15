// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*exitError](err); ok {
		return ee.code
	}
	return 1
}

func initTestProject(t *testing.T, files map[string]string) (string, string, string) {
	t.Helper()
	regDir, regs, flag := makeCmdRegistry(t, "", files)
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
	if err := scaffold.Init(context.Background(), dst, scaffoldRef(t, flag), opts); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	useCmdRegistryConfig(t, regs)
	return regDir, dst, regs
}

func runInProject(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

func TestCheckCmdExitCodes(t *testing.T) {
	t.Run("exit 0 when up to date", func(t *testing.T) {
		_, dst, _ := initTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newCheckCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("check error: %v (exit %d)", err, exitCodeOf(err))
		}
		if got := stdout.String(); !strings.Contains(got, "up to date") {
			t.Errorf("stdout = %q, want up-to-date line", got)
		}
	})
	t.Run("exit 2 when template released", func(t *testing.T) {
		regDir, dst, _ := initTestProject(t, map[string]string{"hello.txt": "v1\n"})
		updateCmdRegistryFile(t, regDir, cmdTestTemplate, "hello.txt", "v2\n")
		runInProject(t, dst)
		cmd := newCheckCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		if exitCodeOf(err) != 2 {
			t.Fatalf("check err = %v (exit %d), want exit 2", err, exitCodeOf(err))
		}
		if got := stdout.String(); !strings.Contains(got, "update available") {
			t.Errorf("stdout = %q, want update-available line", got)
		}
	})
	t.Run("exit 1 without state", func(t *testing.T) {
		runInProject(t, t.TempDir())
		cmd := newCheckCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		if exitCodeOf(err) != 1 {
			t.Errorf("check err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
	t.Run("exit 1 with bad version", func(t *testing.T) {
		_, dst, _ := initTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newCheckCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--ref", "not-a-version"})
		err := cmd.Execute()
		if exitCodeOf(err) != 1 {
			t.Errorf("check err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
}

func TestExitCodeMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil is success", err: nil, want: 0},
		{name: "drift is scriptable", err: &exitError{code: exitDriftOrAvailable, msg: "x"}, want: 2},
		{name: "runtime error is 1", err: errors.New("boom"), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCodeOf(tt.err); got != tt.want {
				t.Errorf("exitCodeOf(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestCheckOffersCmd(t *testing.T) {
	t.Run("ref flag registers", func(t *testing.T) {
		cmd := newCheckCmd()
		if cmd.Flags().Lookup("ref") == nil {
			t.Error("flag --ref missing on check")
		}
	})
}
