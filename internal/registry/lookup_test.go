// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRegistryURI(t *testing.T) {
	t.Run("default resolves without an entry", func(t *testing.T) {
		got, err := ResolveRegistryURI(map[string]string{}, DefaultRegistryName)
		require.NoError(t, err)
		require.Equal(t, DefaultRegistryURI, got.String())
	})

	t.Run("explicit entry wins over default", func(t *testing.T) {
		got, err := ResolveRegistryURI(
			map[string]string{DefaultRegistryName: "oci://example.com/acme/custom"}, DefaultRegistryName)
		require.NoError(t, err)
		require.Equal(t, "oci://example.com/acme/custom", got.String())
	})

	t.Run("named entry resolves", func(t *testing.T) {
		got, err := ResolveRegistryURI(map[string]string{"acme": "fs:./templates"}, "acme")
		require.NoError(t, err)
		require.Equal(t, SchemeFS, got.Scheme)
	})

	t.Run("unknown name errors", func(t *testing.T) {
		_, err := ResolveRegistryURI(map[string]string{}, "acme")
		require.Error(t, err)
		_, err = ResolveRegistryURI(map[string]string{}, "")
		require.Error(t, err)
		_, err = ResolveRegistryURI(map[string]string{}, "  ")
		require.Error(t, err)
	})

	t.Run("bad stored uri errors", func(t *testing.T) {
		_, err := ResolveRegistryURI(map[string]string{"acme": "ftp://example.com/x"}, "acme")
		require.Error(t, err)
		_, err = ResolveRegistryURI(map[string]string{"acme": ""}, "acme")
		require.Error(t, err)
		_, err = ResolveRegistryURI(map[string]string{"acme": "  "}, "acme")
		require.Error(t, err)
	})
}
