// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/registry"
)

var registryNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const registryStarterVersion = "0.1.0"

func newRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage template registries",
		Long: `Manage template registries: author a registry in the current
directory (init, create) or store named registry references in the user
config (add). The "turutan" registry resolves to its default OCI address
without an explicit add.

The registry index file is registry.yml (registry.yaml is accepted when
reading).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRegistryInitCmd(), newRegistryCreateCmd(), newRegistryAddCmd())
	return cmd
}

type registryInitFlags struct {
	name    string
	author  string
	version string
	force   bool
}

func newRegistryInitCmd() *cobra.Command {
	flags := &registryInitFlags{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a template registry in the current directory",
		Long: `Initialize a template registry in the current directory by
writing registry.yml: the registry identity (name, version, author) with
an empty template list.

The author uses the canonical "Name <email> (website)" form. Refuses to
overwrite an existing index file unless --force is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRegistryInit(cmd, flags)
		},
	}
	cmd.Flags().StringVar(&flags.name, "name", "", "registry name (default: current directory basename)")
	cmd.Flags().StringVar(&flags.author, "author", "",
		`registry author as "Name <email> (website)" (required)`)
	cmd.Flags().StringVar(&flags.version, "version", registryStarterVersion,
		"initial registry version (strict semver)")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing registry index file")
	return cmd
}

func runRegistryInit(cmd *cobra.Command, flags *registryInitFlags) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("registry init: %w", err)
	}
	name := strings.TrimSpace(flags.name)
	if name == "" {
		name = filepath.Base(cwd)
	}
	authorText := strings.TrimSpace(flags.author)
	if authorText == "" {
		return fmt.Errorf(`registry init: --author is required, e.g. --author "Jane Doe <jane@example.com> (https://example.com)"`)
	}
	author, err := registry.ParseAuthor(authorText)
	if err != nil {
		return fmt.Errorf("registry init: %w", err)
	}
	versionText := strings.TrimSpace(flags.version)
	if versionText == "" {
		versionText = registryStarterVersion
	}
	if _, err := registry.ParseStrict(versionText); err != nil {
		return fmt.Errorf("registry init: %w", err)
	}
	path := filepath.Join(cwd, registry.IndexFileName)
	if existing, ok := existingIndexFile(cwd); ok {
		if !flags.force {
			return fmt.Errorf("registry init: %q already exists (use --force to overwrite)", existing)
		}
		path = existing
	}
	index := &registry.RegistryIndex{Name: name, Version: versionText, Author: author}
	if err := index.Validate(); err != nil {
		return fmt.Errorf("registry init: %w", err)
	}
	if err := registry.SaveIndexFile(path, index); err != nil {
		return fmt.Errorf("registry init: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "initialized registry %q in %s\n", name, path); err != nil {
		return err
	}
	return nil
}

type registryCreateFlags struct {
	force bool
}

func newRegistryCreateCmd() *cobra.Command {
	flags := &registryCreateFlags{}
	cmd := &cobra.Command{
		Use:   "create <template-name>",
		Short: "Create a starter template inside the current registry",
		Long: `Create a starter template inside the current registry: the
command walks up from the working directory to find the registry index,
scaffolds templates/<name>/ with a minimal .turutan.yml, README.md and
hello.txt.tmpl, and records the template in the index.

Refuses to overwrite an existing template directory or index entry unless
--force is given.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegistryCreate(cmd, args[0], flags)
		},
	}
	cmd.Flags().BoolVar(&flags.force, "force", false,
		"overwrite an existing template directory or index entry")
	return cmd
}

func runRegistryCreate(cmd *cobra.Command, name string, flags *registryCreateFlags) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("registry create: template name is empty")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("registry create: %w", err)
	}
	root, indexPath, err := findRegistryRoot(cwd)
	if err != nil {
		return err
	}
	index, err := registry.LoadIndexFile(indexPath)
	if err != nil {
		return fmt.Errorf("registry create: %w", err)
	}
	uri, err := registry.ParseURI(filepath.ToSlash(filepath.Join("templates", name)))
	if err != nil {
		return fmt.Errorf("registry create: %w", err)
	}
	manifest := registry.TemplateManifest{
		Name:    name,
		Version: registryStarterVersion,
		Author:  index.Author,
		URI:     uri,
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("registry create: %w", err)
	}
	dir := filepath.Join(root, "templates", filepath.FromSlash(name))
	if _, err := os.Stat(dir); err == nil && !flags.force {
		return fmt.Errorf("registry create: %q already exists (use --force to overwrite)", dir)
	}
	if _, err := index.FindTemplate(name); err == nil && !flags.force {
		return fmt.Errorf("registry create: template %q is already indexed (use --force to overwrite)", name)
	}
	if err := writeRegistryStarter(dir, name); err != nil {
		return err
	}
	upsertRegistryTemplate(index, manifest)
	if err := registry.SaveIndexFile(indexPath, index); err != nil {
		return fmt.Errorf("registry create: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "created template %q in %s\n", name, dir); err != nil {
		return err
	}
	return nil
}

func writeRegistryStarter(dir, name string) error {
	files := map[string]string{
		".turutan.yml":   "min-engine: \">=0.1.0\"\nlicense: MIT-0\nconflict: inline\n",
		"README.md":      "# " + name + "\n\nA turutan template. Render it with `turutan init --defaults . <out-dir>`.\n",
		"hello.txt.tmpl": "Hello, {{ .project_name }}!\n",
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("registry create: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("registry create: %w", err)
		}
	}
	return nil
}

func upsertRegistryTemplate(index *registry.RegistryIndex, manifest registry.TemplateManifest) {
	for i := range index.Templates {
		if index.Templates[i].Name == manifest.Name {
			index.Templates[i] = manifest
			return
		}
	}
	index.Templates = append(index.Templates, manifest)
}

type registryAddFlags struct {
	force bool
}

func newRegistryAddCmd() *cobra.Command {
	flags := &registryAddFlags{}
	cmd := &cobra.Command{
		Use:   "add <registry-name> <uri>",
		Short: "Store a named registry reference in the user config",
		Long: `Store a named registry reference (URI plus auth resolved from
the environment) in the user config registries map. Accepts fs
(relative or absolute), http(s), s3, git and oci URIs.

The "turutan" name resolves to its default OCI address without an
explicit add; overriding it requires --force. Overwriting any other
stored entry also requires --force.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegistryAdd(cmd, args[0], args[1], flags)
		},
	}
	cmd.Flags().BoolVar(&flags.force, "force", false,
		`overwrite an existing entry (required to override the built-in "turutan" registry)`)
	return cmd
}

func runRegistryAdd(cmd *cobra.Command, name, rawURI string, flags *registryAddFlags) error {
	name = strings.TrimSpace(name)
	if !registryNameRe.MatchString(name) {
		return fmt.Errorf("registry add: name %q must match [A-Za-z0-9._-] and start alphanumerically", name)
	}
	parsed, err := registry.ParseURI(rawURI)
	if err != nil {
		return fmt.Errorf("registry add: %w", err)
	}
	path := config.ResolveConfigPath(appCfg.cfgFile)
	if path == "" {
		path = config.DefaultUserConfigPath()
	}
	existing, err := config.LoadRegistries(path)
	if err != nil {
		return fmt.Errorf("registry add: %w", err)
	}
	if name == registry.DefaultRegistryName {
		if _, ok := existing[name]; !ok && !flags.force {
			return fmt.Errorf(
				"registry add: %q resolves to the built-in default %s without an add (use --force to override)",
				name, registry.DefaultRegistryURI)
		}
	}
	if prev, ok := existing[name]; ok {
		switch {
		case prev == parsed.String():
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "registry %q already points at %s (%s)\n", name, prev, path); err != nil {
				return err
			}
			return nil
		case !flags.force:
			return fmt.Errorf("registry add: %q already points at %q (use --force to overwrite)", name, prev)
		}
	}
	if err := config.UpsertRegistry(path, name, parsed.String()); err != nil {
		return fmt.Errorf("registry add: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "added registry %q -> %s (%s)\n", name, parsed.String(), path); err != nil {
		return err
	}
	return nil
}

func existingIndexFile(dir string) (string, bool) {
	for _, name := range []string{registry.IndexFileName, registry.IndexFileNameAlt} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return "", false
}

func findRegistryRoot(dir string) (string, string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", fmt.Errorf("registry: resolving %q: %w", dir, err)
	}
	for {
		if path, ok := existingIndexFile(abs); ok {
			return abs, path, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", "", fmt.Errorf(
				"registry: no index (%s or %s) found in %q or its parents: run `turutan registry init` first",
				registry.IndexFileName, registry.IndexFileNameAlt, dir)
		}
		abs = parent
	}
}
