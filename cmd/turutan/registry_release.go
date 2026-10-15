// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/registry"
)

type registryReleaseFlags struct {
	force       bool
	author      string
	uriOverride string
}

func newRegistryReleaseCmd() *cobra.Command {
	flags := &registryReleaseFlags{}
	cmd := &cobra.Command{
		Use:   "release <template-name> <major|minor|patch|same> <fs|http|s3|git|oci>",
		Short: "Cut a versioned template release in the current registry",
		Long: `Cut a versioned template release in the current registry: the
command walks up from the working directory to find the registry index
(registry.json), resolves the next version, bundles templates/<name>/
through the scheme backend, writes the single-version manifest
templates/<name>/manifest/<version>.json and records the version at the
front of the index releases history.

Archive layout: fs and git releases store one gzip-tar archive per
version at templates/<name>/releases/<version>.tar.gz. The fs backend
writes the archive file there; the git backend stores the same archive
bytes at <subpath>/releases/<version>.tar.gz under tag
templates/<name>/<version> (subpath defaults to templates/<name>).
http(s)/s3 backends upload the same bundle bytes to the indexed URI, and
oci pushes an OCI artifact. Bundles never contain manifest/ or releases/
(the collector skips them), so no strip step runs on fetch.

Versioning: major|minor|patch bumps the indexed latest (new templates
start at 0.1.0); same rebuilds the indexed version in place with no bump.
--force rebuilds: it overwrites the versioned manifest file, the release
archive, and re-records an already-listed version. Without --force the
command refuses to overwrite any of them. Non-fs first releases need
--uri-override because the host or bucket cannot be guessed; later
releases adapt the indexed URI. Auth resolves from
TURUTAN_REGISTRY_<NAME>_* and scheme environment variables; secrets are
never logged.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegistryRelease(cmd, args[0], args[1], args[2], flags)
		},
	}
	cmd.Flags().BoolVar(&flags.force, "force", false,
		"rebuild in place: overwrite the versioned manifest, release archive, or an already-listed version (required with the \"same\" bump)")
	cmd.Flags().StringVar(&flags.author, "author", "",
		`release author as "Name <email> (website)" (default: indexed author)`)
	cmd.Flags().StringVar(&flags.uriOverride, "uri-override", "",
		"explicit release URI (scheme must match the release scheme)")
	return cmd
}

func runRegistryRelease(cmd *cobra.Command, name, bumpText, schemeText string, flags *registryReleaseFlags) error {
	name = strings.TrimSpace(name)
	if err := registry.ValidateTemplateName(name); err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	bump, err := registry.ParseReleaseBump(bumpText)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	scheme := strings.TrimSpace(schemeText)
	if scheme == "https" {
		scheme = "http"
	}
	switch scheme {
	case "fs", "http", "s3", "git", "oci":
	default:
		return fmt.Errorf("registry release: invalid scheme %q: want fs|http|s3|git|oci", schemeText)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	root, indexPath, err := findRegistryRoot(cwd)
	if err != nil {
		return err
	}
	index, err := registry.LoadIndexFile(indexPath)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	existing, _ := index.FindTemplate(name)
	next, existed, err := registry.NextReleaseVersion(index, name, bump)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	srcDir := filepath.Join(root, "templates", filepath.FromSlash(name))
	info, err := os.Stat(srcDir)
	if err != nil {
		return fmt.Errorf("registry release: template dir %q: %w", srcDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("registry release: template dir %q is not a directory", srcDir)
	}
	uri, err := registry.BuildReleaseURI(scheme, name, next, flags.uriOverride, existing)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	author := index.Author
	if existing != nil {
		author = existing.Author
	}
	if trimmed := strings.TrimSpace(flags.author); trimmed != "" {
		parsed, err := registry.ParseAuthor(trimmed)
		if err != nil {
			return fmt.Errorf("registry release: %w", err)
		}
		author = parsed
	}
	var contributors []registry.Author
	if existing != nil {
		contributors = existing.Contributors
	}
	templateCfg, err := config.LoadManifest(os.DirFS(srcDir))
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	staged, cleanup, err := registry.StageReleaseTree(srcDir)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	defer cleanup()
	auth := registry.ResolveAuth(uri.String(), uri)
	backendURI := uri
	if uri.IsRelative() {
		backendURI, err = uri.ResolveRelativeTo(root)
		if err != nil {
			return fmt.Errorf("registry release: %w", err)
		}
	}
	backend, err := registry.BackendFor(backendURI.Scheme)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	digest, err := backend.Bundle(cmd.Context(), staged, backendURI, auth)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	manifestHash, err := registry.TemplateManifestHash(srcDir)
	if err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	manifest := registry.TemplateManifest{
		Name:         name,
		Version:      next,
		Author:       author,
		Contributors: contributors,
		URI:          uri,
		Digest:       digest,
		MinEngine:    templateCfg.MinEngine,
		ManifestHash: manifestHash,
		License:      templateCfg.License,
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	manifestPath := registry.VersionedManifestPath(root, name, next)
	if _, err := os.Stat(manifestPath); err == nil && !flags.force {
		return fmt.Errorf("registry release: %q already exists (use --force to overwrite)", manifestPath)
	}
	if existed && slices.Contains(existing.Releases, next) && !flags.force {
		return fmt.Errorf("registry release: version %q of %q is already released (use --force to re-release)", next, name)
	}
	if _, err := registry.SaveVersionedManifest(root, &manifest); err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	registry.UpsertIndexRelease(index, manifest)
	if err := registry.SaveIndexFile(indexPath, index); err != nil {
		return fmt.Errorf("registry release: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "released %s@%s %s %s\n",
		name, next, digest, uri.String()); err != nil {
		return err
	}
	return nil
}
