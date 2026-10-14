// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

func TestResolveProfilesListPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		flag    []string
		env     string
		project []string
		user    []string
		want    []string
	}{
		{name: "flag wins over all", flag: []string{"go"}, env: "github", project: []string{"docker"}, user: []string{"kubernetes"}, want: []string{"go"}},
		{name: "comma-packed flag splits", flag: []string{"core,go"}, want: []string{"core", "go"}},
		{name: "env wins over project and user", env: "github", project: []string{"docker"}, user: []string{"kubernetes"}, want: []string{"github"}},
		{name: "project wins over user", project: []string{"docker"}, user: []string{"kubernetes"}, want: []string{"docker"}},
		{name: "user used when above empty", user: []string{"kubernetes"}, want: []string{"kubernetes"}},
		{name: "empty means code default", want: []string{"core", "ai"}},
		{name: "whitespace ignored", flag: []string{"  "}, env: " \t", project: nil, user: []string{"go"}, want: []string{"go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveProfilesListPrecedence(tt.flag, tt.env, tt.project, tt.user)
			if !slices.Equal(got, tt.want) {
				t.Errorf("resolve = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveProfilesWithViperLayers(t *testing.T) {
	t.Run("flag beats env project user", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		t.Setenv(config.EnvProfilesOverride, "github")
		v := viper.New()
		v.Set(config.ProfilesConfigKey, []string{"docker"})
		root := newRootCmd(v)
		var bootstrap *cobra.Command
		for _, sub := range root.Commands() {
			if sub.Name() == "bootstrap" {
				bootstrap = sub
				break
			}
		}
		if bootstrap == nil {
			t.Fatal("bootstrap subcommand not found")
		}
		if err := root.PersistentFlags().Set("profiles", "go"); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeProjectState(t, dir, "./from-project")
		statePath := filepath.Join(dir, config.StateFileName)
		_ = os.Remove(statePath)
		raw := `{"version":1,"template":"tpl","sourceKind":"filesystem","resolvedCommit":"` + strings.Repeat("ab", 32) + `","engine":"0.1.0","templateLicense":"MIT-0","profiles":["docker"]}`
		if err := os.MkdirAll(filepath.Dir(statePath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		got := resolveProfilesWithViper(v, bootstrap, dir)
		if !slices.Equal(got, []string{"go"}) {
			t.Errorf("profiles = %v, want [go] (flag wins)", got)
		}
	})
	t.Run("env beats project and user", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		t.Setenv(config.EnvProfilesOverride, "github")
		v := viper.New()
		v.Set(config.ProfilesConfigKey, []string{"docker"})
		root := newRootCmd(v)
		var bootstrap *cobra.Command
		for _, sub := range root.Commands() {
			if sub.Name() == "bootstrap" {
				bootstrap = sub
				break
			}
		}
		dir := t.TempDir()
		raw := `{"version":1,"template":"tpl","sourceKind":"filesystem","resolvedCommit":"` + strings.Repeat("ab", 32) + `","engine":"0.1.0","templateLicense":"MIT-0","profiles":["kubernetes"]}`
		statePath := filepath.Join(dir, config.StateFileName)
		if err := os.MkdirAll(filepath.Dir(statePath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = root
		got := resolveProfilesWithViper(v, bootstrap, dir)
		if !slices.Equal(got, []string{"github"}) {
			t.Errorf("profiles = %v, want [github] (env wins)", got)
		}
	})
	t.Run("project beats user", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		if err := os.Unsetenv(config.EnvProfilesOverride); err != nil {
			t.Fatal(err)
		}
		v := viper.New()
		v.Set(config.ProfilesConfigKey, []string{"docker"})
		root := newRootCmd(v)
		var bootstrap *cobra.Command
		for _, sub := range root.Commands() {
			if sub.Name() == "bootstrap" {
				bootstrap = sub
				break
			}
		}
		dir := t.TempDir()
		raw := `{"version":1,"template":"tpl","sourceKind":"filesystem","resolvedCommit":"` + strings.Repeat("ab", 32) + `","engine":"0.1.0","templateLicense":"MIT-0","profiles":["github"]}`
		statePath := filepath.Join(dir, config.StateFileName)
		if err := os.MkdirAll(filepath.Dir(statePath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = root
		got := resolveProfilesWithViper(v, bootstrap, dir)
		if !slices.Equal(got, []string{"github"}) {
			t.Errorf("profiles = %v, want [github] (project wins)", got)
		}
	})
	t.Run("user used when above empty", func(t *testing.T) {
		resetRootFlags(t)
		isolateConfigCandidates(t)
		if err := os.Unsetenv(config.EnvProfilesOverride); err != nil {
			t.Fatal(err)
		}
		v := viper.New()
		v.Set(config.ProfilesConfigKey, []string{"docker"})
		root := newRootCmd(v)
		var bootstrap *cobra.Command
		for _, sub := range root.Commands() {
			if sub.Name() == "bootstrap" {
				bootstrap = sub
				break
			}
		}
		_ = root
		got := resolveProfilesWithViper(v, bootstrap, t.TempDir())
		if !slices.Equal(got, []string{"docker"}) {
			t.Errorf("profiles = %v, want [docker] (user config)", got)
		}
	})
}

func TestBootstrapProfilesLayersRenderYml(t *testing.T) {
	fixture := filepath.Join("..", "..", "templates", "default")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("default template not found: %v", err)
	}
	ymlKeep := filepath.Join(".config", "ai-rulez", "domains", "yml", ".gitkeep")
	bootstrapWithProfiles := func(t *testing.T, profiles []string) string {
		t.Helper()
		target := filepath.Join(t.TempDir(), "proj")
		var stdout bytes.Buffer
		opts := scaffold.Options{
			NonInteractive: true,
			Defaults:       true,
			Profiles:       profiles,
			Engine:         "0.1.0",
			Stdout:         &stdout,
			Stderr:         &bytes.Buffer{},
			Stdin:          bytes.NewReader(nil),
		}
		if err := scaffold.Bootstrap(context.Background(), fixture, target, opts); err != nil {
			t.Fatalf("Bootstrap error: %v", err)
		}
		return target
	}
	t.Run("flag layer selects yml", func(t *testing.T) {
		flag := []string{"core", "ai", "go", "github", "custom"}
		resolved := resolveProfilesListPrecedence(flag, "", nil, nil)
		target := bootstrapWithProfiles(t, resolved)
		if _, err := os.Stat(filepath.Join(target, ymlKeep)); err != nil {
			t.Errorf("yml .gitkeep missing via --profiles flag %v: %v", flag, err)
		}
	})
	t.Run("env layer selects yml", func(t *testing.T) {
		t.Setenv(config.EnvProfilesOverride, "core,ai,go,github,custom")
		envVal, _ := os.LookupEnv(config.EnvProfilesOverride)
		resolved := resolveProfilesListPrecedence(nil, envVal, nil, nil)
		target := bootstrapWithProfiles(t, resolved)
		if _, err := os.Stat(filepath.Join(target, ymlKeep)); err != nil {
			t.Errorf("yml .gitkeep missing via %s: %v", config.EnvProfilesOverride, err)
		}
	})
	t.Run("config file layer selects yml", func(t *testing.T) {
		user := []string{"core", "ai", "go", "github", "custom"}
		resolved := resolveProfilesListPrecedence(nil, "", nil, user)
		target := bootstrapWithProfiles(t, resolved)
		if _, err := os.Stat(filepath.Join(target, ymlKeep)); err != nil {
			t.Errorf("yml .gitkeep missing via user config %v: %v", user, err)
		}
	})
	t.Run("foo layered include re-includes", func(t *testing.T) {
		manifest, err := config.LoadManifest(os.DirFS(fixture))
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		owners := config.ProfilesForPath(manifest, nil, ".config/ai-rulez/domains/yml/.gitkeep")
		slices.Sort(owners)
		if !slices.Equal(owners, []string{"docker", "github", "kubernetes"}) {
			t.Errorf("yml owners = %v, want [docker github kubernetes]", owners)
		}
	})
}
