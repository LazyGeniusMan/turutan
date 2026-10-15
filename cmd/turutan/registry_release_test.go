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

func execRegistryRelease(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRegistryReleaseCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestRegistryReleaseCmd(t *testing.T) {
	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		t.Chdir(dir)
		_, _, err := execRegistryInit(t, "--author", "Jane Doe <jane@example.com>")
		require.NoError(t, err)
		_, err = execRegistryCreate(t, "foo")
		require.NoError(t, err)
		return dir
	}
	load := func(t *testing.T, dir string) *registry.RegistryIndex {
		t.Helper()
		loaded, err := registry.LoadIndexFile(filepath.Join(dir, registry.IndexFileName))
		require.NoError(t, err)
		return loaded
	}

	t.Run("first release from a created entry starts at 0.1.0 whatever the bump", func(t *testing.T) {
		dir := setup(t)
		out, err := execRegistryRelease(t, "foo", "minor", "fs")
		require.NoError(t, err)
		require.Contains(t, out, "foo@0.1.0")
		manifestPath := filepath.Join(dir, "templates", "foo", "manifest", "0.1.0.json")
		manifest, err := registry.LoadVersionedManifest(dir, "foo", "0.1.0")
		require.NoError(t, err)
		require.Equal(t, "foo", manifest.Name)
		require.Equal(t, "0.1.0", manifest.Version)
		require.Contains(t, out, manifest.Digest)
		require.Equal(t, "fs://templates/foo/releases/0.1.0.tar.gz", manifest.URI.String())
		if _, err := os.Stat(manifestPath); err != nil {
			t.Errorf("versioned manifest missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "templates", "foo", "releases", "0.1.0.tar.gz")); err != nil {
			t.Errorf("release archive missing: %v", err)
		}
		loaded := load(t, dir)
		found, err := loaded.FindTemplate("foo")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", found.Version)
		require.Equal(t, []string{"0.1.0"}, found.Releases)
		require.Equal(t, manifest.Digest, found.Digest)
		out, err = execRegistryRelease(t, "foo", "minor", "fs")
		require.NoError(t, err)
		require.Contains(t, out, "foo@0.2.0")
		found, err = load(t, dir).FindTemplate("foo")
		require.NoError(t, err)
		require.Equal(t, "0.2.0", found.Version)
		require.Equal(t, []string{"0.2.0", "0.1.0"}, found.Releases)
	})

	t.Run("major and patch bumps", func(t *testing.T) {
		dir := setup(t)
		_, err := execRegistryRelease(t, "foo", "patch", "fs")
		require.NoError(t, err, "first cut is 0.1.0 even for patch")
		require.Equal(t, "0.1.0", load(t, dir).Templates[0].Version)
		_, err = execRegistryRelease(t, "foo", "minor", "fs")
		require.NoError(t, err)
		require.Equal(t, "0.2.0", load(t, dir).Templates[0].Version)
		_, err = execRegistryRelease(t, "foo", "major", "fs")
		require.NoError(t, err)
		found, err := load(t, dir).FindTemplate("foo")
		require.NoError(t, err)
		require.Equal(t, "1.0.0", found.Version)
		require.Equal(t, []string{"1.0.0", "0.2.0", "0.1.0"}, found.Releases)
		_ = dir
	})

	t.Run("same bump rebuilds without version change", func(t *testing.T) {
		dir := setup(t)
		_, err := execRegistryRelease(t, "foo", "patch", "fs")
		require.NoError(t, err)
		out, err := execRegistryRelease(t, "foo", "same", "fs", "--force")
		require.NoError(t, err)
		require.Contains(t, out, "foo@0.1.0")
		found, err := load(t, dir).FindTemplate("foo")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", found.Version)
		require.Equal(t, []string{"0.1.0"}, found.Releases)
	})

	t.Run("same bump on a created entry cuts 0.1.0", func(t *testing.T) {
		dir := setup(t)
		out, err := execRegistryRelease(t, "foo", "same", "fs", "--force")
		require.NoError(t, err)
		require.Contains(t, out, "foo@0.1.0")
		found, err := load(t, dir).FindTemplate("foo")
		require.NoError(t, err)
		require.True(t, found.IsReleased())
		require.Equal(t, []string{"0.1.0"}, found.Releases)
		_ = dir
	})

	t.Run("new template without create starts at 0.1.0", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		_, _, err := execRegistryInit(t, "--author", "Jane Doe")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates", "bare"), 0o750))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "templates", "bare", ".turutan.yml"),
			[]byte("min-engine: \">=0.1.0\"\n"), 0o600))
		_, err = execRegistryRelease(t, "bare", "major", "fs")
		require.NoError(t, err)
		found, err := load(t, dir).FindTemplate("bare")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", found.Version)
	})

	t.Run("same bump without force refuses to overwrite", func(t *testing.T) {
		dir := setup(t)
		_, err := execRegistryRelease(t, "foo", "patch", "fs")
		require.NoError(t, err)
		_, err = execRegistryRelease(t, "foo", "same", "fs")
		require.Error(t, err, "released version already has a manifest and archive")
		_, err = execRegistryRelease(t, "foo", "same", "fs", "--force")
		require.NoError(t, err)
		found, err := load(t, dir).FindTemplate("foo")
		require.NoError(t, err)
		require.Equal(t, []string{"0.1.0"}, found.Releases)
		_ = dir
	})

	t.Run("author override and archive layout", func(t *testing.T) {
		dir := setup(t)
		_, err := execRegistryRelease(t, "foo", "minor", "fs",
			"--author", "John Doe <john@example.com>")
		require.NoError(t, err)
		manifest, err := registry.LoadVersionedManifest(dir, "foo", "0.1.0")
		require.NoError(t, err)
		require.Equal(t, "John Doe <john@example.com>", manifest.Author.String())
		archive := filepath.Join(dir, "templates", "foo", "releases", "0.1.0.tar.gz")
		info, err := os.Stat(archive)
		require.NoError(t, err)
		require.Greater(t, info.Size(), int64(0))
	})

	t.Run("rejects bad bump scheme and missing template", func(t *testing.T) {
		setup(t)
		_, err := execRegistryRelease(t, "foo", "mega", "fs")
		require.Error(t, err)
		_, err = execRegistryRelease(t, "foo", "minor", "ftp")
		require.Error(t, err)
		_, err = execRegistryRelease(t, "missing", "minor", "fs")
		require.Error(t, err)
	})

	t.Run("fails outside a registry", func(t *testing.T) {
		t.Chdir(t.TempDir())
		_, err := execRegistryRelease(t, "foo", "minor", "fs")
		require.Error(t, err)
	})
}
