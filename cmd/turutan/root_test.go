// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func resetRootFlags(t *testing.T) {
	t.Helper()
	oldCfg := appCfg
	appCfg = appConfig{}
	t.Cleanup(func() {
		appCfg = oldCfg
	})
}

func isolateConfigCandidates(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.EnvXDGConfigHome, filepath.Join(t.TempDir(), "xdg-missing"))
	t.Chdir(t.TempDir())
	if err := os.Unsetenv(config.EnvConfigOverride); err != nil {
		t.Fatal(err)
	}
}

func writeIsolatedConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.ConfigFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeProjectState(t *testing.T, dir, template string) {
	t.Helper()
	state := fmt.Sprintf(`{"version":1,"template":%q,"sourceKind":"filesystem","resolvedCommit":%q,"engine":"0.1.0","templateLicense":"MIT-0"}`,
		template, strings.Repeat("ab", 32))
	if err := os.WriteFile(filepath.Join(dir, config.StateFileName), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestVersionOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "version subcommand", args: []string{"version"}},
		{name: "version flag", args: []string{"--version"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRootFlags(t)
			isolateConfigCandidates(t)
			cmd := newRootCmd(viper.New())
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute %v error: %v", tt.args, err)
			}
			got := stdout.String()
			for _, want := range []string{
				"turutan " + version,
				"commit " + commit,
				"built " + date,
				"Apache-2.0",
				"MIT-0",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestVersionByteIdentity(t *testing.T) {
	resetRootFlags(t)
	isolateConfigCandidates(t)
	run := func(args ...string) string {
		t.Helper()
		appCfg = appConfig{}
		cmd := newRootCmd(viper.New())
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute %v error: %v", args, err)
		}
		return stdout.String()
	}
	sub := run("version")
	flag := run("--version")
	if sub != flag {
		t.Errorf("version outputs differ:\nsubcommand %q\nflag %q", sub, flag)
	}
	for _, want := range []string{"Apache-2.0", "MIT-0"} {
		if !strings.Contains(sub, want) {
			t.Errorf("version output missing dual-license %q:\n%s", want, sub)
		}
	}
}

func TestNewViperEnvLayering(t *testing.T) {
	t.Run("turutan template env visible", func(t *testing.T) {
		t.Setenv(config.EnvTemplateOverride, "./from-env")
		v := newViper()
		if got := v.GetString(config.TemplateConfigKey); got != "./from-env" {
			t.Errorf("template = %q, want %q", got, "./from-env")
		}
	})
	t.Run("unset env reads empty", func(t *testing.T) {
		isolateConfigCandidates(t)
		t.Setenv(config.EnvTemplateOverride, "")
		v := newViper()
		if got := v.GetString(config.TemplateConfigKey); got != "" {
			t.Errorf("template = %q, want empty", got)
		}
	})
}

func TestInitConfigWithViper(t *testing.T) {
	tests := []struct {
		name         string
		explicitBody string
		envBody      string
		wantTemplate string
		wantErr      bool
	}{
		{name: "explicit file loads template key", explicitBody: "template: ./from-file\n", wantTemplate: "./from-file"},
		{name: "explicit missing file errors", explicitBody: "MISSING", wantErr: true},
		{name: "turutan config env beats explicit", explicitBody: "template: ./from-flag\n", envBody: "template: ./from-env\n", wantTemplate: "./from-env"},
		{name: "malformed file errors", explicitBody: "template: [unclosed\n", wantErr: true},
		{name: "no candidate is not fatal", explicitBody: "NONE", wantTemplate: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRootFlags(t)
			isolateConfigCandidates(t)
			v := viper.New()
			var cfgPath string
			switch tt.explicitBody {
			case "MISSING":
				cfgPath = filepath.Join(t.TempDir(), "missing.yml")
			case "NONE":
				cfgPath = ""
			default:
				cfgPath = writeIsolatedConfig(t, tt.explicitBody)
			}
			if tt.envBody != "" {
				t.Setenv(config.EnvConfigOverride, writeIsolatedConfig(t, tt.envBody))
			}
			err := initConfigWithViper(v, cfgPath)
			if tt.wantErr {
				if err == nil {
					t.Error("initConfigWithViper succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("initConfigWithViper error: %v", err)
			}
			if got := v.GetString(config.TemplateConfigKey); got != tt.wantTemplate {
				t.Errorf("template = %q, want %q", got, tt.wantTemplate)
			}
		})
	}
}

func TestViperBindsCobraFlags(t *testing.T) {
	findSub := func(root *cobra.Command, name string) *cobra.Command {
		t.Helper()
		for _, sub := range root.Commands() {
			if sub.Name() == name {
				return sub
			}
		}
		t.Fatalf("subcommand %q not found", name)
		return nil
	}
	t.Run("changed bootstrap template flag visible in viper", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		v := viper.New()
		root := newRootCmd(v)
		if err := findSub(root, "bootstrap").Flags().Set("template", "./from-flag"); err != nil {
			t.Fatal(err)
		}
		if got := v.GetString(config.TemplateConfigKey); got != "./from-flag" {
			t.Errorf("template = %q, want %q", got, "./from-flag")
		}
	})
	t.Run("changed persistent verbose flag visible in viper", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		v := viper.New()
		root := newRootCmd(v)
		if err := root.PersistentFlags().Set("verbose", "true"); err != nil {
			t.Fatal(err)
		}
		if got := v.GetBool("verbose"); !got {
			t.Errorf("verbose = %v, want true", got)
		}
	})
	t.Run("flag beats env and file", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		t.Setenv(config.EnvTemplateOverride, "./from-env")
		cfgPath := writeIsolatedConfig(t, "template: ./from-file\n")
		v := viper.New()
		root := newRootCmd(v)
		if err := findSub(root, "bootstrap").Flags().Set("template", "./from-flag"); err != nil {
			t.Fatal(err)
		}
		if err := initConfigWithViper(v, cfgPath); err != nil {
			t.Fatal(err)
		}
		if got := v.GetString(config.TemplateConfigKey); got != "./from-flag" {
			t.Errorf("template = %q, want %q", got, "./from-flag")
		}
	})
	t.Run("env beats file when flag unchanged", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		t.Setenv(config.EnvTemplateOverride, "./from-env")
		cfgPath := writeIsolatedConfig(t, "template: ./from-file\n")
		v := viper.New()
		_ = newRootCmd(v)
		if err := initConfigWithViper(v, cfgPath); err != nil {
			t.Fatal(err)
		}
		if got := v.GetString(config.TemplateConfigKey); got != "./from-env" {
			t.Errorf("template = %q, want %q", got, "./from-env")
		}
	})
	t.Run("repeated execute does not panic on rebind", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		v := viper.New()
		root := newRootCmd(v)
		for range 2 {
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs([]string{"--version"})
			if err := root.Execute(); err != nil {
				t.Fatalf("execute error: %v", err)
			}
		}
	})
}

func TestResolveBootstrapSourceWithViper(t *testing.T) {
	tests := []struct {
		name        string
		arg         string
		flag        string
		env         string
		user        string
		withProject bool
		want        string
	}{
		{name: "explicit arg wins over all", arg: "./a", flag: "./b", env: "./c", user: "./e", withProject: true, want: "./a"},
		{name: "flag wins over env project user", flag: "./b", env: "./c", user: "./e", withProject: true, want: "./b"},
		{name: "env wins over project and user", env: "./c", user: "./e", withProject: true, want: "./c"},
		{name: "project wins over user", user: "./e", withProject: true, want: "./from-project"},
		{name: "user config from isolated viper", user: "./e", want: "./e"},
		{name: "empty means built-in default", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.EnvTemplateOverride, tt.env)
			v := viper.New()
			if tt.user != "" {
				v.Set(config.TemplateConfigKey, tt.user)
			}
			dir := t.TempDir()
			if tt.withProject {
				writeProjectState(t, dir, "./from-project")
			}
			if got := resolveBootstrapSourceWithViper(v, tt.arg, tt.flag, dir); got != tt.want {
				t.Errorf("source = %q, want %q", got, tt.want)
			}
		})
	}
}

func verboseTemplate(t *testing.T) string {
	t.Helper()
	return writeTemplate(t, map[string]string{
		".turutan.yml": "min-engine: \">=0.1.0\"\n",
		"go.mod.tmpl":  "module {{.project_name}}\n",
	})
}

func TestVerboseBootstrapLogs(t *testing.T) {
	oldCfg := appCfg
	appCfg.nonInteractive, appCfg.verbose = true, true
	defer func() { appCfg = oldCfg }()

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
	oldCfg := appCfg
	appCfg.nonInteractive, appCfg.verbose = true, false
	defer func() { appCfg = oldCfg }()

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
	oldCfg := appCfg
	appCfg.nonInteractive, appCfg.verbose = true, true
	defer func() { appCfg = oldCfg }()

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
	oldCfg := appCfg
	appCfg.nonInteractive, appCfg.noColor, appCfg.verbose = true, true, false
	defer func() { appCfg = oldCfg }()

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
