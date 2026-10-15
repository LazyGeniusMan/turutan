// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ErrInvalidManifest is matched with errors.Is when a manifest fails validation.
// ~keep
var ErrInvalidManifest = errors.New("invalid manifest")

var (
	nameRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
	manifestHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// TemplateManifest describes one template release: a single versioned file
// at templates/<name>/manifest/<version>.json. It carries no release
// history; the registry index entry (RegistryTemplate) owns the
// newest-first releases list.
// ~keep
type TemplateManifest struct {
	SchemaVersion int      `json:"schema_version"`
	Schema        string   `json:"$schema,omitempty"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Author        Author   `json:"author"`
	Contributors  []Author `json:"contributors,omitempty"`
	URI           URI      `json:"uri"`
	Digest        string   `json:"digest,omitempty"`
	MinEngine     string   `json:"min-engine,omitempty"`
	ManifestHash  string   `json:"manifestHash,omitempty"`
	License       string   `json:"license,omitempty"`
}

// Validate reports whether the manifest is internally consistent: names,
// strict semver version, author formats, URI, digest, engine constraint
// and manifest hash.
// ~keep
func (m *TemplateManifest) Validate() error {
	if m == nil {
		return fmt.Errorf("%w: manifest is nil", ErrInvalidManifest)
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"%w: unsupported schema_version %d: this engine reads v%d only",
			ErrInvalidManifest, m.SchemaVersion, SchemaVersion)
	}
	if !validResourceName(m.Name) {
		return fmt.Errorf("%w: name %q must match [a-z0-9._/-]", ErrInvalidManifest, m.Name)
	}
	if _, err := ParseStrict(m.Version); err != nil {
		return fmt.Errorf("%w: version: %w", ErrInvalidManifest, err)
	}
	if err := m.Author.Validate(); err != nil {
		return fmt.Errorf("%w: author: %w", ErrInvalidManifest, err)
	}
	for i, contributor := range m.Contributors {
		if err := contributor.Validate(); err != nil {
			return fmt.Errorf("%w: contributors[%d]: %w", ErrInvalidManifest, i, err)
		}
	}
	if err := m.URI.Validate(); err != nil {
		return fmt.Errorf("%w: uri: %w", ErrInvalidManifest, err)
	}
	if m.URI.Scheme == SchemeOCI && m.URI.Ref == "" && m.URI.Digest == "" {
		return fmt.Errorf("%w: oci uri needs a tag or digest", ErrInvalidManifest)
	}
	if m.Digest != "" && !digestRe.MatchString(m.Digest) {
		return fmt.Errorf("%w: digest %q is not a sha256: digest", ErrInvalidManifest, m.Digest)
	}
	if m.MinEngine != "" {
		if _, err := semver.NewConstraint(m.MinEngine); err != nil {
			return fmt.Errorf("%w: bad min-engine %q: %w", ErrInvalidManifest, m.MinEngine, err)
		}
	}
	if m.ManifestHash != "" && !manifestHashRe.MatchString(m.ManifestHash) {
		return fmt.Errorf("%w: manifestHash %q is not a sha256: digest", ErrInvalidManifest, m.ManifestHash)
	}
	return nil
}

func canonical(version string) string {
	parsed, err := ParseStrict(version)
	if err != nil {
		return version
	}
	return parsed.String()
}

func validResourceName(name string) bool {
	if !nameRe.MatchString(name) {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return false
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
