// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFSBackendRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	archive := filepath.Join(t.TempDir(), "releases", "1.0.0.tar.gz")
	uri := mustParseURI(t, "fs:"+filepath.ToSlash(archive))

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
	if info, err := os.Stat(archive); err != nil || info.IsDir() {
		t.Fatalf("archive %q missing or not a file: %v", archive, err)
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
	uri := mustParseURI(t, "templates/demo/releases/1.0.0.tar.gz")
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
	relative := mustParseURI(t, "templates/demo/releases/1.0.0.tar.gz")
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

func TestFSBackendSkipsReleaseMeta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	if err := os.MkdirAll(filepath.Join(src, "manifest"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest", "1.0.0.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "releases"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "releases", "1.0.0.tar.gz"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "out.tar.gz")
	uri := mustParseURI(t, "fs:"+filepath.ToSlash(archive))
	backend := NewFSBackend()
	if _, err := backend.Bundle(ctx, src, uri, AuthConfig{}); err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	for _, meta := range []string{"manifest", "releases"} {
		if _, err := os.Stat(filepath.Join(dest, meta)); !os.IsNotExist(err) {
			t.Fatalf("fetched bundle contains %q", meta)
		}
	}
}
