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

func initRegistryAt(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
	cmd := newRegistryInitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--author", "Jane Doe <jane@example.com>"})
	require.NoError(t, cmd.Execute())
}

func execRegistryCreate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRegistryCreateCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestRegistryCreateCmd(t *testing.T) {
	t.Run("scaffolds starter and indexes it", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		out, err := execRegistryCreate(t, "hello")
		require.NoError(t, err)
		require.Contains(t, out, `"hello"`)
		for _, rel := range []string{
			"templates/hello/.turutan.yml",
			"templates/hello/README.md",
			"templates/hello/hello.txt.tmpl",
		} {
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				t.Errorf("starter is missing %s: %v", rel, err)
			}
		}
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		found, err := loaded.FindTemplate("hello")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", found.Version)
		require.Equal(t, "Jane Doe <jane@example.com>", found.Author.String())
		require.Equal(t, "templates/hello", found.URI.String())
	})

	t.Run("finds registry root from subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		_, err := execRegistryCreate(t, "first")
		require.NoError(t, err)
		t.Chdir(filepath.Join(dir, "templates", "first"))
		_, err = execRegistryCreate(t, "second")
		require.NoError(t, err)
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Len(t, loaded.Templates, 2)
	})

	t.Run("refuses existing without force", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		_, err := execRegistryCreate(t, "hello")
		require.NoError(t, err)
		_, err = execRegistryCreate(t, "hello")
		require.Error(t, err)
		_, err = execRegistryCreate(t, "hello", "--force")
		require.NoError(t, err)
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Len(t, loaded.Templates, 1)
	})

	t.Run("fails outside a registry", func(t *testing.T) {
		t.Chdir(t.TempDir())
		_, err := execRegistryCreate(t, "hello")
		require.Error(t, err)
	})

	t.Run("rejects bad names and arg counts", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		_, err := execRegistryCreate(t, "Bad Name!")
		require.Error(t, err)
		_, err = execRegistryCreate(t)
		require.Error(t, err)
		_, err = execRegistryCreate(t, "a", "b")
		require.Error(t, err)
	})
}
