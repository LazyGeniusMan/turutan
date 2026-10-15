// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
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

const defaultTestAuthor = "Andriyansyah Nurrachman <git@yansyah.my.id> (https://yansyah.my.id)"

// repoRootDir resolves the repository root from this test file location,
// independent of the test working directory. It pins the committed
// created-but-unreleased default entry plus the unreleased source tree:
// zero releases until CI dispatch.
// ~keep
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must report this file")
	root := filepath.Join(filepath.Dir(file), "..", "..")
	abs, err := filepath.Abs(root)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(abs, registry.IndexFileName),
		"repo root must hold the committed registry index")
	require.FileExists(t, filepath.Join(abs, "templates", "default", ".turutan.yml"),
		"repo root must hold the unreleased default source tree")
	return abs
}

// releaseDefaultEphemeral copies the worktree source tree into a temp
// registry root, cuts one fs release there, and returns the root plus the
// indexed entry. Every subtest gets its own root: the committed worktree
// index (created, zero releases) is never mutated nor read — each root
// seeds its own equivalent unreleased entry and cuts the first release.
// ~keep
func releaseDefaultEphemeral(t *testing.T) (string, *registry.RegistryTemplate) {
	t.Helper()
	root := repoRootDir(t)
	regDir := t.TempDir()
	author, err := registry.ParseAuthor(defaultTestAuthor)
	require.NoError(t, err)
	index := &registry.RegistryIndex{
		Name: "turutan", Version: "0.1.0", Author: author,
		Templates: []registry.RegistryTemplate{{Name: "default", Author: author}},
	}
	require.NoError(t, registry.SaveIndexFile(filepath.Join(regDir, registry.IndexFileName), index))
	srcDir := filepath.Join(root, "templates", "default")
	dstDir := filepath.Join(regDir, "templates", "default")
	require.NoError(t, registry.CopyTree(srcDir, dstDir))
	loaded, err := registry.LoadIndexFile(filepath.Join(regDir, registry.IndexFileName))
	require.NoError(t, err)
	seeded, err := loaded.FindTemplate("default")
	require.NoError(t, err)
	require.False(t, seeded.IsReleased(), "seeded entry must mirror the committed unreleased shape")
	next, existed, err := registry.NextReleaseVersion(loaded, "default", registry.ReleasePatch)
	require.NoError(t, err)
	require.False(t, existed, "first release from an unreleased entry has no prior version")
	require.Equal(t, "0.1.0", next)
	uri, err := registry.BuildReleaseURI("fs", "default", next, "", nil)
	require.NoError(t, err)
	staged, cleanup, err := registry.StageReleaseTree(dstDir)
	require.NoError(t, err)
	defer cleanup()
	backendURI, err := uri.ResolveRelativeTo(regDir)
	require.NoError(t, err)
	backend, err := registry.BackendFor(backendURI.Scheme)
	require.NoError(t, err)
	digest, err := backend.Bundle(t.Context(), staged, backendURI, registry.AuthConfig{})
	require.NoError(t, err)
	templateCfg, err := config.LoadManifest(os.DirFS(dstDir))
	require.NoError(t, err)
	manifestHash, err := registry.TemplateManifestHash(dstDir)
	require.NoError(t, err)
	manifest := registry.TemplateManifest{
		Name: "default", Version: next, Author: author,
		URI: uri, Digest: digest, MinEngine: templateCfg.MinEngine,
		ManifestHash: manifestHash, License: templateCfg.License,
	}
	require.NoError(t, manifest.Validate())
	_, err = registry.SaveVersionedManifest(regDir, &manifest)
	require.NoError(t, err)
	registry.UpsertIndexRelease(loaded, manifest)
	require.NoError(t, registry.SaveIndexFile(filepath.Join(regDir, registry.IndexFileName), loaded))
	entry, err := loaded.FindTemplate("default")
	require.NoError(t, err)
	return regDir, entry
}

// TestDefaultTemplateRegistry pins the committed created-but-unreleased
// default entry (indexed, zero releases) plus the unreleased
// templates/default source tree: the authoring manifest stays off the
// legacy git source, the first cut starts at 0.1.0 with a relative fs URI,
// bundle bytes stay deterministic and immutable, and the OCI first-publish
// path needs --uri-override. The first real release lands via CI manual
// dispatch. It touches no network.
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

	t.Run("worktree holds created-but-unreleased default entry, zero releases", func(t *testing.T) {
		index, err := registry.LoadIndexFile(filepath.Join(root, registry.IndexFileName))
		require.NoError(t, err)
		require.Equal(t, "turutan", index.Name)
		require.Len(t, index.Templates, 1, "committed index must record the created default template")
		entry, err := index.FindTemplate("default")
		require.NoError(t, err)
		require.False(t, entry.IsReleased(), "committed entry must carry zero released versions")
		require.Empty(t, entry.Version, "unreleased entry must not carry a version")
		require.Nil(t, entry.URI, "unreleased entry must not carry a uri")
		require.Empty(t, entry.Releases, "unreleased entry must not carry releases history")
		require.Empty(t, entry.Digest, "unreleased entry must not carry a digest")
		require.Equal(t, defaultTestAuthor, entry.Author.String())
		raw, err := os.ReadFile(filepath.Join(root, registry.IndexFileName)) // #nosec G304 -- root is derived from this test file location plus the fixed index file name
		require.NoError(t, err)
		for _, banned := range []string{"digest", "releases", "oci://", "templates/default/releases"} {
			require.NotContains(t, string(raw), banned,
				"unreleased entry must not reference a release: %s", banned)
		}
		var decoded struct {
			Templates []map[string]any `json:"templates"`
		}
		require.NoError(t, json.Unmarshal(raw, &decoded))
		require.Len(t, decoded.Templates, 1)
		for key := range decoded.Templates[0] {
			require.Contains(t, []string{"name", "author"}, key,
				"unreleased entry must serialize identity only, got %q", key)
		}
		entries, err := os.ReadDir(filepath.Join(treeDir, "manifest"))
		if err == nil {
			require.Empty(t, entries, "zero releases means no committed manifests")
		}
	})

	t.Run("ephemeral fs release starts at 0.1.0 with relative uri", func(t *testing.T) {
		regDir, entry := releaseDefaultEphemeral(t)
		require.Equal(t, "0.1.0", entry.Version)
		require.Equal(t, []string{"0.1.0"}, entry.Releases)
		require.Equal(t, registry.SchemeFS, entry.URI.Scheme)
		require.Equal(t, registry.ReleaseSnapshotPath("default", "0.1.0"), entry.URI.String())
		require.NotEmpty(t, entry.Digest)
		staged, cleanup, err := registry.StageReleaseTree(filepath.Join(regDir, "templates", "default"))
		require.NoError(t, err)
		defer cleanup()
		digest, err := registry.DigestDir(staged)
		require.NoError(t, err)
		require.Equal(t, entry.Digest, digest,
			"ephemeral digest must equal the staged bundle digest")
	})

	t.Run("versioned manifest is a single version and matches index", func(t *testing.T) {
		regDir, entry := releaseDefaultEphemeral(t)
		manifestPath := registry.VersionedManifestPath(regDir, "default", "0.1.0")
		require.FileExists(t, manifestPath)
		versioned, err := registry.LoadVersionedManifest(regDir, "default", "0.1.0")
		require.NoError(t, err)
		require.Equal(t, entry.Version, versioned.Version)
		require.Equal(t, entry.Digest, versioned.Digest)
		require.Equal(t, entry.URI.String(), versioned.URI.String())
	})

	t.Run("bundle bytes equal the fs archive bytes", func(t *testing.T) {
		regDir, entry := releaseDefaultEphemeral(t)
		versioned, err := registry.LoadVersionedManifest(regDir, "default", "0.1.0")
		require.NoError(t, err)
		require.Equal(t, entry.Digest, versioned.Digest)
		staged, cleanup, err := registry.StageReleaseTree(filepath.Join(regDir, "templates", "default"))
		require.NoError(t, err)
		defer cleanup()
		blob, digest, err := registry.BundleBytes(staged)
		require.NoError(t, err)
		require.Equal(t, entry.Digest, digest,
			"fs bundle bytes share one BundleBytes path: digests must match")
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

	t.Run("ephemeral fs index drives latest-version resolution", func(t *testing.T) {
		regDir, _ := releaseDefaultEphemeral(t)
		rr, err := scaffold.ResolveRegistry(t.Context(),
			map[string]string{"local": "fs:" + filepath.ToSlash(regDir)}, "local")
		require.NoError(t, err)
		entry, err := rr.Idx.FindTemplate("default")
		require.NoError(t, err)
		require.Equal(t, "0.1.0", entry.Version)
		require.Equal(t, registry.ReleaseSnapshotPath("default", "0.1.0"), entry.URI.String())
		manifest := entry.Manifest()
		require.Equal(t, "default", manifest.Name)
		require.Equal(t, "0.1.0", manifest.Version)
		require.Equal(t, entry.Digest, manifest.Digest)
		require.Equal(t, entry.URI.String(), manifest.URI.String())
	})

	t.Run("oci first release needs uri override", func(t *testing.T) {
		_, err := registry.BuildReleaseURI("oci", "default", "0.1.0", "", nil)
		require.Error(t, err, "unindexed OCI first cut cannot guess the host")
		const publishRepo = "oci://ghcr.io/lazygeniusman/turutan/templates/default"
		uri, err := registry.BuildReleaseURI("oci", "default", "0.1.0", publishRepo, nil)
		require.NoError(t, err)
		require.Equal(t, registry.SchemeOCI, uri.Scheme)
		require.Equal(t, "0.1.0", uri.Ref)
		require.True(t, strings.HasPrefix(uri.String(), publishRepo+":"),
			"publish uri must address the user-facing repo, got %q", uri.String())
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
