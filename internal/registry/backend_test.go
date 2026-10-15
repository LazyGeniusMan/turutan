// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"testing"
)

func TestBackendFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		scheme Scheme
		want   Scheme
	}{
		{SchemeFS, SchemeFS},
		{SchemeHTTP, SchemeHTTP},
		{SchemeHTTPS, SchemeHTTPS},
		{SchemeS3, SchemeS3},
		{SchemeGit, SchemeGit},
		{SchemeOCI, SchemeOCI},
	}
	for _, tt := range tests {
		t.Run(string(tt.scheme), func(t *testing.T) {
			t.Parallel()
			backend, err := BackendFor(tt.scheme)
			if err != nil {
				t.Fatalf("BackendFor(%q) error: %v", tt.scheme, err)
			}
			if backend.Scheme() != tt.want {
				t.Fatalf("Scheme() = %q, want %q", backend.Scheme(), tt.want)
			}
		})
	}
}

func TestBackendForUnknown(t *testing.T) {
	t.Parallel()
	if _, err := BackendFor("ftp"); err == nil {
		t.Fatalf("BackendFor(ftp) succeeded, want error")
	}
}

func TestBackendsRejectForeignScheme(t *testing.T) {
	t.Parallel()
	fsURI := mustParseURI(t, "fs:/tmp/x")
	backends := map[string]Backend{
		"http": NewHTTPBackend(nil, SchemeHTTPS),
		"s3":   NewS3Backend(nil),
		"git":  NewGitBackend(nil),
		"oci":  NewOCIBackend(nil),
	}
	for name, backend := range backends {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if _, err := backend.Bundle(ctx, t.TempDir(), fsURI, AuthConfig{}); err == nil {
				t.Fatalf("%s Bundle accepted an fs uri", name)
			}
			if err := backend.Fetch(ctx, fsURI, AuthConfig{}, t.TempDir()); err == nil {
				t.Fatalf("%s Fetch accepted an fs uri", name)
			}
		})
	}
}

func mustParseURI(t *testing.T, raw string) URI {
	t.Helper()
	uri, err := ParseURI(raw)
	if err != nil {
		t.Fatalf("ParseURI(%q) error: %v", raw, err)
	}
	return uri
}
