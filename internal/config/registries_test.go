// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpsertAndLoadRegistries(t *testing.T) {
	t.Run("round trips entries", func(t *testing.T) {
		isolateHomeAndWorkdir(t)
		path := filepath.Join(t.TempDir(), ConfigFileName)
		require.NoError(t, UpsertRegistry(path, "acme", "oci://ghcr.io/acme/templates"))
		require.NoError(t, UpsertRegistry(path, "local", "templates/local"))
		got, err := LoadRegistries(path)
		require.NoError(t, err)
		require.Equal(t, map[string]string{
			"acme":  "oci://ghcr.io/acme/templates",
			"local": "templates/local",
		}, got)
	})

	t.Run("missing file loads empty", func(t *testing.T) {
		isolateHomeAndWorkdir(t)
		got, err := LoadRegistries(filepath.Join(t.TempDir(), "missing.yml"))
		require.NoError(t, err)
		require.Empty(t, got)
		cfg, err := LoadUserConfigFile(filepath.Join(t.TempDir(), "missing.yml"))
		require.NoError(t, err)
		require.Empty(t, cfg.Registries)
	})

	t.Run("preserves other keys", func(t *testing.T) {
		isolateHomeAndWorkdir(t)
		path := writeConfigFile(t, t.TempDir(), ConfigFileName, "template: ./from-user\n")
		require.NoError(t, UpsertRegistry(path, "acme", "oci://ghcr.io/acme/templates"))
		cfg, err := LoadUserConfigFile(path)
		require.NoError(t, err)
		require.Equal(t, "./from-user", cfg.Template)
		require.Equal(t, "oci://ghcr.io/acme/templates", cfg.Registries["acme"])
	})

	t.Run("rejects empty path and bad yaml", func(t *testing.T) {
		require.Error(t, UpsertRegistry("", "acme", "oci://x"))
		isolateHomeAndWorkdir(t)
		bad := writeConfigFile(t, t.TempDir(), ConfigFileName, ": bad: [\n")
		_, err := LoadRegistries(bad)
		require.Error(t, err)
		require.Error(t, UpsertRegistry(bad, "acme", "oci://x"))
	})

	t.Run("default path is writable", func(t *testing.T) {
		isolateHomeAndWorkdir(t)
		path := DefaultUserConfigPath()
		require.NotEmpty(t, path)
		require.Equal(t, ConfigFileName, filepath.Base(path))
		require.NoError(t, UpsertRegistry(path, "acme", "oci://ghcr.io/acme/templates"))
		data, err := os.ReadFile(path) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		require.NoError(t, err)
		require.Contains(t, string(data), "acme")
	})
}
