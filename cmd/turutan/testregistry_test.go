// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/registry"
	"github.com/LazyGeniusMan/turutan/internal/scaffold"
)

const (
	cmdTestRegistry   = "testreg"
	cmdTestTemplate   = "test"
	cmdTestVersion    = "0.1.0"
	cmdTestAuthorText = "Test Author <test@example.com>"
)

// makeCmdRegistry builds an fs registry with one released template for CLI
// tests. It returns the registry dir, the registry URI for scaffold calls
// and the --template flag value tracking latest.
// ~keep
func makeCmdRegistry(t *testing.T, manifest string, files map[string]string) (string, string, string) {
	t.Helper()
	regDir := t.TempDir()
	author, err := registry.ParseAuthor(cmdTestAuthorText)
	if err != nil {
		t.Fatal(err)
	}
	index := &registry.RegistryIndex{Name: cmdTestRegistry, Version: "1.0.0", Author: author}
	if err := registry.SaveIndexFile(filepath.Join(regDir, registry.IndexFileName), index); err != nil {
		t.Fatal(err)
	}
	merged := make(map[string]string, len(files)+1)
	for k, v := range files {
		merged[k] = v
	}
	if manifest != "" {
		merged["template.yml"] = manifest
	}
	for name, content := range merged {
		path := filepath.Join(regDir, "templates", cmdTestTemplate, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	releaseCmdRegistry(t, regDir, cmdTestTemplate)
	registryURI := "fs:" + filepath.ToSlash(regDir)
	return regDir, registryURI, registryURI + "/" + cmdTestTemplate
}

// releaseCmdRegistry cuts a patch release of the template in regDir,
// mirroring `registry release` for fs registries in CLI tests.
// ~keep
func releaseCmdRegistry(t *testing.T, regDir, name string) string {
	t.Helper()
	indexPath := filepath.Join(regDir, registry.IndexFileName)
	index, err := registry.LoadIndexFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, findErr := index.FindTemplate(name)
	version := cmdTestVersion
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
	if findErr == nil {
		author = existing.Author
		contributors = existing.Contributors
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
		SchemaVersion: registry.SchemaVersion,
		Name:          name, Version: version, Author: author, Contributors: contributors,
		URI:    uri,
		Digest: digest, MinEngine: templateCfg.MinEngine,
		ManifestHash: manifestHash, License: templateCfg.License,
	}
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

// updateCmdRegistryFile rewrites a registry template file and cuts a new
// patch release.
// ~keep
func updateCmdRegistryFile(t *testing.T, regDir, name, path, content string) string {
	t.Helper()
	full := filepath.Join(regDir, "templates", name, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return releaseCmdRegistry(t, regDir, name)
}

func scaffoldRef(t *testing.T, flag string) scaffold.RegistryRef {
	t.Helper()
	ref, err := scaffold.ParseRegistryRef(flag)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
