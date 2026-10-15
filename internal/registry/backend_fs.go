// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"fmt"
	"os"
)

// FSBackend copies template trees to and from local directories.
// ~keep
type FSBackend struct{}

// NewFSBackend returns an FSBackend with no configuration.
// ~keep
func NewFSBackend() *FSBackend { return &FSBackend{} }

// Scheme reports the fs scheme this backend handles.
// ~keep
func (b *FSBackend) Scheme() Scheme { return SchemeFS }

// Bundle digests srcDir and copies its tree to the local uri path.
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
	digest, err := DigestDir(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, err)
	}
	if err := CopyTree(srcDir, uri.Path); err != nil {
		return "", fmt.Errorf("bundling %q: %w", uri.Path, err)
	}
	return digest, nil
}

// Fetch copies the local uri tree into destDir.
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
	info, err := os.Stat(uri.Path)
	if err != nil {
		return fmt.Errorf("fetching %q: %w", uri.Path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("fetching %q: source is not a directory", uri.Path)
	}
	if err := CopyTree(uri.Path, destDir); err != nil {
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
