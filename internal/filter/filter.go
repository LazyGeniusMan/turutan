// SPDX-License-Identifier: Apache-2.0

// Package filter matches ignore/preserve globs and joins paths safely.
// Glob matching uses doublestar v4 semantics; all path joins are scoped to
// a root directory so template entries cannot escape it.
package filter

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ToSlash normalizes path to forward slashes before glob matching so
// patterns behave identically on Windows and Unix.
func ToSlash(path string) string {
	return filepath.ToSlash(path)
}

// Match reports whether name matches the doublestar glob pattern after
// slash normalization. Pattern and name are slash-normalized first so
// Windows paths match the same patterns as Unix paths.
func Match(pattern, name string) (bool, error) {
	matched, err := doublestar.Match(ToSlash(pattern), ToSlash(name))
	if err != nil {
		return false, fmt.Errorf("matching pattern %q against %q: %w", pattern, name, err)
	}
	return matched, nil
}

// MatchAny reports whether name matches any of the doublestar glob
// patterns. It returns false when patterns is empty.
func MatchAny(patterns []string, name string) (bool, error) {
	for _, pattern := range patterns {
		matched, err := Match(pattern, name)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// SecureJoin joins user-controlled path p onto root using lexical
// containment only (no filesystem access): absolute paths and any ".."
// segment escaping root are refused. It is the fallback when os.Root is
// unavailable; prefer SafeJoin plus an os.Root read/write (see
// ReadFileWithinRoot) so symlinks resolving outside root are refused too.
func SecureJoin(root, p string) (string, error) {
	if p == "" || p == "." {
		return root, nil
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("joining path %q: absolute paths are not allowed", p)
	}
	if !filepath.IsLocal(p) {
		return "", fmt.Errorf("joining path %q: path escapes its root", p)
	}
	joined := filepath.Join(root, p)
	cleanRoot := filepath.Clean(root)
	rel, err := filepath.Rel(cleanRoot, joined)
	if err != nil {
		return "", fmt.Errorf("relativizing path %q: %w", p, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root %q", p, root)
	}
	return joined, nil
}

// SafeJoin joins user-controlled path p onto root and refuses anything
// that would escape root: absolute paths, non-local paths such as "..",
// and joins whose cleaned result leaves root. Callers must use it for
// every template entry before touching the filesystem.
func SafeJoin(root, p string) (string, error) {
	joined, err := SecureJoin(root, p)
	if err != nil {
		return "", err
	}
	if err := EnsureWithinRoot(root, joined); err != nil {
		return "", err
	}
	return joined, nil
}

// ValidatePattern rejects ignore/preserve/skip globs that could reach
// outside the subpath root: absolute patterns and any ".." segment.
// Glob matching itself is always scoped per file inside the root, so a
// validated pattern can only select files under it.
func ValidatePattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("invalid glob %q: pattern is empty", pattern)
	}
	slash := ToSlash(pattern)
	if strings.HasPrefix(slash, "/") {
		return fmt.Errorf("invalid glob %q: absolute patterns are not allowed", pattern)
	}
	for segment := range strings.SplitSeq(slash, "/") {
		if segment == ".." {
			return fmt.Errorf("invalid glob %q: \"..\" escapes its root", pattern)
		}
	}
	return nil
}

// ValidateGlobs validates every pattern in the list (see ValidatePattern).
func ValidateGlobs(patterns []string) error {
	for _, pattern := range patterns {
		if err := ValidatePattern(pattern); err != nil {
			return err
		}
	}
	return nil
}

// IsSpecialFile reports whether info describes a special file that must
// never be copied, rendered or merged: fifos, sockets and devices. Callers
// copy regular files, create directories and resolve symlinks explicitly;
// everything else is skipped via this helper.
func IsSpecialFile(info fs.FileInfo) bool {
	mode := info.Mode()
	return !mode.IsRegular() && !info.IsDir() && mode&fs.ModeSymlink == 0
}

// EnsureWithinRoot reports an error when target (typically a
// symlink-resolved path) lies outside root. Use it after
// filepath.EvalSymlinks to refuse symlinks escaping the template root.
func EnsureWithinRoot(root, target string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving root %q: %w", root, err)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolving path %q: %w", target, err)
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return fmt.Errorf("relativizing path %q: %w", target, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q escapes root %q", target, root)
	}
	return nil
}
