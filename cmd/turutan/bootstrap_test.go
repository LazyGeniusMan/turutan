// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

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
	oldCfg := appCfg
	appCfg.nonInteractive = true
	defer func() { appCfg = oldCfg }()

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

func TestResolveSourcePrecedence(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		flag string
		env  string
		proj string
		user string
		want string
	}{
		{name: "explicit arg wins over all", arg: "./a", flag: "./b", env: "./c", proj: "./d", user: "./e", want: "./a"},
		{name: "default alias passes through", arg: "default", flag: "./b", want: "default"},
		{name: "flag wins over env and configs", flag: "./b", env: "./c", proj: "./d", user: "./e", want: "./b"},
		{name: "env wins over project and user", env: "./c", proj: "./d", user: "./e", want: "./c"},
		{name: "project state wins over user", proj: "./d", user: "./e", want: "./d"},
		{name: "user config used when above empty", user: "./e", want: "./e"},
		{name: "empty means built-in default", want: ""},
		{name: "whitespace layers ignored", flag: "  ", env: " \t", proj: "", user: "./e", want: "./e"},
		{name: "layered values trimmed", flag: "  ./b  ", want: "./b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			is.Equal(tt.want, resolveSourcePrecedence(tt.arg, tt.flag, tt.env, tt.proj, tt.user))
		})
	}
}

func TestBootstrapDefaultSourceLayers(t *testing.T) {
	oldCfg := appCfg
	appCfg.nonInteractive = true
	defer func() { appCfg = oldCfg }()

	t.Run("template flag selects source without positional arg", func(t *testing.T) {
		is := assert.New(t)
		src := writeTemplate(t, map[string]string{
			".turutan.yml": "min-engine: \">=0.1.0\"\n",
			"go.mod.tmpl":  "module {{.project_name}}\n",
		})
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newBootstrapCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--template", src, "--defaults", dst})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("bootstrap error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		is.Equal("module proj\n", string(got))
	})
	t.Run("turutan template env selects source without positional arg", func(t *testing.T) {
		is := assert.New(t)
		src := writeTemplate(t, map[string]string{
			".turutan.yml": "min-engine: \">=0.1.0\"\n",
			"go.mod.tmpl":  "module {{.project_name}}\n",
		})
		t.Setenv(config.EnvTemplateOverride, src)
		dst := t.TempDir()
		t.Chdir(dst)
		cmd := newBootstrapCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--defaults"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("bootstrap error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		base := filepath.Base(dst)
		is.Equal("module "+base+"\n", string(got))
	})
}
