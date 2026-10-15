// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrInvalidURI is matched with errors.Is when URI text fails validation.
// ~keep
var ErrInvalidURI = errors.New("invalid uri")

var digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Scheme is a URI scheme supported by the registry.
// ~keep
type Scheme string

// Supported URI schemes.
// ~keep
const (
	SchemeFS    Scheme = "fs"
	SchemeHTTP  Scheme = "http"
	SchemeHTTPS Scheme = "https"
	SchemeS3    Scheme = "s3"
	SchemeGit   Scheme = "git"
	SchemeOCI   Scheme = "oci"
)

// S3Client abstracts an S3-compatible object store so MinIO, Garage and
// other S3 API implementations work without any AWS-exclusive SDK.
// ~keep
type S3Client interface {
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error
}

// URI is a parsed registry content address. Only fs and http(s) URIs can be
// relative; every other scheme requires its absolute form.
// ~keep
type URI struct {
	Scheme    Scheme
	Transport string
	User      string
	Host      string
	Bucket    string
	Path      string
	Endpoint  string
	Ref       string
	Digest    string
	Subpath   string
}

// ParseURI parses raw text into a URI, accepting bare relative filesystem
// paths as relative fs URIs.
// ~keep
func ParseURI(raw string) (URI, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return URI{}, fmt.Errorf("%w: uri is empty", ErrInvalidURI)
	}
	if strings.HasPrefix(trimmed, "fs:") {
		return parseFSURI(trimmed)
	}
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		return parseAbsoluteURI(trimmed, trimmed[:idx])
	}
	if scheme, _, ok := strings.Cut(trimmed, ":"); ok && schemePattern(scheme) {
		return URI{}, fmt.Errorf("%w: %q uses unsupported scheme %q", ErrInvalidURI, trimmed, scheme)
	}
	return parseRelativeFS(trimmed)
}

func schemePattern(scheme string) bool {
	if scheme == "" {
		return false
	}
	for i, r := range scheme {
		isLetter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if isLetter {
			continue
		}
		if i > 0 && (r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			continue
		}
		return false
	}
	return true
}

func parseAbsoluteURI(raw, schemePart string) (URI, error) {
	scheme, transport := splitGitScheme(schemePart)
	switch scheme {
	case SchemeFS:
		return parseFSURI(raw)
	case SchemeHTTP, SchemeHTTPS:
		return parseHTTPURI(raw, scheme)
	case SchemeS3:
		return parseS3URI(raw)
	case SchemeGit:
		return parseGitURI(raw, transport)
	case SchemeOCI:
		return parseOCIURI(raw)
	default:
		return URI{}, fmt.Errorf("%w: %q uses unsupported scheme %q", ErrInvalidURI, raw, schemePart)
	}
}

func splitGitScheme(schemePart string) (Scheme, string) {
	if rest, ok := strings.CutPrefix(schemePart, "git+"); ok {
		return SchemeGit, rest
	}
	if schemePart == "git" {
		return SchemeGit, "git"
	}
	return Scheme(schemePart), ""
}

func parseFSURI(raw string) (URI, error) {
	rest := strings.TrimPrefix(strings.TrimPrefix(raw, "fs://"), "fs:")
	if strings.TrimSpace(rest) == "" {
		return URI{}, fmt.Errorf("%w: %q has an empty path", ErrInvalidURI, raw)
	}
	return URI{Scheme: SchemeFS, Path: rest}, nil
}

func parseRelativeFS(raw string) (URI, error) {
	if strings.HasPrefix(raw, "/") {
		return URI{}, fmt.Errorf("%w: %q needs an explicit fs: scheme", ErrInvalidURI, raw)
	}
	return URI{Scheme: SchemeFS, Path: path.Clean(raw)}, nil
}

func rejectEmbeddedPassword(raw string, user *url.Userinfo) error {
	if user != nil {
		if _, has := user.Password(); has {
			return fmt.Errorf("%w: %q must not embed credentials; use auth config", ErrInvalidURI, raw)
		}
	}
	return nil
}

func parseHTTPURI(raw string, scheme Scheme) (URI, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return URI{}, fmt.Errorf("%w: %q is not a valid URL: %w", ErrInvalidURI, raw, err)
	}
	if parsed.Host == "" {
		return URI{}, fmt.Errorf("%w: %q has an empty host", ErrInvalidURI, raw)
	}
	if err := rejectEmbeddedPassword(raw, parsed.User); err != nil {
		return URI{}, err
	}
	out := URI{Scheme: scheme, Host: parsed.Host, Path: parsed.EscapedPath()}
	if parsed.User != nil {
		out.User = parsed.User.Username()
	}
	if out.Path == "" {
		out.Path = "/"
	}
	return out, nil
}

func parseS3URI(raw string) (URI, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return URI{}, fmt.Errorf("%w: %q is not a valid URL: %w", ErrInvalidURI, raw, err)
	}
	if parsed.Host == "" {
		return URI{}, fmt.Errorf("%w: %q has an empty bucket", ErrInvalidURI, raw)
	}
	if strings.Contains(parsed.Host, ".") && strings.Contains(parsed.Host, ":") {
		return URI{}, fmt.Errorf("%w: %q must use s3://bucket/key form", ErrInvalidURI, raw)
	}
	out := URI{Scheme: SchemeS3, Bucket: parsed.Host, Path: strings.TrimPrefix(parsed.EscapedPath(), "/")}
	if strings.Contains(out.Bucket, "/") || strings.TrimSpace(out.Bucket) == "" {
		return URI{}, fmt.Errorf("%w: %q has an invalid bucket", ErrInvalidURI, raw)
	}
	if out.Path == "" {
		return URI{}, fmt.Errorf("%w: %q has an empty key", ErrInvalidURI, raw)
	}
	for key, values := range parsed.Query() {
		switch key {
		case "endpoint":
			if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
				return URI{}, fmt.Errorf("%w: %q has an invalid endpoint", ErrInvalidURI, raw)
			}
			endpoint, err := url.Parse(strings.TrimSpace(values[0]))
			if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
				return URI{}, fmt.Errorf("%w: %q has an invalid endpoint", ErrInvalidURI, raw)
			}
			out.Endpoint = strings.TrimSpace(values[0])
		default:
			return URI{}, fmt.Errorf("%w: %q has unknown query parameter %q", ErrInvalidURI, raw, key)
		}
	}
	return out, nil
}

func parseGitURI(raw, transport string) (URI, error) {
	switch transport {
	case "git", "http", "https", "ssh":
	default:
		return URI{}, fmt.Errorf("%w: %q uses unsupported git transport %q", ErrInvalidURI, raw, transport)
	}
	locator := raw
	if transport != "git" {
		locator = strings.TrimPrefix(raw, "git+")
	}
	parsed, err := url.Parse(locator)
	if err != nil {
		return URI{}, fmt.Errorf("%w: %q is not a valid URL: %w", ErrInvalidURI, raw, err)
	}
	if parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
		return URI{}, fmt.Errorf("%w: %q needs a host and repository path", ErrInvalidURI, raw)
	}
	if err := rejectEmbeddedPassword(raw, parsed.User); err != nil {
		return URI{}, err
	}
	out := URI{Scheme: SchemeGit, Transport: transport, Host: parsed.Host, Path: strings.TrimPrefix(parsed.EscapedPath(), "/")}
	if parsed.User != nil {
		out.User = parsed.User.Username()
	}
	for key, values := range parsed.Query() {
		if len(values) != 1 {
			return URI{}, fmt.Errorf("%w: %q has a duplicate query parameter %q", ErrInvalidURI, raw, key)
		}
		switch key {
		case "ref":
			out.Ref = strings.TrimSpace(values[0])
		case "subpath":
			out.Subpath = strings.TrimSpace(values[0])
		default:
			return URI{}, fmt.Errorf("%w: %q has unknown query parameter %q", ErrInvalidURI, raw, key)
		}
	}
	if out.Ref == "" {
		return URI{}, fmt.Errorf("%w: %q is missing ?ref=", ErrInvalidURI, raw)
	}
	if out.Subpath != "" && !validSubpath(out.Subpath) {
		return URI{}, fmt.Errorf("%w: %q has an invalid subpath", ErrInvalidURI, raw)
	}
	return out, nil
}

func parseOCIURI(raw string) (URI, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return URI{}, fmt.Errorf("%w: %q is not a valid URL: %w", ErrInvalidURI, raw, err)
	}
	if parsed.Host == "" {
		return URI{}, fmt.Errorf("%w: %q has an empty registry host", ErrInvalidURI, raw)
	}
	repo, ref, digest, err := splitOCIReference(strings.TrimPrefix(parsed.EscapedPath(), "/"), parsed.Fragment)
	if err != nil {
		return URI{}, fmt.Errorf("%w: %q: %w", ErrInvalidURI, raw, err)
	}
	out := URI{Scheme: SchemeOCI, Host: parsed.Host, Path: repo, Ref: ref, Digest: digest}
	if err := out.Validate(); err != nil {
		return URI{}, err
	}
	return out, nil
}

func splitOCIReference(rest, fragment string) (string, string, string, error) {
	repo := rest
	digest := ""
	if idx := strings.LastIndex(repo, "@"); idx >= 0 {
		digest = repo[idx+1:]
		repo = repo[:idx]
		if !digestRe.MatchString(digest) {
			return "", "", "", fmt.Errorf("digest %q must be a sha256: digest", digest)
		}
	}
	_ = fragment
	tag := ""
	if idx := strings.LastIndex(repo, ":"); idx > strings.LastIndex(repo, "/") {
		tag = repo[idx+1:]
		repo = repo[:idx]
	}
	if strings.TrimSpace(repo) == "" || !strings.Contains(repo, "/") {
		return "", "", "", fmt.Errorf("repository %q must include a namespace", rest)
	}
	if tag != "" {
		if _, err := ParseStrict(tag); err != nil {
			return "", "", "", fmt.Errorf("tag %q: %w", tag, err)
		}
	}
	return repo, tag, digest, nil
}

// Validate reports whether the URI is complete for its scheme.
// ~keep
func (u URI) Validate() error {
	switch u.Scheme {
	case SchemeFS:
		if u.Path == "" {
			return fmt.Errorf("%w: fs uri has an empty path", ErrInvalidURI)
		}
	case SchemeHTTP, SchemeHTTPS:
		if u.Host == "" {
			return fmt.Errorf("%w: http uri has an empty host", ErrInvalidURI)
		}
	case SchemeS3:
		if u.Bucket == "" || u.Path == "" {
			return fmt.Errorf("%w: s3 uri needs a bucket and key", ErrInvalidURI)
		}
	case SchemeGit:
		if u.Host == "" || u.Path == "" || u.Ref == "" {
			return fmt.Errorf("%w: git uri needs a host, repository path and ?ref=", ErrInvalidURI)
		}
	case SchemeOCI:
		if u.Host == "" || u.Path == "" {
			return fmt.Errorf("%w: oci uri needs a host and repository", ErrInvalidURI)
		}
	default:
		return fmt.Errorf("%w: unknown scheme %q", ErrInvalidURI, u.Scheme)
	}
	return nil
}

// IsRelative reports whether the URI is a relative fs reference that needs
// a base to resolve against.
// ~keep
func (u URI) IsRelative() bool {
	return u.Scheme == SchemeFS && !strings.HasPrefix(u.Path, "/")
}

// ResolveRelativeTo resolves a relative URI against base, which is either a
// filesystem directory for fs results or an absolute http(s) URL for http
// results. Absolute URIs return unchanged.
// ~keep
func (u URI) ResolveRelativeTo(base string) (URI, error) {
	if !u.IsRelative() {
		return u, nil
	}
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return URI{}, fmt.Errorf("%w: cannot resolve %q against an empty base", ErrInvalidURI, u.Path)
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return resolveAgainstURL(u, trimmed)
	}
	joined := filepath.ToSlash(filepath.Join(trimmed, filepath.FromSlash(u.Path)))
	return URI{Scheme: SchemeFS, Path: joined}, nil
}

func resolveAgainstURL(u URI, base string) (URI, error) {
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Host == "" {
		return URI{}, fmt.Errorf("%w: base %q is not an absolute http(s) URL", ErrInvalidURI, base)
	}
	ref, err := url.Parse(u.Path)
	if err != nil {
		return URI{}, fmt.Errorf("%w: cannot resolve %q: %w", ErrInvalidURI, u.Path, err)
	}
	return ParseURI(baseURL.ResolveReference(ref).String())
}

// S3Parts returns the endpoint, bucket and key of an S3 URI. The endpoint is
// empty when the URI carries no override and resolution must fall back to
// auth configuration or the client default.
// ~keep
func (u URI) S3Parts() (endpoint, bucket, key string) {
	return u.Endpoint, u.Bucket, u.Path
}

// String returns the canonical form of the URI.
// ~keep
func (u URI) String() string {
	switch u.Scheme {
	case SchemeFS:
		if u.IsRelative() {
			return u.Path
		}
		return "fs:" + u.Path
	case SchemeHTTP, SchemeHTTPS:
		return string(u.Scheme) + "://" + u.Host + ensureSlash(u.Path)
	case SchemeS3:
		out := "s3://" + u.Bucket + "/" + u.Path
		if u.Endpoint != "" {
			out += "?endpoint=" + u.Endpoint
		}
		return out
	case SchemeGit:
		return u.gitString()
	case SchemeOCI:
		out := "oci://" + u.Host + "/" + u.Path
		if u.Ref != "" {
			out += ":" + u.Ref
		}
		if u.Digest != "" {
			out += "@" + u.Digest
		}
		return out
	default:
		return ""
	}
}

func (u URI) gitString() string {
	host := u.Host
	if u.User != "" {
		host = u.User + "@" + u.Host
	}
	var out string
	if u.Transport == "git" {
		out = "git://" + host + "/" + u.Path
	} else {
		out = "git+" + u.Transport + "://" + host + "/" + u.Path
	}
	out += "?ref=" + u.Ref
	if u.Subpath != "" {
		out += "&subpath=" + u.Subpath
	}
	return out
}

func ensureSlash(p string) string {
	if p == "" {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

func validSubpath(subpath string) bool {
	cleaned := path.Clean(subpath)
	if cleaned == "." {
		return true
	}
	if strings.HasPrefix(cleaned, "/") {
		return false
	}
	for segment := range strings.SplitSeq(cleaned, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

// MarshalJSON renders the URI in canonical string form without HTML
// escaping.
// ~keep
func (u URI) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(u.String()); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// UnmarshalJSON parses canonical URI string form, rejecting unknown
// shapes via the strict ParseURI grammar.
// ~keep
func (u *URI) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		*u = URI{}
		return nil
	}
	parsed, err := ParseURI(raw)
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}
