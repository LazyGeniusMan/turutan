// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"fmt"
	"os"
)

type FSBackend struct{}

func NewFSBackend() *FSBackend { return &FSBackend{} }

func (b *FSBackend) Scheme() Scheme { return SchemeFS }

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
