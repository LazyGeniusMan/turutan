// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/registry"
)

func isolatedAddConfig(t *testing.T) string {
	t.Helper()
	resetRootFlags(t)
	path := filepath.Join(t.TempDir(), config.ConfigFileName)
	appCfg.cfgFile = path
	t.Chdir(t.TempDir())
	return path
}

func execRegistryAdd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRegistryAddCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestRegistryAddCmd(t *testing.T) {
	t.Run("stores named registry in user config", func(t *testing.T) {
		path := isolatedAddConfig(t)
		out, err := execRegistryAdd(t, "acme", "oci://ghcr.io/acme/templates")
		require.NoError(t, err)
		require.Contains(t, out, `"acme"`)
		got, err := config.LoadRegistries(path)
		require.NoError(t, err)
		require.Equal(t, "oci://ghcr.io/acme/templates", got["acme"])
		parsed, err := registry.ResolveRegistryURI(got, "acme")
		require.NoError(t, err)
		require.Equal(t, registry.SchemeOCI, parsed.Scheme)
	})

	t.Run("supports every uri scheme", func(t *testing.T) {
		path := isolatedAddConfig(t)
		cases := map[string]string{
			"local-rel": "templates/local",
			"local-abs": "fs:/srv/registry",
			"web":       "https://example.com/registry.yml",
			"bucket":    "s3://my-bucket/registry.yml",
			"repo":      "git+https://example.com/org/registry.git?ref=v1.0.0",
			"image":     "oci://ghcr.io/acme/templates",
		}
		for name, uri := range cases {
			_, err := execRegistryAdd(t, name, uri)
			require.NoError(t, err, "add %s %s", name, uri)
		}
		got, err := config.LoadRegistries(path)
		require.NoError(t, err)
		require.Len(t, got, len(cases))
	})

	t.Run("turutan resolves without add but needs force to override", func(t *testing.T) {
		path := isolatedAddConfig(t)
		got, err := config.LoadRegistries(path)
		require.NoError(t, err)
		parsed, err := registry.ResolveRegistryURI(got, registry.DefaultRegistryName)
		require.NoError(t, err)
		require.Equal(t, registry.DefaultRegistryURI, parsed.String())
		_, err = execRegistryAdd(t, "turutan", "oci://example.com/acme/custom")
		require.Error(t, err)
		_, err = execRegistryAdd(t, "turutan", "oci://example.com/acme/custom", "--force")
		require.NoError(t, err)
		got, err = config.LoadRegistries(path)
		require.NoError(t, err)
		require.Equal(t, "oci://example.com/acme/custom", got["turutan"])
	})

	t.Run("refuses overwrite without force", func(t *testing.T) {
		isolatedAddConfig(t)
		_, err := execRegistryAdd(t, "acme", "oci://ghcr.io/acme/one")
		require.NoError(t, err)
		_, err = execRegistryAdd(t, "acme", "oci://ghcr.io/acme/two")
		require.Error(t, err)
		out, err := execRegistryAdd(t, "acme", "oci://ghcr.io/acme/one")
		require.NoError(t, err)
		require.Contains(t, out, "already points at")
		_, err = execRegistryAdd(t, "acme", "oci://ghcr.io/acme/two", "--force")
		require.NoError(t, err)
	})

	t.Run("rejects bad names and uris", func(t *testing.T) {
		isolatedAddConfig(t)
		_, err := execRegistryAdd(t, "bad name!", "oci://ghcr.io/acme/templates")
		require.Error(t, err)
		_, err = execRegistryAdd(t, "", "oci://ghcr.io/acme/templates")
		require.Error(t, err)
		_, err = execRegistryAdd(t, "acme", "ftp://example.com/registry")
		require.Error(t, err)
		_, err = execRegistryAdd(t, "acme", "")
		require.Error(t, err)
		_, err = execRegistryAdd(t, "only-name")
		require.Error(t, err)
	})

	t.Run("preserves other user config keys", func(t *testing.T) {
		path := isolatedAddConfig(t)
		require.NoError(t, config.UpsertRegistry(path, "seed", "fs:./x"))
		v := newViper()
		v.SetConfigFile(path)
		require.NoError(t, v.ReadInConfig())
		v.Set(config.TemplateConfigKey, "./from-user")
		require.NoError(t, v.WriteConfig())
		_, err := execRegistryAdd(t, "acme", "oci://ghcr.io/acme/templates")
		require.NoError(t, err)
		cfg, err := config.LoadUserConfigFile(path)
		require.NoError(t, err)
		require.Equal(t, "./from-user", cfg.Template)
		require.Equal(t, "fs:./x", cfg.Registries["seed"])
		require.Equal(t, "oci://ghcr.io/acme/templates", cfg.Registries["acme"])
	})
}
