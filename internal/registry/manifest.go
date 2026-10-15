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

// TemplateManifest describes one template release line: the newest release
// plus its history. Extra carries forward-compatible fields unknown to this
// engine version.
// ~keep
type TemplateManifest struct {
	Name         string         `yaml:"name"`
	Version      string         `yaml:"version"`
	Author       Author         `yaml:"author"`
	Contributors []Author       `yaml:"contributors,omitempty"`
	URI          URI            `yaml:"uri"`
	Releases     []string       `yaml:"releases,omitempty"`
	Digest       string         `yaml:"digest,omitempty"`
	MinEngine    string         `yaml:"min-engine,omitempty"`
	ManifestHash string         `yaml:"manifestHash,omitempty"`
	License      string         `yaml:"license,omitempty"`
	Extra        map[string]any `yaml:",inline"`
}

// Validate reports whether the manifest is internally consistent: names,
// strict semver versions with newest-first releases, author formats, URI,
// digest, engine constraint and manifest hash.
// ~keep
func (m *TemplateManifest) Validate() error {
	if m == nil {
		return fmt.Errorf("%w: manifest is nil", ErrInvalidManifest)
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
	if err := validateReleases(m.Version, m.Releases); err != nil {
		return err
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

func validateReleases(version string, releases []string) error {
	for i, release := range releases {
		if _, err := ParseStrict(release); err != nil {
			return fmt.Errorf("%w: releases[%d]: %w", ErrInvalidManifest, i, err)
		}
	}
	if !IsNewestFirst(releases) && len(releases) > 0 {
		return fmt.Errorf("%w: releases must be strict semver sorted newest-first", ErrInvalidManifest)
	}
	if len(releases) > 0 && canonical(releases[0]) != canonical(version) {
		return fmt.Errorf(
			"%w: releases[0] %q must equal version %q",
			ErrInvalidManifest, releases[0], version)
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
