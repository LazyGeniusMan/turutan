// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

const profilesCmdManifest = "inputs:\n" +
	"  project_name:\n    type: string\n" +
	"profiles:\n" +
	"  go:\n    includes: [\"go.mod\"]\n" +
	"  github:\n    includes: [\".github/**\"]\n"

func bootstrapProfilesProject(t *testing.T, profiles ...string) string {
	t.Helper()
	src := writeTemplate(t, map[string]string{
		".turutan.yml": profilesCmdManifest,
		"keep.txt":     "keep\n",
		"go.mod":       "module example\n",
	})
	var list strings.Builder
	list.WriteString("project_name: proj\n")
	dataFile := filepath.Join(t.TempDir(), "data.yml")
	if err := os.WriteFile(dataFile, []byte(list.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "proj")
	opts := scaffold.Options{
		NonInteractive: true,
		Defaults:       true,
		DataFile:       dataFile,
		Profiles:       append([]string{}, profiles...),
		Engine:         "0.1.0",
		Stdout:         &bytes.Buffer{},
		Stderr:         &bytes.Buffer{},
		Stdin:          bytes.NewReader(nil),
	}
	if err := scaffold.Bootstrap(context.Background(), src, dst, opts); err != nil {
		t.Fatalf("Bootstrap error: %v", err)
	}
	return dst
}

func TestProfilesCmd(t *testing.T) {
	t.Run("lists offered versus enabled with exit 0", func(t *testing.T) {
		dst := bootstrapProfilesProject(t, "core", "go")
		runInProject(t, dst)
		cmd := newProfilesCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("profiles error: %v (exit %d)", err, exitCodeOf(err))
		}
		got := stdout.String()
		for _, want := range []string{
			"offered profiles: core, custom, github, go",
			"enabled profiles: core, custom, go",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("stdout = %q, want it to contain %q", got, want)
			}
		}
	})
	t.Run("exit 1 without state", func(t *testing.T) {
		runInProject(t, t.TempDir())
		cmd := newProfilesCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{})
		if err := cmd.Execute(); exitCodeOf(err) != 1 {
			t.Errorf("profiles err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
}

func TestDiffWhatFlag(t *testing.T) {
	t.Run("what flag registers with all default", func(t *testing.T) {
		cmd := newDiffCmd()
		flag := cmd.Flags().Lookup("what")
		if flag == nil {
			t.Fatal("flag --what missing")
		}
		if flag.DefValue != "all" {
			t.Errorf("--what default = %q, want all", flag.DefValue)
		}
	})
	t.Run("unknown what fails", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		runInProject(t, dst)
		cmd := newDiffCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--what", "bogus"})
		if err := cmd.Execute(); exitCodeOf(err) != 1 {
			t.Errorf("diff err = %v (exit %d), want exit 1", err, exitCodeOf(err))
		}
	})
	t.Run("upstream hides local-only drift", func(t *testing.T) {
		_, dst := bootstrapTestProject(t, map[string]string{"hello.txt": "v1\n"})
		if err := os.WriteFile(filepath.Join(dst, "hello.txt"), []byte("local\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runInProject(t, dst)
		cmd := newDiffCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--what", "upstream"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("diff error: %v (exit %d)", err, exitCodeOf(err))
		}
		if got := stdout.String(); !strings.Contains(got, "no drift") {
			t.Errorf("stdout = %q, want no-drift line", got)
		}
	})
}

func TestUpdateProfileFlags(t *testing.T) {
	t.Run("enable disable preview flags register", func(t *testing.T) {
		cmd := newUpdateCmd()
		for _, name := range []string{"enable", "disable", "preview"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag --%s missing", name)
			}
		}
	})
	t.Run("unknown enable profile fails", func(t *testing.T) {
		dst := bootstrapProfilesProject(t, "core")
		runInProject(t, dst)
		cmd := newUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--enable", "nope", "--force"})
		if err := cmd.Execute(); err == nil {
			t.Error("update with unknown --enable succeeded, want error")
		} else if !strings.Contains(err.Error(), `unknown profile "nope"`) {
			t.Errorf("error = %q, want it to name the profile", err)
		}
	})
	t.Run("preview leaves the tree alone", func(t *testing.T) {
		dst := bootstrapProfilesProject(t, "core")
		runInProject(t, dst)
		cmd := newUpdateCmd()
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--enable", "go", "--preview"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}
		if got := stdout.String(); !strings.Contains(got, "preview go") {
			t.Errorf("stdout = %q, want the per-profile plan", got)
		}
		if _, err := os.Stat(filepath.Join(dst, "go.mod")); !os.IsNotExist(err) {
			t.Error("go.mod exists, want preview to write nothing")
		}
	})
}

func TestUpdateDisableCmd(t *testing.T) {
	t.Run("unknown disable fails with offered list", func(t *testing.T) {
		dst := bootstrapProfilesProject(t, "core")
		runInProject(t, dst)
		cmd := newUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--disable", "nope", "--force"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("update with unknown --disable succeeded, want error")
		}
		if !strings.Contains(err.Error(), `unknown profile "nope"`) {
			t.Errorf("error = %q, want it to name the profile", err)
		}
	})
	t.Run("same id enable and disable keeps it enabled", func(t *testing.T) {
		dst := bootstrapProfilesProject(t, "core", "go")
		runInProject(t, dst)
		cmd := newUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--enable", "go", "--disable", "go", "--force"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dst, "go.mod")); err != nil {
			t.Errorf("go.mod missing after enable-wins, want it kept: %v", err)
		}
	})
	t.Run("whitespace disable is trimmed", func(t *testing.T) {
		dst := bootstrapProfilesProject(t, "core", "go")
		runInProject(t, dst)
		cmd := newUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--disable", "  go  ", "--force"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("update error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dst, "go.mod")); !os.IsNotExist(err) {
			t.Error("go.mod exists, want it removed after trimmed --disable")
		}
	})
	t.Run("disable and preview flags register", func(t *testing.T) {
		cmd := newUpdateCmd()
		for _, name := range []string{"disable", "enable", "preview", "force", "data-file"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("flag --%s missing", name)
			}
		}
	})
}
