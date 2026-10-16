// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
)

type fakeS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeS3Client() *fakeS3Client {
	return &fakeS3Client{objects: map[string][]byte{}}
}

func (f *fakeS3Client) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.objects[bucket+"/"+key]
	if !ok {
		return nil, fmt.Errorf("no such object %s/%s", bucket, key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeS3Client) PutObject(_ context.Context, bucket, key string, body io.Reader, _ int64) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[bucket+"/"+key] = data
	return nil
}

func TestS3BackendRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		key  string
	}{
		{"key prefix", "s3://mybucket/templates/demo/", "templates/demo/bundle.tar.gz"},
		{"verbatim key", "s3://mybucket/templates/demo.tar.gz", "templates/demo.tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			fake := newFakeS3Client()
			backend := NewS3Backend(func(URI, AuthConfig) (S3Client, error) { return fake, nil })
			if backend.Scheme() != SchemeS3 {
				t.Fatalf("Scheme() = %q, want s3", backend.Scheme())
			}
			src := t.TempDir()
			writeFixtureTemplate(t, src)
			uri := mustParseURI(t, tt.raw)
			digest, err := backend.Bundle(ctx, src, uri, AuthConfig{})
			if err != nil {
				t.Fatalf("Bundle error: %v", err)
			}
			if !digestPattern.MatchString(digest) {
				t.Fatalf("digest %q is not sha256:<hex>", digest)
			}
			if _, ok := fake.objects["mybucket/"+tt.key]; !ok {
				t.Fatalf("object %q was not stored", tt.key)
			}
			dest := t.TempDir()
			if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
				t.Fatalf("Fetch error: %v", err)
			}
			assertSameTree(t, src, dest)
			if got, err := DigestDir(dest); err != nil || got != digest {
				t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, digest)
			}
		})
	}
}

func TestS3BackendMissingObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := newFakeS3Client()
	backend := NewS3Backend(func(URI, AuthConfig) (S3Client, error) { return fake, nil })
	uri := mustParseURI(t, "s3://mybucket/absent.tar.gz")
	if err := backend.Fetch(ctx, uri, AuthConfig{}, t.TempDir()); err == nil {
		t.Fatalf("Fetch of a missing object succeeded, want error")
	}
}

func TestDefaultS3ClientFactory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		uri       string
		auth      AuthConfig
		wantError bool
	}{
		{"uri endpoint", "s3://b/k.tgz?endpoint=https://minio.example.com:9000", AuthConfig{}, false},
		{"auth endpoint", "s3://b/k.tgz", AuthConfig{Endpoint: "http://minio.example.com:9000"}, false},
		{"default endpoint", "s3://b/k.tgz", AuthConfig{}, false},
		{
			"with credentials", "s3://b/k.tgz",
			AuthConfig{Kind: AuthKindS3, Username: "akid", Password: "secret"}, // betterleaks:allow
			false,
		}, // betterleaks:allow (test fixture)
		{"bad endpoint", "s3://b/k.tgz", AuthConfig{Endpoint: "ftp://x"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uri := mustParseURI(t, tt.uri)
			client, err := DefaultS3ClientFactory(uri, tt.auth)
			if tt.wantError {
				if err == nil {
					t.Fatalf("DefaultS3ClientFactory succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultS3ClientFactory error: %v", err)
			}
			if client == nil {
				t.Fatalf("DefaultS3ClientFactory returned nil client")
			}
		})
	}
}
