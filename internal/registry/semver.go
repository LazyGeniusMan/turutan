// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Masterminds/semver/v3"
)

// ErrInvalidVersion is matched with errors.Is when version text fails validation.
// ~keep
var ErrInvalidVersion = errors.New("invalid version")

// ParseStrict parses raw text as a strict semantic version, rejecting
// ranges, constraints and loose forms.
// ~keep
func ParseStrict(raw string) (*semver.Version, error) {
	version, err := semver.StrictNewVersion(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a strict semantic version: %w", ErrInvalidVersion, raw, err)
	}
	return version, nil
}

// Compare compares two strict semantic versions, returning -1, 0 or +1.
// ~keep
func Compare(first, second string) (int, error) {
	left, err := ParseStrict(first)
	if err != nil {
		return 0, err
	}
	right, err := ParseStrict(second)
	if err != nil {
		return 0, err
	}
	return left.Compare(right), nil
}

// SortDesc returns the canonical forms of versions sorted newest first.
// It fails on the first entry that is not a strict semantic version.
// ~keep
func SortDesc(versions []string) ([]string, error) {
	parsed := make([]*semver.Version, 0, len(versions))
	for _, raw := range versions {
		version, err := ParseStrict(raw)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, version)
	}
	slices.SortFunc(parsed, func(left, right *semver.Version) int {
		return right.Compare(left)
	})
	out := make([]string, 0, len(parsed))
	for _, version := range parsed {
		out = append(out, version.String())
	}
	return out, nil
}

// IsNewestFirst reports whether versions are strict semantic versions in
// strictly descending order with no duplicates.
// ~keep
func IsNewestFirst(versions []string) bool {
	for i := 1; i < len(versions); i++ {
		cmp, err := Compare(versions[i-1], versions[i])
		if err != nil || cmp <= 0 {
			return false
		}
	}
	if len(versions) > 0 {
		if _, err := ParseStrict(versions[len(versions)-1]); err != nil {
			return false
		}
	}
	return true
}

// BumpMajor returns version with the major component incremented and the
// minor, patch, prerelease and metadata components cleared.
// ~keep
func BumpMajor(version string) (string, error) {
	return bump(version, func(parsed *semver.Version) *semver.Version {
		return semver.New(parsed.Major()+1, 0, 0, "", "")
	})
}

// BumpMinor returns version with the minor component incremented and the
// patch, prerelease and metadata components cleared.
// ~keep
func BumpMinor(version string) (string, error) {
	return bump(version, func(parsed *semver.Version) *semver.Version {
		return semver.New(parsed.Major(), parsed.Minor()+1, 0, "", "")
	})
}

// BumpPatch returns version with the patch component incremented and the
// prerelease and metadata components cleared.
// ~keep
func BumpPatch(version string) (string, error) {
	return bump(version, func(parsed *semver.Version) *semver.Version {
		return semver.New(parsed.Major(), parsed.Minor(), parsed.Patch()+1, "", "")
	})
}

func bump(version string, next func(*semver.Version) *semver.Version) (string, error) {
	parsed, err := ParseStrict(version)
	if err != nil {
		return "", err
	}
	return next(parsed).String(), nil
}
