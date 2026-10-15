// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LazyGeniusMan/turutan/internal/schemagen"
)

// IndexFileName is the registry index file name (JSON only).
// ~keep
const IndexFileName = "turutan.json"

// ErrInvalidIndex is matched with errors.Is when an index fails validation.
// ~keep
var ErrInvalidIndex = errors.New("invalid index")

// ErrTemplateNotFound is matched with errors.Is when a template is absent.
// ~keep
var ErrTemplateNotFound = errors.New("template not found")

// RegistryTemplate is one index entry: the latest manifest fields plus the
// newest-first releases history. Versioned files (TemplateManifest) hold a
// single version with no releases list.
//
// distinctly from the single-version TemplateManifest file shape.
//
//nolint:revive // ~keep: RegistryTemplate names the index-entry shape
type RegistryTemplate struct {
	Name         string   `json:"name"`
	Version      string   `json:"version,omitempty"`
	Author       Author   `json:"author"`
	Contributors []Author `json:"contributors,omitempty"`
	URI          *URI     `json:"uri,omitempty"`
	Releases     []string `json:"releases,omitempty"`
	Digest       string   `json:"digest,omitempty"`
	MinEngine    string   `json:"min-engine,omitempty"`
	ManifestHash string   `json:"manifestHash,omitempty"`
	License      string   `json:"license,omitempty"`
}

// IsReleased reports whether the entry owns at least one release. A
// created-but-unreleased entry (recorded by `registry create`, no
// `registry release` yet) carries no version, URI, digest or releases
// history: the first release for any bump starts at 0.1.0.
// ~keep
func (t *RegistryTemplate) IsReleased() bool {
	return t != nil && len(t.Releases) > 0
}

// Validate reports whether the index entry is well-formed. Entries with
// no releases are created-but-unreleased: they must carry no release
// artifacts (version, URI, digest, manifest hash), only identity.
// ~keep
func (t *RegistryTemplate) Validate() error {
	if !t.IsReleased() {
		return t.validateUnreleased()
	}
	if t.URI == nil {
		return fmt.Errorf("%w: released template %q must carry a uri", ErrInvalidIndex, t.Name)
	}
	manifest := TemplateManifest{
		SchemaVersion: SchemaVersion,
		Name:          t.Name,
		Version:       t.Version,
		Author:        t.Author,
		Contributors:  t.Contributors,
		URI:           *t.URI,
		Digest:        t.Digest,
		MinEngine:     t.MinEngine,
		ManifestHash:  t.ManifestHash,
		License:       t.License,
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	for i, release := range t.Releases {
		if _, err := ParseStrict(release); err != nil {
			return fmt.Errorf("%w: releases[%d]: %w", ErrInvalidIndex, i, err)
		}
	}
	if !IsNewestFirst(t.Releases) && len(t.Releases) > 0 {
		return fmt.Errorf("%w: releases must be strict semver sorted newest-first", ErrInvalidIndex)
	}
	return nil
}

func (t *RegistryTemplate) validateUnreleased() error {
	if !validResourceName(t.Name) {
		return fmt.Errorf("%w: name %q must match [a-z0-9._/-]", ErrInvalidIndex, t.Name)
	}
	if err := t.Author.Validate(); err != nil {
		return fmt.Errorf("%w: author: %w", ErrInvalidIndex, err)
	}
	for i, contributor := range t.Contributors {
		if err := contributor.Validate(); err != nil {
			return fmt.Errorf("%w: contributors[%d]: %w", ErrInvalidIndex, i, err)
		}
	}
	if t.Version != "" {
		return fmt.Errorf("%w: unreleased template %q must not carry version %q", ErrInvalidIndex, t.Name, t.Version)
	}
	if t.URI != nil {
		return fmt.Errorf("%w: unreleased template %q must not carry uri %q", ErrInvalidIndex, t.Name, t.URI.String())
	}
	if t.Digest != "" {
		return fmt.Errorf("%w: unreleased template %q must not carry a digest", ErrInvalidIndex, t.Name)
	}
	if t.ManifestHash != "" {
		return fmt.Errorf("%w: unreleased template %q must not carry a manifest hash", ErrInvalidIndex, t.Name)
	}
	return nil
}

// Manifest returns the single-version view of this entry. Unreleased
// entries yield a manifest with no version or URI; callers resolving it
// fail at fetch time, never at index load time.
// ~keep
func (t *RegistryTemplate) Manifest() TemplateManifest {
	out := TemplateManifest{
		SchemaVersion: SchemaVersion,
		Name:          t.Name,
		Version:       t.Version,
		Author:        t.Author,
		Contributors:  t.Contributors,
		Digest:        t.Digest,
		MinEngine:     t.MinEngine,
		ManifestHash:  t.ManifestHash,
		License:       t.License,
	}
	if t.URI != nil {
		out.URI = *t.URI
	}
	return out
}

// RegistryIndex is the registry catalog: one entry per template holding its
// latest release plus history. The index file itself carries the registry
// identity.
//
//nolint:revive // ~keep: RegistryIndex is the mandated public name used across registry subcommands.
type RegistryIndex struct {
	SchemaVersion int                `json:"schema_version"`
	Schema        string             `json:"$schema,omitempty"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Author        Author             `json:"author"`
	Templates     []RegistryTemplate `json:"templates"`
}

// ParseIndex decodes and validates index JSON, rejecting unknown fields.
// ~keep
func ParseIndex(data []byte) (*RegistryIndex, error) {
	var index RegistryIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil {
		return nil, fmt.Errorf("parsing registry index: %w", err)
	}
	if err := index.Validate(); err != nil {
		return nil, err
	}
	return &index, nil
}

// LoadIndexFile reads, decodes and validates the index file at path.
// ~keep
func LoadIndexFile(path string) (*RegistryIndex, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the explicit caller-supplied index file
	if err != nil {
		return nil, fmt.Errorf("reading registry index %q: %w", path, err)
	}
	index, err := ParseIndex(data)
	if err != nil {
		return nil, fmt.Errorf("loading registry index %q: %w", path, err)
	}
	return index, nil
}

// SaveIndexFile validates the index and writes it to path as indented JSON,
// stamping the current schema_version with its relative $schema reference
// and regenerating the registry index schema beside it.
// ~keep
func SaveIndexFile(path string, index *RegistryIndex) error {
	index.SchemaVersion = SchemaVersion
	index.Schema = IndexSchemaRef
	if err := index.Validate(); err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := schemagen.WriteFile(
			filepath.Join(dir, filepath.FromSlash(IndexSchemaFileName)), IndexJSONSchema()); err != nil {
			return err
		}
	}
	data, err := marshalJSONIndent(index)
	if err != nil {
		return fmt.Errorf("encoding registry index %q: %w", path, err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing registry index %q: %w", path, err)
	}
	return nil
}

// Validate reports whether the index identity and every template entry are
// well-formed, with unique template names.
// ~keep
func (r *RegistryIndex) Validate() error {
	if r == nil {
		return fmt.Errorf("%w: index is nil", ErrInvalidIndex)
	}
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"%w: unsupported schema_version %d: this engine reads v%d only",
			ErrInvalidIndex, r.SchemaVersion, SchemaVersion)
	}
	if !validResourceName(r.Name) {
		return fmt.Errorf("%w: name %q must match [a-z0-9._/-]", ErrInvalidIndex, r.Name)
	}
	if _, err := ParseStrict(r.Version); err != nil {
		return fmt.Errorf("%w: version: %w", ErrInvalidIndex, err)
	}
	if err := r.Author.Validate(); err != nil {
		return fmt.Errorf("%w: author: %w", ErrInvalidIndex, err)
	}
	seen := make(map[string]bool, len(r.Templates))
	for i := range r.Templates {
		if err := r.Templates[i].Validate(); err != nil {
			return fmt.Errorf("%w: templates[%d]: %w", ErrInvalidIndex, i, err)
		}
		if seen[r.Templates[i].Name] {
			return fmt.Errorf("%w: duplicate template %q", ErrInvalidIndex, r.Templates[i].Name)
		}
		seen[r.Templates[i].Name] = true
	}
	return nil
}

// FindTemplate returns the index entry for name or ErrTemplateNotFound.
// ~keep
func (r *RegistryIndex) FindTemplate(name string) (*RegistryTemplate, error) {
	for i := range r.Templates {
		if r.Templates[i].Name == name {
			return &r.Templates[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrTemplateNotFound, name)
}
