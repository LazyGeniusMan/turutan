// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFSBackendRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	store := filepath.Join(t.TempDir(), "store")
	uri := mustParseURI(t, "fs:"+store)

	backend := NewFSBackend()
	if backend.Scheme() != SchemeFS {
		t.Fatalf("Scheme() = %q, want fs", backend.Scheme())
	}
	digest, err := backend.Bundle(ctx, src, uri, AuthConfig{})
	if err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	if !digestPattern.MatchString(digest) {
		t.Fatalf("digest %q is not sha256:<hex>", digest)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	assertSameTree(t, src, dest)
	if got, err := DigestDir(dest); err != nil || got != digest {
		t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, digest)
	}
}

func TestFSBackendRelativeRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uri := mustParseURI(t, "templates/demo")
	backend := NewFSBackend()
	if _, err := backend.Bundle(ctx, t.TempDir(), uri, AuthConfig{}); err == nil {
		t.Fatalf("Bundle accepted a relative uri")
	}
	if err := backend.Fetch(ctx, uri, AuthConfig{}, t.TempDir()); err == nil {
		t.Fatalf("Fetch accepted a relative uri")
	}
}

func TestFSBackendResolvedRelative(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	base := t.TempDir()
	relative := mustParseURI(t, "templates/demo")
	uri, err := relative.ResolveRelativeTo(base)
	if err != nil {
		t.Fatalf("ResolveRelativeTo error: %v", err)
	}
	backend := NewFSBackend()
	digest, err := backend.Bundle(ctx, src, uri, AuthConfig{})
	if err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	assertSameTree(t, src, dest)
	if got, err := DigestDir(dest); err != nil || got != digest {
		t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, digest)
	}
}
