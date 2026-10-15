// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"errors"
	"testing"
)

func TestParseURI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		expected    URI
		expectedStr string
		relative    bool
		wantErr     bool
	}{
		{
			name:        "absolute fs",
			input:       "fs:/srv/templates",
			expected:    URI{Scheme: SchemeFS, Path: "/srv/templates"},
			expectedStr: "fs:/srv/templates",
		},
		{
			name:        "fs double slash form",
			input:       "fs:///srv/templates",
			expected:    URI{Scheme: SchemeFS, Path: "/srv/templates"},
			expectedStr: "fs:/srv/templates",
		},
		{
			name:        "relative fs bare path",
			input:       "templates/default",
			expected:    URI{Scheme: SchemeFS, Path: "templates/default"},
			expectedStr: "templates/default",
			relative:    true,
		},
		{
			name:  "https",
			input: "https://example.com/templates/default",
			expected: URI{
				Scheme: SchemeHTTPS,
				Host:   "example.com",
				Path:   "/templates/default",
			},
			expectedStr: "https://example.com/templates/default",
		},
		{
			name:  "s3 basic",
			input: "s3://mybucket/templates/default.tgz",
			expected: URI{
				Scheme: SchemeS3,
				Bucket: "mybucket",
				Path:   "templates/default.tgz",
			},
			expectedStr: "s3://mybucket/templates/default.tgz",
		},
		{
			name:  "s3 with endpoint",
			input: "s3://mybucket/templates/a.tgz?endpoint=https://minio.example.com:9000",
			expected: URI{
				Scheme:   SchemeS3,
				Bucket:   "mybucket",
				Path:     "templates/a.tgz",
				Endpoint: "https://minio.example.com:9000",
			},
			expectedStr: "s3://mybucket/templates/a.tgz?endpoint=https://minio.example.com:9000",
		},
		{
			name:  "git native",
			input: "git://github.com/org/repo.git?ref=v1.0.0",
			expected: URI{
				Scheme:    SchemeGit,
				Transport: "git",
				Host:      "github.com",
				Path:      "org/repo.git",
				Ref:       "v1.0.0",
			},
			expectedStr: "git://github.com/org/repo.git?ref=v1.0.0",
		},
		{
			name:  "git https with subpath",
			input: "git+https://github.com/org/repo.git?ref=main&subpath=templates/default",
			expected: URI{
				Scheme:    SchemeGit,
				Transport: "https",
				Host:      "github.com",
				Path:      "org/repo.git",
				Ref:       "main",
				Subpath:   "templates/default",
			},
			expectedStr: "git+https://github.com/org/repo.git?ref=main&subpath=templates/default",
		},
		{
			name:  "oci tag",
			input: "oci://ghcr.io/lazygeniusman/turutan/templates/default:1.2.0",
			expected: URI{
				Scheme: SchemeOCI,
				Host:   "ghcr.io",
				Path:   "lazygeniusman/turutan/templates/default",
				Ref:    "1.2.0",
			},
			expectedStr: "oci://ghcr.io/lazygeniusman/turutan/templates/default:1.2.0",
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			input:   "ftp://example.com/x",
			wantErr: true,
		},
		{
			name:    "s3 missing key",
			input:   "s3://mybucket",
			wantErr: true,
		},
		{
			name:    "s3 bad endpoint",
			input:   "s3://b/k?endpoint=notaurl",
			wantErr: true,
		},
		{
			name:    "git missing ref",
			input:   "git://github.com/org/repo.git",
			wantErr: true,
		},
		{
			name:    "git escaping subpath",
			input:   "git://github.com/org/r.git?ref=main&subpath=../escape",
			wantErr: true,
		},
		{
			name:  "oci repo root without ref",
			input: "oci://ghcr.io/org/repo",
			expected: URI{
				Scheme: SchemeOCI,
				Host:   "ghcr.io",
				Path:   "org/repo",
			},
			expectedStr: "oci://ghcr.io/org/repo",
		},
		{
			name:    "oci non semver tag",
			input:   "oci://ghcr.io/org/repo:latest",
			wantErr: true,
		},
		{
			name:    "oci missing namespace",
			input:   "oci://ghcr.io/repo:1.0.0",
			wantErr: true,
		},
		{
			name:    "embedded password rejected",
			input:   "https://user:secret@example.com/x", // #nosec G101 -- test-only URL asserting credential rejection, not a real credential
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseURI(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseURI(%q) = %+v, want error", tt.input, got)
				}
				if !errors.Is(err, ErrInvalidURI) {
					t.Errorf("ParseURI(%q) error = %v, want ErrInvalidURI", tt.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseURI(%q) error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseURI(%q) = %+v, want %+v", tt.input, got, tt.expected)
			}
			if got.IsRelative() != tt.relative {
				t.Errorf("IsRelative() = %v, want %v", got.IsRelative(), tt.relative)
			}
			if got.String() != tt.expectedStr {
				t.Errorf("String() = %q, want %q", got.String(), tt.expectedStr)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("Validate() error: %v", err)
			}
		})
	}
}

func TestURIResolveRelativeTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		base     string
		expected string
		wantErr  bool
	}{
		{
			name:     "fs against dir",
			raw:      "templates/default",
			base:     "/srv/registry",
			expected: "fs:/srv/registry/templates/default",
		},
		{
			name:     "fs against relative dir",
			raw:      "a/b",
			base:     "r",
			expected: "r/a/b",
		},
		{
			name:     "relative against http base",
			raw:      "templates/default",
			base:     "https://example.com/registry",
			expected: "https://example.com/templates/default",
		},
		{
			name:     "absolute passes through",
			raw:      "oci://ghcr.io/org/repo:1.0.0",
			base:     "/srv/registry",
			expected: "oci://ghcr.io/org/repo:1.0.0",
		},
		{
			name:    "empty base",
			raw:     "templates/default",
			base:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			uri, err := ParseURI(tt.raw)
			if err != nil {
				t.Fatalf("ParseURI error: %v", err)
			}
			got, err := uri.ResolveRelativeTo(tt.base)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveRelativeTo(%q) = %v, want error", tt.base, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveRelativeTo error: %v", err)
			}
			if got.String() != tt.expected {
				t.Errorf("resolved = %q, want %q", got.String(), tt.expected)
			}
		})
	}
}

func TestURIS3Parts(t *testing.T) {
	t.Parallel()
	uri, err := ParseURI("s3://mybucket/k.tgz?endpoint=https://minio.example.com")
	if err != nil {
		t.Fatalf("ParseURI error: %v", err)
	}
	endpoint, bucket, key := uri.S3Parts()
	if endpoint != "https://minio.example.com" || bucket != "mybucket" || key != "k.tgz" {
		t.Errorf("S3Parts() = %q %q %q", endpoint, bucket, key)
	}
}
