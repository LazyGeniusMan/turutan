// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"os"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/git"
)

// Environment variables read by the auth resolver. Auth resolves only by
// URI scheme (one credential per scheme type): S3-compatible, git, OCI and
// HTTP(S) variables below. fs URIs are never authenticated.
// ~keep
const (
	EnvS3AccessKey  = "AWS_ACCESS_KEY_ID"     // #nosec G101 -- env var name, not a credential
	EnvS3SecretKey  = "AWS_SECRET_ACCESS_KEY" // #nosec G101 -- env var name, not a credential
	EnvS3Endpoint   = "AWS_ENDPOINT_URL"
	EnvS3Region     = "AWS_REGION"
	EnvOCIUser      = "TURUTAN_OCI_USER"      // #nosec G101 -- env var name, not a credential
	EnvOCIPassword  = "TURUTAN_OCI_PASSWORD"  // #nosec G101 -- env var name, not a credential (betterleaks:allow)
	EnvOCIToken     = "TURUTAN_OCI_TOKEN"     // #nosec G101 -- env var name, not a credential
	EnvHTTPUser     = "TURUTAN_HTTP_USER"     // #nosec G101 -- env var name, not a credential
	EnvHTTPPassword = "TURUTAN_HTTP_PASSWORD" // #nosec G101 -- env var name, not a credential (betterleaks:allow)
	EnvHTTPToken    = "TURUTAN_HTTP_TOKEN"    // #nosec G101 -- env var name, not a credential
)

// AuthKind describes how credentials in an AuthConfig authenticate.
// ~keep
type AuthKind string

// Supported auth kinds.
// ~keep
const (
	AuthKindNone  AuthKind = "none"
	AuthKindBasic AuthKind = "basic"
	AuthKindToken AuthKind = "token"
	AuthKindSSH   AuthKind = "ssh"
	AuthKindS3    AuthKind = "s3"
)

// AuthConfig carries credentials for one URI. Secrets never
// appear in String output, logs or YAML redaction helpers; load them only
// from environment variables or the config file.
// ~keep
type AuthConfig struct {
	Kind     AuthKind `yaml:"kind,omitempty"`
	Username string   `yaml:"username,omitempty"`
	Password string   `yaml:"password,omitempty"`
	Token    string   `yaml:"token,omitempty"`
	Endpoint string   `yaml:"endpoint,omitempty"`
	Region   string   `yaml:"region,omitempty"`
	SSHKey   string   `yaml:"sshKey,omitempty"`
}

// HasCredentials reports whether the config carries any secret material.
// ~keep
func (a AuthConfig) HasCredentials() bool {
	return a.Password != "" || a.Token != "" || a.SSHKey != ""
}

// BasicAuth returns the configured username and password when either is set.
// ~keep
func (a AuthConfig) BasicAuth() (username, password string, ok bool) {
	if a.Username == "" && a.Password == "" {
		return "", "", false
	}
	return a.Username, a.Password, true
}

// BearerToken returns the configured token for Authorization headers.
// ~keep
func (a AuthConfig) BearerToken() (string, bool) {
	if a.Token == "" {
		return "", false
	}
	return a.Token, true
}

// Redacted returns a copy with every secret replaced by a fixed mask.
// ~keep
func (a AuthConfig) Redacted() AuthConfig {
	out := a
	if out.Password != "" {
		out.Password = "***"
	}
	if out.Token != "" {
		out.Token = "***"
	}
	if out.SSHKey != "" {
		out.SSHKey = "***"
	}
	return out
}

// String renders a redacted summary that is safe to log.
// ~keep
func (a AuthConfig) String() string {
	redacted := a.Redacted()
	out := "kind=" + string(redacted.Kind)
	if redacted.Username != "" {
		out += " username=" + redacted.Username
	}
	if redacted.Endpoint != "" {
		out += " endpoint=" + redacted.Endpoint
	}
	if redacted.Region != "" {
		out += " region=" + redacted.Region
	}
	if a.HasCredentials() {
		out += " credentials=***"
	}
	return out
}

// ResolveAuth resolves auth for a content uri from the process
// environment. Auth resolves only by URI scheme (one credential per scheme
// type); fs URIs are never authenticated.
// ~keep
func ResolveAuth(uri URI) AuthConfig {
	return ResolveAuthWithEnv(uri, os.Getenv)
}

// ResolveAuthWithEnv resolves auth for a content uri using getenv, so
// callers can inject test environments. Auth resolves only by URI scheme.
// ~keep
func ResolveAuthWithEnv(uri URI, getenv func(string) string) AuthConfig {
	return schemeAuth(uri, getenv)
}

func schemeAuth(uri URI, getenv func(string) string) AuthConfig {
	switch uri.Scheme {
	case SchemeS3:
		return s3Auth(uri, getenv)
	case SchemeGit:
		return gitAuth(getenv)
	case SchemeOCI:
		return ociAuth(getenv)
	case SchemeHTTP, SchemeHTTPS:
		return httpAuth(getenv)
	default:
		return AuthConfig{Kind: AuthKindNone}
	}
}

func s3Auth(uri URI, getenv func(string) string) AuthConfig {
	cfg := AuthConfig{Kind: AuthKindNone, Endpoint: uri.Endpoint}
	if access := getenv(EnvS3AccessKey); access != "" {
		cfg.Username = access
	}
	if secret := getenv(EnvS3SecretKey); secret != "" {
		cfg.Password = secret
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = getenv(EnvS3Endpoint)
	}
	cfg.Region = getenv(EnvS3Region)
	if cfg.Username != "" && cfg.Password != "" {
		cfg.Kind = AuthKindS3
	}
	return cfg
}

func gitAuth(getenv func(string) string) AuthConfig {
	if token := getenv(git.EnvGitHubToken); token != "" {
		return AuthConfig{Kind: AuthKindToken, Username: "x-access-token", Token: token}
	}
	if key, ok := lookupNonEmpty(getenv, git.EnvSSHKey); ok {
		return AuthConfig{Kind: AuthKindSSH, SSHKey: key}
	}
	if password, ok := lookupNonEmpty(getenv, git.EnvSSHPassword); ok {
		return AuthConfig{Kind: AuthKindSSH, Password: password}
	}
	return AuthConfig{Kind: AuthKindNone}
}

func ociAuth(getenv func(string) string) AuthConfig {
	if token, ok := lookupNonEmpty(getenv, EnvOCIToken); ok {
		return AuthConfig{Kind: AuthKindToken, Token: token}
	}
	cfg := AuthConfig{Kind: AuthKindNone}
	if user, ok := lookupNonEmpty(getenv, EnvOCIUser); ok {
		cfg.Username = user
		cfg.Kind = AuthKindBasic
	}
	if password, ok := lookupNonEmpty(getenv, EnvOCIPassword); ok {
		cfg.Password = password
		cfg.Kind = AuthKindBasic
	}
	return cfg
}

func httpAuth(getenv func(string) string) AuthConfig {
	if token, ok := lookupNonEmpty(getenv, EnvHTTPToken); ok {
		return AuthConfig{Kind: AuthKindToken, Token: token}
	}
	cfg := AuthConfig{Kind: AuthKindNone}
	if user, ok := lookupNonEmpty(getenv, EnvHTTPUser); ok {
		cfg.Username = user
		cfg.Kind = AuthKindBasic
	}
	if password, ok := lookupNonEmpty(getenv, EnvHTTPPassword); ok {
		cfg.Password = password
		cfg.Kind = AuthKindBasic
	}
	return cfg
}

func lookupNonEmpty(getenv func(string) string, key string) (string, bool) {
	value := strings.TrimSpace(getenv(key))
	return value, value != ""
}
