// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeConfigFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func isolateHomeAndWorkdir(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	t.Setenv(EnvXDGConfigHome, filepath.Join(t.TempDir(), "xdg-nonexistent"))
	if err := os.Unsetenv(EnvConfigOverride); err != nil {
		t.Fatal(err)
	}
}

func TestResolveConfigPath(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (explicit, want string)
	}{
		{
			name: "turutan config beats explicit flag",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				envFile := writeConfigFile(t, t.TempDir(), ConfigFileName, "template: x\n")
				explicit := filepath.Join(t.TempDir(), ConfigFileName)
				t.Setenv(EnvConfigOverride, envFile)
				return explicit, envFile
			},
		},
		{
			name: "turutan config dir joins file name",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				dir := t.TempDir()
				t.Setenv(EnvConfigOverride, dir)
				return "", filepath.Join(dir, ConfigFileName)
			},
		},
		{
			name: "turutan config dir uses json when yml absent",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				dir := t.TempDir()
				want := writeConfigFile(t, dir, ConfigFileNameJSON, "template: x\n")
				t.Setenv(EnvConfigOverride, dir)
				return "", want
			},
		},
		{
			name: "turutan config dir prefers yml when both exist",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				dir := t.TempDir()
				want := writeConfigFile(t, dir, ConfigFileName, "template: x\n")
				writeConfigFile(t, dir, ConfigFileNameJSON, "template: y\n")
				t.Setenv(EnvConfigOverride, dir)
				return "", want
			},
		},
		{
			name: "empty turutan config falls through to explicit",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				explicit := filepath.Join(t.TempDir(), ConfigFileName)
				t.Setenv(EnvConfigOverride, "")
				return explicit, explicit
			},
		},
		{
			name: "explicit used when env unset",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				explicit := filepath.Join(t.TempDir(), ConfigFileName)
				return explicit, explicit
			},
		},
		{
			name: "xdg absolute candidate used",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				xdg := t.TempDir()
				want := writeConfigFile(t, filepath.Join(xdg, ToolName), ConfigFileName, "template: x\n")
				t.Setenv(EnvXDGConfigHome, xdg)
				return "", want
			},
		},
		{
			name: "xdg relative ignored",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				t.Setenv(EnvXDGConfigHome, filepath.Join("relative", "xdg"))
				return "", ""
			},
		},
		{
			name: "project config fallback",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				if err := os.Unsetenv(EnvXDGConfigHome); err != nil {
					t.Fatal(err)
				}
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				return "", writeConfigFile(t, filepath.Join(cwd, ".config"), ConfigFileName, "template: x\n")
			},
		},
		{
			name: "no candidate returns empty",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				if err := os.Unsetenv(EnvXDGConfigHome); err != nil {
					t.Fatal(err)
				}
				return "", ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			explicit, want := tt.setup(t)
			is.Equal(want, ResolveConfigPath(explicit))
		})
	}
}

func TestResolveConfigPathJSONDiscovery(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (explicit, want string)
	}{
		{
			name: "json candidate used when yml absent",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				xdg := t.TempDir()
				want := writeConfigFile(t, filepath.Join(xdg, ToolName), ConfigFileNameJSON, "template: x\n")
				t.Setenv(EnvXDGConfigHome, xdg)
				return "", want
			},
		},
		{
			name: "yml wins over json within layer",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				xdg := t.TempDir()
				dir := filepath.Join(xdg, ToolName)
				want := writeConfigFile(t, dir, ConfigFileName, "template: x\n")
				writeConfigFile(t, dir, ConfigFileNameJSON, "template: y\n")
				t.Setenv(EnvXDGConfigHome, xdg)
				return "", want
			},
		},
		{
			name: "earlier layer json beats later layer yml",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				if err := os.Unsetenv(EnvXDGConfigHome); err != nil {
					t.Fatal(err)
				}
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatal(err)
				}
				want := writeConfigFile(t, filepath.Join(home, ".config", ToolName), ConfigFileNameJSON, "template: x\n")
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				writeConfigFile(t, filepath.Join(cwd, ".config"), ConfigFileName, "template: y\n")
				return "", want
			},
		},
		{
			name: "project json fallback",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				if err := os.Unsetenv(EnvXDGConfigHome); err != nil {
					t.Fatal(err)
				}
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				return "", writeConfigFile(t, filepath.Join(cwd, ".config"), ConfigFileNameJSON, "template: x\n")
			},
		},
		{
			name: "project yml wins over project json",
			setup: func(t *testing.T) (string, string) {
				isolateHomeAndWorkdir(t)
				if err := os.Unsetenv(EnvXDGConfigHome); err != nil {
					t.Fatal(err)
				}
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				dir := filepath.Join(cwd, ".config")
				want := writeConfigFile(t, dir, ConfigFileName, "template: x\n")
				writeConfigFile(t, dir, ConfigFileNameJSON, "template: y\n")
				return "", want
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			explicit, want := tt.setup(t)
			is.Equal(want, ResolveConfigPath(explicit))
		})
	}
}
