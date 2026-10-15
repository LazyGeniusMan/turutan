// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"strings"
	"testing"
)

func envGetter(vars map[string]string) func(string) string {
	return func(key string) string {
		return vars[key]
	}
}

func TestResolveAuth(t *testing.T) {
	t.Parallel()
	s3URI, err := ParseURI("s3://mybucket/k.tgz")
	if err != nil {
		t.Fatalf("ParseURI error: %v", err)
	}
	gitURI, err := ParseURI("git+https://github.com/org/r.git?ref=main")
	if err != nil {
		t.Fatalf("ParseURI error: %v", err)
	}
	ociURI, err := ParseURI("oci://ghcr.io/org/repo:1.0.0")
	if err != nil {
		t.Fatalf("ParseURI error: %v", err)
	}
	tests := []struct {
		name     string
		registry string
		uri      URI
		env      map[string]string
		expected AuthConfig
	}{
		{
			name:     "no credentials",
			registry: "example",
			uri:      ociURI,
			env:      map[string]string{},
			expected: AuthConfig{Kind: AuthKindNone},
		},
		{
			name:     "per registry basic",
			registry: "example",
			uri:      ociURI,
			env: map[string]string{
				"TURUTAN_REGISTRY_EXAMPLE_USER": "alice",
				"TURUTAN_REGISTRY_EXAMPLE_PASS": "s3cret", // betterleaks:allow (test fixture, not a credential)
			},
			expected: AuthConfig{Kind: AuthKindBasic, Username: "alice", Password: "s3cret"}, // betterleaks:allow (test fixture, not a credential)
		},
		{
			name:     "per registry token",
			registry: "example",
			uri:      ociURI,
			env:      map[string]string{"TURUTAN_REGISTRY_EXAMPLE_TOKEN": "tok"},
			expected: AuthConfig{Kind: AuthKindToken, Token: "tok"},
		},
		{
			name:     "per registry beats generic",
			registry: "example",
			uri:      ociURI,
			env: map[string]string{
				"TURUTAN_REGISTRY_EXAMPLE_TOKEN": "rtok",
				"TURUTAN_OCI_TOKEN":              "gtok",
			},
			expected: AuthConfig{Kind: AuthKindToken, Token: "rtok"},
		},
		{
			name:     "s3 generic env",
			registry: "data",
			uri:      s3URI,
			env: map[string]string{
				"AWS_ACCESS_KEY_ID":     "akid",
				"AWS_SECRET_ACCESS_KEY": "secret", // betterleaks:allow (test fixture, not a credential)
				"AWS_ENDPOINT_URL":      "https://minio.example.com",
			},
			expected: AuthConfig{
				Kind:     AuthKindS3,
				Username: "akid",
				Password: "secret", // betterleaks:allow (test fixture, not a credential)
				Endpoint: "https://minio.example.com",
			},
		},
		{
			name:     "git github token",
			registry: "code",
			uri:      gitURI,
			env:      map[string]string{"GITHUB_TOKEN": "gh"},
			expected: AuthConfig{Kind: AuthKindToken, Username: "x-access-token", Token: "gh"},
		},
		{
			name:     "oci generic token",
			registry: "images",
			uri:      ociURI,
			env:      map[string]string{"TURUTAN_OCI_TOKEN": "ocitok"},
			expected: AuthConfig{Kind: AuthKindToken, Token: "ocitok"},
		},
		{
			name:     "oci generic basic",
			registry: "images",
			uri:      ociURI,
			env: map[string]string{
				"TURUTAN_OCI_USER":     "bot",
				"TURUTAN_OCI_PASSWORD": "pw",
			},
			expected: AuthConfig{Kind: AuthKindBasic, Username: "bot", Password: "pw"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveAuthWithEnv(tt.registry, tt.uri, envGetter(tt.env))
			if got != tt.expected {
				t.Errorf("ResolveAuthWithEnv = %+v, want %+v", got.Redacted(), tt.expected.Redacted())
			}
		})
	}
}

func TestAuthNeverLogsSecrets(t *testing.T) {
	t.Parallel()
	cfg := AuthConfig{
		Kind:     AuthKindBasic,
		Username: "alice",
		Password: "s3cret", // betterleaks:allow (test fixture, not a credential)
		Token:    "tok",
		SSHKey:   "key",
	}
	rendered := cfg.String()
	for _, secret := range []string{"s3cret", "tok", "key"} { // betterleaks:allow (test fixture, not a credential)
		if strings.Contains(rendered, secret) {
			t.Errorf("String() leaks secret %q in %q", secret, rendered)
		}
	}
	if !strings.Contains(rendered, "alice") {
		t.Errorf("String() = %q, want username retained", rendered)
	}
	redacted := cfg.Redacted()
	if redacted.Password == "s3cret" || redacted.Token == "tok" || redacted.SSHKey == "key" {
		t.Errorf("Redacted() keeps secrets: %+v", redacted)
	}
}
