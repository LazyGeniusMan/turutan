// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/LazyGeniusMan/turutan/internal/registry"
)

func execRegistryInit(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRegistryInitCmd()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestRegistryInitCmd(t *testing.T) {
	t.Run("creates index with flags", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		out, _, err := execRegistryInit(t,
			"--name", "acme",
			"--author", "Jane Doe <jane@example.com> (https://example.com)",
			"--version", "1.0.0")
		require.NoError(t, err)
		require.Contains(t, out, `"acme"`)
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, "acme", loaded.Name)
		require.Equal(t, "1.0.0", loaded.Version)
		require.Equal(t, "Jane Doe <jane@example.com> (https://example.com)", loaded.Author.String())
		require.Empty(t, loaded.Templates)
	})

	t.Run("defaults name to directory basename", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		_, _, err := execRegistryInit(t, "--author", "Jane Doe")
		require.NoError(t, err)
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, filepath.Base(dir), loaded.Name)
	})

	t.Run("refuses existing without force", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		_, _, err := execRegistryInit(t, "--author", "Jane Doe")
		require.NoError(t, err)
		_, _, err = execRegistryInit(t, "--author", "John Doe")
		require.Error(t, err)
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, "Jane Doe", loaded.Author.String())
	})

	t.Run("force overwrites existing", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		_, _, err := execRegistryInit(t, "--author", "Jane Doe")
		require.NoError(t, err)
		_, _, err = execRegistryInit(t, "--author", "John Doe", "--force")
		require.NoError(t, err)
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, "John Doe", loaded.Author.String())
	})

	t.Run("writes json index", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		_, _, err := execRegistryInit(t, "--author", "Jane Doe")
		require.NoError(t, err)
		raw, err := os.ReadFile(filepath.Join(dir, registry.IndexFileName)) // #nosec G304 -- dir is t.TempDir() plus the fixed index file name
		require.NoError(t, err)
		require.Contains(t, string(raw), `"name"`)
		require.Equal(t, "turutan.json", registry.IndexFileName)
	})

	t.Run("requires author", func(t *testing.T) {
		t.Chdir(t.TempDir())
		_, _, err := execRegistryInit(t)
		require.Error(t, err)
	})

	t.Run("rejects bad author and version", func(t *testing.T) {
		t.Chdir(t.TempDir())
		_, _, err := execRegistryInit(t, "--author", "Jane <not-an-email>")
		require.Error(t, err)
		_, _, err = execRegistryInit(t, "--author", "Jane Doe", "--version", "not-semver")
		require.Error(t, err)
		_, _, err = execRegistryInit(t, "--name", "Bad Name!", "--author", "Jane Doe")
		require.Error(t, err)
	})
}
