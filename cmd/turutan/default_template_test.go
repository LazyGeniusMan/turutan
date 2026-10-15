// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
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
// the authoring manifest migrated off the legacy git source, the committed
// OCI release at 0.1.0 with its single-version manifest and index history,
// bundle-bytes digest equivalence with the pushed OCI layer, and offline
// latest-version resolution from the committed index. It touches no network.
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

	t.Run("registry index tracks default at 0.1.0 with OCI uri", func(t *testing.T) {
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, "turutan", index.Name)
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", entry.Version)
		require.NotEmpty(t, entry.Releases)
		require.Equal(t, "0.1.0", entry.Releases[0])
		require.Equal(t, registry.SchemeOCI, entry.URI.Scheme)
		require.Equal(t, "0.1.0", entry.URI.Ref)
		require.Equal(t, registry.DefaultTemplateRef("0.1.0"), entry.URI.String())
		require.NotEmpty(t, entry.Digest)
		staged, cleanup, err := registry.StageReleaseTree(treeDir)
		require.NoError(t, err)
		defer cleanup()
		digest, err := registry.DigestDir(staged)
		require.NoError(t, err)
		require.Equal(t, entry.Digest, digest,
			"committed digest must equal the staged bundle digest the OCI push records")
	})

	t.Run("versioned manifest is a single version and matches index", func(t *testing.T) {
		manifestPath := registry.VersionedManifestPath(root, "default", "0.1.0")
		require.FileExists(t, manifestPath)
		require.Equal(t, filepath.Join(root, "templates", "default", "manifest", "0.1.0.json"), manifestPath)
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

	t.Run("bundle bytes equal the OCI layer bytes", func(t *testing.T) {
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		versioned, err := registry.LoadVersionedManifest(root, "default", "0.1.0")
		require.NoError(t, err)
		require.Equal(t, entry.Digest, versioned.Digest)
		staged, cleanup, err := registry.StageReleaseTree(treeDir)
		require.NoError(t, err)
		defer cleanup()
		blob, digest, err := registry.BundleBytes(staged)
		require.NoError(t, err)
		require.Equal(t, entry.Digest, digest,
			"fs bundle bytes and the OCI layer share one BundleBytes path: digests must match")
		_, repeat, err := registry.BundleBytes(staged)
		require.NoError(t, err)
		require.Equal(t, digest, repeat, "bundle bytes must be deterministic")
		dest := t.TempDir()
		require.NoError(t, registry.ExtractBundle(bytes.NewReader(blob), dest))
		require.FileExists(t, filepath.Join(dest, ".turutan.yml"))
		for _, meta := range []string{"manifest", "releases"} {
			_, err := os.Stat(filepath.Join(dest, meta))
			require.True(t, os.IsNotExist(err),
				"bundle must never contain its own release history: %s", meta)
		}
		roundTrip, err := registry.DigestDir(dest)
		require.NoError(t, err)
		require.Equal(t, entry.Digest, roundTrip,
			"bundle must stay immutable: digest must match the release manifest")
	})

	t.Run("committed OCI index drives latest-version resolution", func(t *testing.T) {
		rr, err := scaffold.ResolveRegistry(t.Context(),
			map[string]string{"local": "fs:" + filepath.ToSlash(root)}, "local")
		require.NoError(t, err)
		entry, err := rr.Idx.FindTemplate("default")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", entry.Version)
		require.Equal(t, registry.DefaultTemplateRef("0.1.0"), entry.URI.String())
		manifest := entry.Manifest()
		require.Equal(t, "default", manifest.Name)
		require.Equal(t, "0.1.0", manifest.Version)
		require.Equal(t, entry.Digest, manifest.Digest)
		require.Equal(t, entry.URI.String(), manifest.URI.String())
	})

	t.Run("state stores the relative uri", func(t *testing.T) {
		state, err := config.LoadState(os.DirFS(root))
		if err != nil {
			t.Skipf("no dogfood state in test checkout: %v", err)
		}
		require.NotContains(t, state.TemplateURI, "fs:/",
			"state must store the pre-resolution relative URI, got %q", state.TemplateURI)
		require.Equal(t, "templates/default/releases/0.1.0.tar.gz", state.TemplateURI)
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
