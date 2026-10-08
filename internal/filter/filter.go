// SPDX-License-Identifier: Apache-2.0

// Package filter matches ignore/preserve globs and joins paths safely.
// Doublestar matching and os.Root hardening arrive in M2+; M0 provides the
// slash-normalization stub so call sites normalize Windows paths early.
package filter

import (
	"path"
	"path/filepath"
)

// ToSlash normalizes path to forward slashes before glob matching.
func ToSlash(path string) string {
	return filepath.ToSlash(path)
}

// Match reports whether path matches the glob pattern after slash
// normalization. It uses stdlib semantics until doublestar lands in M2.
func Match(pattern, name string) (bool, error) {
	return path.Match(ToSlash(pattern), ToSlash(name))
}
