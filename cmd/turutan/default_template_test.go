// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/registry"
	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

// repoRootDir resolves the repository root (the turutan registry root)
// from this test file location, independent of the test working directory.
// ~keep
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this file")
	root := filepath.Join(filepath.Dir(file), "..", "..")
	abs, err := filepath.Abs(root)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(abs, registry.IndexFileName),
		"repo root must hold the registry index")
	return abs
}

// TestDefaultTemplateRegistry pins the recovered templates/default tree:
// the authoring manifest migrated off the legacy git source, the fs
// release at 0.1.0 with its versioned manifest and prepended index
// release, offline fetch with release metadata stripped, and the OCI
// publish URI building users need. It touches no network.
// ~keep
func TestDefaultTemplateRegistry(t *testing.T) {
	root := repoRootDir(t)
	treeDir := filepath.Join(root, "templates", "default")

	t.Run("authoring manifest migrated off legacy source", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(treeDir, ".turutan.yml")) // #nosec G304 -- treeDir is derived from this test file location plus a fixed manifest name
		require.NoError(t, err)
		require.NotContains(t, string(raw), "git::",
			"registry is the source of truth: no legacy source may remain")
		manifest, err := config.LoadManifest(os.DirFS(treeDir))
		require.NoError(t, err)
		require.Equal(t, ">=0.1.0", manifest.MinEngine)
		require.Equal(t, "MIT-0", manifest.License)
		require.NotEmpty(t, manifest.Includes)
		require.NotEmpty(t, manifest.Inputs["project_name"])
	})

	t.Run("registry index tracks default at 0.1.0 with fs uri", func(t *testing.T) {
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, "turutan", index.Name)
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", entry.Version)
		require.NotEmpty(t, entry.Releases)
		require.Equal(t, entry.Version, entry.Releases[0],
			"index releases must be newest-first")
		require.True(t, entry.URI.IsRelative(),
			"dogfood release uri must stay fs-relative, got %q", entry.URI.String())
		require.Equal(t, registry.ReleaseSnapshotPath("default", "0.1.0"), entry.URI.Path)
		require.NotEmpty(t, entry.Digest)
	})

	t.Run("versioned manifest exists and matches index", func(t *testing.T) {
		manifestPath := registry.VersionedManifestPath(root, "default", "0.1.0")
		require.FileExists(t, manifestPath)
		versioned, err := registry.LoadVersionedManifest(root, "default", "0.1.0")
		require.NoError(t, err)
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		require.Equal(t, entry.Version, versioned.Version)
		require.Equal(t, entry.Digest, versioned.Digest)
		require.Equal(t, entry.URI.String(), versioned.URI.String())
	})

	t.Run("release snapshot matches authoring tree minus meta", func(t *testing.T) {
		snapshot := filepath.Join(root,
			filepath.FromSlash(registry.ReleaseSnapshotPath("default", "0.1.0")))
		require.DirExists(t, snapshot)
		require.FileExists(t, filepath.Join(snapshot, ".turutan.yml"))
		for _, meta := range []string{"manifest", "releases"} {
			_, err := os.Stat(filepath.Join(snapshot, meta))
			require.True(t, os.IsNotExist(err),
				"snapshot must not bundle its own release history: %s", meta)
		}
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		digest, err := registry.DigestDir(snapshot)
		require.NoError(t, err)
		require.Equal(t, entry.Digest, digest,
			"snapshot must stay immutable: digest must match the release manifest")
	})

	t.Run("fs fetch strips release metadata", func(t *testing.T) {
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		rr := &scaffold.ResolvedRegistry{
			Name: "local",
			Root: root,
			Idx:  index,
		}
		manifest := *entry
		dir, fetchURI, cleanup, err := scaffold.FetchTemplateBundle(t.Context(), rr, &manifest)
		require.NoError(t, err)
		defer cleanup()
		require.NotEmpty(t, fetchURI)
		require.FileExists(t, filepath.Join(dir, ".turutan.yml"))
		for _, meta := range []string{"manifest", "releases"} {
			_, err := os.Stat(filepath.Join(dir, meta))
			require.True(t, os.IsNotExist(err),
				"fetched bundle must drop release metadata: %s", meta)
		}
	})

	t.Run("oci publish uri builds offline with override", func(t *testing.T) {
		const publishRepo = "oci://ghcr.io/lazygeniusman/turutan/templates/default"
		uri, err := registry.BuildReleaseURI("oci", "default", "0.2.0", publishRepo, nil)
		require.NoError(t, err)
		require.Equal(t, registry.SchemeOCI, uri.Scheme)
		require.Equal(t, "0.2.0", uri.Ref)
		require.True(t, strings.HasPrefix(uri.String(), publishRepo+":"),
			"publish uri must address the user-facing repo, got %q", uri.String())
	})

	t.Run("zero-config default resolves to oci fallback", func(t *testing.T) {
		ref := scaffold.ResolveTemplateFlag("", "", "")
		require.Equal(t, "turutan", ref.Registry)
		require.Equal(t, "default", ref.Template)
		uri, err := registry.ResolveRegistryURI(map[string]string{}, "turutan")
		require.NoError(t, err)
		require.Equal(t, registry.DefaultRegistryURI, uri.String())
	})
}
