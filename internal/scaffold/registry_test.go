// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/registry"
)

const (
	testRegistryName     = "testreg"
	testRegistryTemplate = "test"
	testRegistryVersion  = "0.1.0"
)

// makeTestRegistry builds an fs registry with one template and returns the
// registries map plus a latest-tracking ref for it. Files land in
// templates/test/ alongside the authoring manifest text.
// ~keep
func makeTestRegistry(t *testing.T, manifest string, files map[string]string) (map[string]string, RegistryRef) {
	t.Helper()
	regDir := t.TempDir()
	author, err := registry.ParseAuthor("Test Author <test@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	index := &registry.RegistryIndex{Name: testRegistryName, Version: "1.0.0", Author: author}
	if err := registry.SaveIndexFile(filepath.Join(regDir, registry.IndexFileName), index); err != nil {
		t.Fatal(err)
	}
	tmplDir := filepath.Join(regDir, "templates", testRegistryTemplate)
	if manifest != "" {
		files = withManifest(files, manifest)
	}
	writeTestFiles(t, tmplDir, files)
	releaseTestTemplate(t, regDir, testRegistryTemplate)
	regs := map[string]string{testRegistryName: "fs:" + filepath.ToSlash(regDir)}
	return regs, RegistryRef{Registry: testRegistryName, Template: testRegistryTemplate}
}

func withManifest(files map[string]string, manifest string) map[string]string {
	out := make(map[string]string, len(files)+1)
	for k, v := range files {
		out[k] = v
	}
	out[".turutan.yml"] = manifest
	return out
}

func writeTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// releaseTestTemplate cuts a patch release of the template in regDir,
// mirroring `registry release` for fs registries in tests.
// ~keep
func releaseTestTemplate(t *testing.T, regDir, name string) string {
	t.Helper()
	indexPath := filepath.Join(regDir, registry.IndexFileName)
	index, err := registry.LoadIndexFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, findErr := index.FindTemplate(name)
	version := testRegistryVersion
	if findErr == nil {
		version, err = registry.BumpVersion(existing.Version, registry.ReleasePatch)
		if err != nil {
			t.Fatal(err)
		}
	}
	srcDir := filepath.Join(regDir, "templates", name)
	staged, cleanup, err := registry.StageReleaseTree(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	uri, err := registry.ParseURI(registry.ReleaseSnapshotPath(name, version))
	if err != nil {
		t.Fatal(err)
	}
	backendURI, err := uri.ResolveRelativeTo(regDir)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := registry.BackendFor(backendURI.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := backend.Bundle(t.Context(), staged, backendURI, registry.AuthConfig{})
	if err != nil {
		t.Fatal(err)
	}
	author := index.Author
	var contributors []registry.Author
	var releases []string
	if findErr == nil {
		author = existing.Author
		contributors = existing.Contributors
		releases = existing.Releases
	}
	templateCfg, err := config.LoadManifest(os.DirFS(srcDir))
	if err != nil {
		t.Fatal(err)
	}
	manifestHash, err := registry.TemplateManifestHash(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	entry := registry.TemplateManifest{
		Name:         name,
		Version:      version,
		Author:       author,
		Contributors: contributors,
		URI:          uri,
		Releases:     append([]string{version}, releases...),
		Digest:       digest,
		MinEngine:    templateCfg.MinEngine,
		ManifestHash: manifestHash,
		License:      templateCfg.License,
	}
	seen := map[string]bool{}
	deduped := entry.Releases[:0]
	for _, release := range entry.Releases {
		if !seen[release] {
			seen[release] = true
			deduped = append(deduped, release)
		}
	}
	entry.Releases = deduped
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SaveVersionedManifest(regDir, &entry); err != nil {
		t.Fatal(err)
	}
	registry.UpsertIndexRelease(index, entry)
	if err := registry.SaveIndexFile(indexPath, index); err != nil {
		t.Fatal(err)
	}
	return version
}

// writeTestRegistryFiles writes files into the registry template without
// cutting a release, for batching multiple changes into one release.
// ~keep
func writeTestRegistryFiles(t *testing.T, regs map[string]string, name string, files map[string]string) {
	t.Helper()
	regDir := testRegistryDir(t, regs)
	writeTestFiles(t, filepath.Join(regDir, "templates", name), files)
}

// removeTestTemplateFile removes a file from the registry template and cuts
// a new patch release.
// ~keep
func removeTestTemplateFile(t *testing.T, regs map[string]string, name, path string) string {
	t.Helper()
	regDir := testRegistryDir(t, regs)
	full := filepath.Join(regDir, "templates", name, filepath.FromSlash(path))
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	return releaseTestTemplate(t, regDir, name)
}

// updateTestTemplateFile rewrites a file in the test registry template and
// cuts a new patch release, so check/update tests can observe drift.
// ~keep
func updateTestTemplateFile(t *testing.T, regs map[string]string, name, path, content string) string {
	t.Helper()
	regDir := testRegistryDir(t, regs)
	full := filepath.Join(regDir, "templates", name, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return releaseTestTemplate(t, regDir, name)
}

func testRegistryDir(t *testing.T, regs map[string]string) string {
	t.Helper()
	raw, ok := regs[testRegistryName]
	if !ok {
		t.Fatalf("test registries lack %q", testRegistryName)
	}
	return strings.TrimPrefix(raw, "fs:")
}

// mustInitTestProject inits target from the test registry and fails fast.
// ~keep
func mustInitTestProject(t *testing.T, target string, regs map[string]string, ref RegistryRef, opts Options) {
	t.Helper()
	if err := Init(t.Context(), target, ref, regs, opts); err != nil {
		t.Fatalf("Init error: %v", err)
	}
}

// initRegistryProject builds a one-template registry and inits a project
// from its latest release, returning the project dir and registries map.
// ~keep
func initRegistryProject(t *testing.T, manifest string, files map[string]string) (string, map[string]string) {
	t.Helper()
	regs, ref := makeTestRegistry(t, manifest, files)
	target := filepath.Join(t.TempDir(), "proj")
	mustInitTestProject(t, target, regs, ref, testOptions(&strings.Builder{}))
	return target, regs
}
