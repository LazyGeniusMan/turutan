// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"testing"

	"oras.land/oras-go/v2/content/memory"
)

func TestOCIBackendRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	shared := memory.New()
	backend := NewOCIBackend(func(context.Context, URI, AuthConfig) (OCITarget, error) {
		return shared, nil
	})
	if backend.Scheme() != SchemeOCI {
		t.Fatalf("Scheme() = %q, want oci", backend.Scheme())
	}
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	uri := mustParseURI(t, "oci://example.com/org/templates/demo:1.0.0")
	digest, err := backend.Bundle(ctx, src, uri, AuthConfig{})
	if err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	if !digestPattern.MatchString(digest) {
		t.Fatalf("digest %q is not sha256:<hex>", digest)
	}
	dest := t.TempDir()
	if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	assertSameTree(t, src, dest)
	if got, err := DigestDir(dest); err != nil || got != digest {
		t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, digest)
	}
}

func TestOCIBackendFetchByDigest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	shared := memory.New()
	backend := NewOCIBackend(func(context.Context, URI, AuthConfig) (OCITarget, error) {
		return shared, nil
	})
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	tagged := mustParseURI(t, "oci://example.com/org/templates/demo:2.0.0")
	if _, err := backend.Bundle(ctx, src, tagged, AuthConfig{}); err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	manifestDesc, err := shared.Resolve(ctx, "2.0.0")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if err := shared.Tag(ctx, manifestDesc, manifestDesc.Digest.String()); err != nil {
		t.Fatalf("Tag error: %v", err)
	}
	pinned := mustParseURI(t, "oci://example.com/org/templates/demo@"+manifestDesc.Digest.String())
	dest := t.TempDir()
	if err := backend.Fetch(ctx, pinned, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch by digest error: %v", err)
	}
	assertSameTree(t, src, dest)
}

func TestOCIBackendBundleNeedsTag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	shared := memory.New()
	backend := NewOCIBackend(func(context.Context, URI, AuthConfig) (OCITarget, error) {
		return shared, nil
	})
	uri := mustParseURI(t, "oci://example.com/org/templates/demo")
	if _, err := backend.Bundle(ctx, t.TempDir(), uri, AuthConfig{}); err == nil {
		t.Fatalf("Bundle without a tag succeeded, want error")
	}
}
