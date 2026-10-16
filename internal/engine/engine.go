// SPDX-License-Identifier: Apache-2.0

// Package engine owns the turutan engine version helpers.
// ~keep
package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Version is the turutan binary version that stamps generated files.
// It defaults to dev for local builds and is overwritten via ldflags
// (-X github.com/LazyGeniusMan/turutan/internal/engine.Version=...).
// ~keep
var Version = "dev"

// ErrEngineTooOld is matched with errors.Is when the running engine is
// older than the stored engine_version.
// ~keep
var ErrEngineTooOld = errors.New("engine version too old")

// Normalize strips the turutan/ and v prefixes callers historically used,
// returning the bare version for semver comparison.
// ~keep
func Normalize(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "turutan/")
	trimmed = strings.TrimPrefix(trimmed, "v")
	return strings.TrimSpace(trimmed)
}

// Current returns the normalized running binary version.
// ~keep
func Current() string {
	normalized := Normalize(Version)
	if normalized == "" {
		return "dev"
	}
	return normalized
}

// CurrentFrom prefers an explicit engine string (scaffold Options.Engine,
// CLI --engine plumbing) falling back to the binary Version.
// ~keep
func CurrentFrom(explicit string) string {
	if strings.TrimSpace(explicit) == "" {
		return Current()
	}
	normalized := Normalize(explicit)
	if normalized == "" {
		return Current()
	}
	return normalized
}

// IsDev reports whether a version string bypasses the gate (dev builds and
// unparseable placeholders never block).
// ~keep
func IsDev(raw string) bool {
	normalized := Normalize(raw)
	if normalized == "" || normalized == "dev" {
		return true
	}
	_, err := semver.NewVersion(normalized)
	return err != nil
}

// Check requires current >= stored via semver, returning ErrEngineTooOld
// otherwise. Empty or dev values on either side skip the gate so fresh
// files stamp without a stored floor and dev builds never block.
// ~keep
func Check(stored, current string) error {
	storedNorm := Normalize(stored)
	currentNorm := Normalize(current)
	if storedNorm == "" || currentNorm == "" {
		return nil
	}
	if storedNorm == "dev" || currentNorm == "dev" {
		return nil
	}
	storedVer, err := semver.NewVersion(storedNorm)
	if err != nil {
		return fmt.Errorf("%w: bad engine_version %q: %v", ErrEngineTooOld, stored, err)
	}
	currentVer, err := semver.NewVersion(currentNorm)
	if err != nil {
		return nil
	}
	if currentVer.LessThan(storedVer) {
		return fmt.Errorf(
			"%w: running engine %s is older than stored engine_version %s: upgrade turutan",
			ErrEngineTooOld, currentNorm, storedNorm)
	}
	return nil
}

// Bump validates stored against current then returns current, the value
// every modify stamps.
// ~keep
func Bump(stored, current string) (string, error) {
	if err := Check(stored, current); err != nil {
		return "", err
	}
	normalized := Normalize(current)
	if normalized == "" {
		normalized = Current()
	}
	return normalized, nil
}

// ValidateField reports whether an engine_version field value is well-formed
// (bare semver or dev).
// ~keep
func ValidateField(raw string) error {
	normalized := Normalize(raw)
	if normalized == "" {
		return fmt.Errorf("%w: engine_version is empty", ErrEngineTooOld)
	}
	if normalized == "dev" {
		return nil
	}
	if _, err := semver.NewVersion(normalized); err != nil {
		return fmt.Errorf("%w: bad engine_version %q: %v", ErrEngineTooOld, raw, err)
	}
	return nil
}
