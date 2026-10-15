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
	t.Run("scaffolds starter and records it unreleased", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		out, err := execRegistryCreate(t, "hello")
		require.NoError(t, err)
		require.Contains(t, out, `"hello"`)
		for _, rel := range []string{
			"templates/hello/template.yml",
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
		require.False(t, found.IsReleased(), "fresh create must not look released")
		require.Empty(t, found.Version)
		require.Nil(t, found.URI)
		require.Empty(t, found.Releases)
		require.Empty(t, found.Digest)
		require.Equal(t, "Jane Doe <jane@example.com>", found.Author.String())
		raw, err := os.ReadFile(filepath.Join(dir, registry.IndexFileName)) // #nosec G304 -- dir is a test temp dir plus the fixed index file name
		require.NoError(t, err)
		for _, banned := range []string{"releases", "digest", "templates/hello"} {
			require.NotContains(t, string(raw), banned,
				"unreleased entry must not reference a release: %s", banned)
		}
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
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Len(t, loaded.Templates, 1)
	})

	t.Run("force moves the existing dir aside instead of deleting it", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		_, err := execRegistryCreate(t, "hello")
		require.NoError(t, err)
		marker := filepath.Join(dir, "templates", "hello", "author.txt")
		require.NoError(t, os.WriteFile(marker, []byte("author content"), 0o600))
		_, err = execRegistryCreate(t, "hello", "--force")
		require.NoError(t, err)
		content, err := os.ReadFile(filepath.Join(dir, "templates", "hello.bak", "author.txt")) // #nosec G304 -- dir is a test temp dir plus fixed fixture names
		require.NoError(t, err, "force must back the author content up, not delete it")
		require.Equal(t, "author content", string(content))
		require.FileExists(t, filepath.Join(dir, "templates", "hello", "template.yml"))
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		require.Len(t, loaded.Templates, 1)
		found, err := loaded.FindTemplate("hello")
		require.NoError(t, err)
		require.False(t, found.IsReleased())
	})

	t.Run("force refuses when the backup already exists", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		_, err := execRegistryCreate(t, "hello")
		require.NoError(t, err)
		_, err = execRegistryCreate(t, "hello", "--force")
		require.NoError(t, err)
		_, err = execRegistryCreate(t, "hello", "--force")
		require.Error(t, err, "second force must stop instead of burying the first backup")
	})

	t.Run("recreates the entry when only the dir is gone", func(t *testing.T) {
		dir := t.TempDir()
		initRegistryAt(t, dir)
		_, err := execRegistryCreate(t, "hello")
		require.NoError(t, err)
		require.NoError(t, os.RemoveAll(filepath.Join(dir, "templates", "hello")))
		_, err = execRegistryCreate(t, "hello")
		require.Error(t, err, "indexed entry still refuses without force")
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
