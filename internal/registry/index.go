// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// IndexFileName is the default registry index file name.
// ~keep
const IndexFileName = "registry.yml"

// IndexFileNameAlt is the accepted alternate index file name.
// ~keep
const IndexFileNameAlt = "registry.yaml"

// ErrInvalidIndex is matched with errors.Is when an index fails validation.
// ~keep
var ErrInvalidIndex = errors.New("invalid index")

// ErrTemplateNotFound is matched with errors.Is when a template is absent.
// ~keep
var ErrTemplateNotFound = errors.New("template not found")

// RegistryIndex is the registry catalog: one entry per template holding its
// latest release. The index file itself carries the registry identity.
//
//nolint:revive // ~keep: RegistryIndex is the mandated public name used across registry subcommands.
type RegistryIndex struct {
	Name      string             `yaml:"name"`
	Version   string             `yaml:"version"`
	Author    Author             `yaml:"author"`
	Templates []TemplateManifest `yaml:"templates"`
}

// ParseIndex decodes and validates index YAML, rejecting unknown fields.
// ~keep
func ParseIndex(data []byte) (*RegistryIndex, error) {
	var index RegistryIndex
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
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

// SaveIndexFile validates the index and writes it to path.
// ~keep
func SaveIndexFile(path string, index *RegistryIndex) error {
	if err := index.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(index)
	if err != nil {
		return fmt.Errorf("encoding registry index %q: %w", path, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
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

// FindTemplate returns the manifest for name or ErrTemplateNotFound.
// ~keep
func (r *RegistryIndex) FindTemplate(name string) (*TemplateManifest, error) {
	for i := range r.Templates {
		if r.Templates[i].Name == name {
			return &r.Templates[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrTemplateNotFound, name)
}
