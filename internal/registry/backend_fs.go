// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// FSBackend stores template bundles as gzip-tar archives in local files.
// ~keep
type FSBackend struct{}

// NewFSBackend returns an FSBackend with no configuration.
// ~keep
func NewFSBackend() *FSBackend { return &FSBackend{} }

// Scheme reports the fs scheme this backend handles.
// ~keep
func (b *FSBackend) Scheme() Scheme { return SchemeFS }

// Bundle archives srcDir and writes the bundle bytes to the local uri path
// (an archive file such as templates/<name>/releases/<v>.tar.gz).
// ~keep
func (b *FSBackend) Bundle(ctx context.Context, srcDir string, uri URI, _ AuthConfig) (string, error) {
	if err := requireScheme(uri, SchemeFS); err != nil {
		return "", err
	}
	if uri.IsRelative() {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, ErrRelativeURI)
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, err)
	}
	info, err := os.Stat(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %q: reading source: %w", uri.Path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("bundling %q: source is not a directory", uri.Path)
	}
	blob, digest, err := BundleBytes(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, err)
	}
	if err := os.MkdirAll(filepath.Dir(uri.Path), 0o750); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, err)
	}
	if err := os.WriteFile(uri.Path, blob, 0o600); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, err)
	}
	return digest, nil
}

// Fetch extracts the local uri archive file into destDir.
// ~keep
func (b *FSBackend) Fetch(ctx context.Context, uri URI, _ AuthConfig, destDir string) error {
	if err := requireScheme(uri, SchemeFS); err != nil {
		return err
	}
	if uri.IsRelative() {
		return fmt.Errorf("fetching %q: %w", uri.Path, ErrRelativeURI)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.Path, err)
	}
	data, err := os.ReadFile(uri.Path) // #nosec G304 -- uri.Path is the caller-supplied absolute archive file
	if err != nil {
		return fmt.Errorf("fetching %q: %w", uri.Path, err)
	}
	if err := ExtractBundle(bytes.NewReader(data), destDir); err != nil {
		return fmt.Errorf("fetching %q: %w", uri.Path, err)
	}
	return nil
}

func requireScheme(uri URI, want Scheme) error {
	if err := uri.Validate(); err != nil {
		return err
	}
	if uri.Scheme != want {
		return fmt.Errorf("backend for %q cannot handle %q uris", want, uri.Scheme)
	}
	return nil
}
