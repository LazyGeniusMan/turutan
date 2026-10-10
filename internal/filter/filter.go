// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func ToSlash(path string) string {
	return filepath.ToSlash(path)
}

func Match(pattern, name string) (bool, error) {
	matched, err := doublestar.Match(ToSlash(pattern), ToSlash(name))
	if err != nil {
		return false, fmt.Errorf("matching pattern %q against %q: %w", pattern, name, err)
	}
	return matched, nil
}

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

func ValidateGlobs(patterns []string) error {
	for _, pattern := range patterns {
		if err := ValidatePattern(pattern); err != nil {
			return err
		}
	}
	return nil
}

func IsSpecialFile(info fs.FileInfo) bool {
	mode := info.Mode()
	return !mode.IsRegular() && !info.IsDir() && mode&fs.ModeSymlink == 0
}

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
