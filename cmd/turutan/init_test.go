// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func TestInitCmd(t *testing.T) {
	oldCfg := appCfg
	appCfg.nonInteractive = true
	defer func() { appCfg = oldCfg }()

	t.Run("inits from registry with defaults", func(t *testing.T) {
		_, _, flag := makeCmdRegistry(t, "", map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
		})
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newInitCmd()
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--template", flag, "--defaults", dst})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("init error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dst, "go.mod")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "module proj\n" {
			t.Errorf("go.mod = %q, want defaulted module", got)
		}
	})
	t.Run("invalid conflict mode fails", func(t *testing.T) {
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{filepath.Join(t.TempDir(), "whatever"), "--conflict", "merge"})
		if err := cmd.Execute(); err == nil {
			t.Error("init with bad --conflict succeeded, want error")
		}
	})
	t.Run("missing data fail non-interactively", func(t *testing.T) {
		_, _, flag := makeCmdRegistry(t, "", map[string]string{
			"go.mod.tmpl": "module {{.project_name}}\n",
		})
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--template", flag, filepath.Join(t.TempDir(), "p")})
		if err := cmd.Execute(); err == nil {
			t.Error("init without --defaults/--data-file succeeded, want error")
		}
	})
	t.Run("positional source rejected", func(t *testing.T) {
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"./a", "./b", "--defaults"})
		if err := cmd.Execute(); err == nil {
			t.Error("init with two positionals succeeded, want usage error")
		}
	})
}

func TestResolveTemplateFlag(t *testing.T) {
	const (
		uriA = "oci://example.com/org/a"
		uriB = "oci://example.com/org/b"
		uriC = "oci://example.com/org/c"
	)
	tests := []struct {
		name    string
		flag    string
		env     string
		user    string
		want    scaffold.RegistryRef
		wantErr string
	}{
		{
			name: "explicit flag wins over all",
			flag: uriA + "/t@1.0.0", env: uriB + "/u@1.0.0", user: uriC + "/v@1.0.0",
			want: scaffold.RegistryRef{Registry: uriA, Template: "t", Version: "1.0.0"},
		},
		{
			name: "flag without version tracks latest",
			flag: uriA + "/t",
			want: scaffold.RegistryRef{Registry: uriA, Template: "t"},
		},
		{
			name: "env wins over user",
			env:  uriB + "/u@2.0.0",
			user: uriC + "/v@1.0.0",
			want: scaffold.RegistryRef{Registry: uriB, Template: "u", Version: "2.0.0"},
		},
		{
			name: "user config used when above empty",
			user: uriC + "/v@1.0.0",
			want: scaffold.RegistryRef{Registry: uriC, Template: "v", Version: "1.0.0"},
		},
		{
			name: "empty means default URI",
			want: scaffold.RegistryRef{Registry: "oci://ghcr.io/lazygeniusman/turutan/templates", Template: "default"},
		},
		{
			name: "whitespace layers ignored",
			flag: "  ", env: " \t", user: uriC + "/v",
			want: scaffold.RegistryRef{Registry: uriC, Template: "v"},
		},
		{
			name: "bare name resolves to default registry",
			flag: "mytemplate",
			want: scaffold.RegistryRef{Registry: "oci://ghcr.io/lazygeniusman/turutan/templates", Template: "mytemplate"},
		},
		{
			name:    "name pair without scheme errors",
			flag:    "a/t@1.0.0",
			wantErr: "want <registry-uri>/<template>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			got, err := scaffold.ResolveTemplateFlag(tt.flag, tt.env, tt.user)
			if tt.wantErr != "" {
				is.Error(err)
				if err != nil {
					is.Contains(err.Error(), tt.wantErr)
				}
				return
			}
			is.NoError(err)
			is.Equal(tt.want, got)
		})
	}
}

func TestInitTemplateLayers(t *testing.T) {
	oldCfg := appCfg
	appCfg.nonInteractive = true
	defer func() { appCfg = oldCfg }()

	t.Run("explicit version pins the release", func(t *testing.T) {
		is := assert.New(t)
		regDir, _, flag := makeCmdRegistry(t, "", map[string]string{"hello.txt": "v1\n"})
		updateCmdRegistryFile(t, regDir, cmdTestTemplate, "hello.txt", "v2\n")
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--template", flag + "@" + cmdTestVersion, "--defaults", dst})
		require.NoError(t, cmd.Execute())
		got, err := os.ReadFile(filepath.Join(dst, "hello.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		require.NoError(t, err)
		is.Equal("v1\n", string(got))
	})
	t.Run("latest fallback renders newest", func(t *testing.T) {
		is := assert.New(t)
		regDir, _, flag := makeCmdRegistry(t, "", map[string]string{"hello.txt": "v1\n"})
		updateCmdRegistryFile(t, regDir, cmdTestTemplate, "hello.txt", "v2\n")
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--template", flag, "--defaults", dst})
		require.NoError(t, cmd.Execute())
		got, err := os.ReadFile(filepath.Join(dst, "hello.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		require.NoError(t, err)
		is.Equal("v2\n", string(got))
	})
	t.Run("user config template used when flag empty", func(t *testing.T) {
		is := assert.New(t)
		_, _, flag := makeCmdRegistry(t, "", map[string]string{"hello.txt": "v1\n"})
		t.Setenv(config.EnvTemplateOverride, "")
		v := viper.GetViper()
		v.Set(config.TemplateConfigKey, flag)
		t.Cleanup(func() { v.Set(config.TemplateConfigKey, "") })
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--defaults", dst})
		require.NoError(t, cmd.Execute())
		got, err := os.ReadFile(filepath.Join(dst, "hello.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		require.NoError(t, err)
		is.Equal("v1\n", string(got))
	})
	t.Run("turutan template env selects without flag", func(t *testing.T) {
		is := assert.New(t)
		_, _, flag := makeCmdRegistry(t, "", map[string]string{"hello.txt": "v1\n"})
		t.Setenv(config.EnvTemplateOverride, flag)
		dst := filepath.Join(t.TempDir(), "proj")
		cmd := newInitCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--defaults", dst})
		require.NoError(t, cmd.Execute())
		got, err := os.ReadFile(filepath.Join(dst, "hello.txt")) // #nosec G304 -- test-only path under t.TempDir() with a constant segment
		require.NoError(t, err)
		is.Equal("v1\n", string(got))
	})
}
