// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/registry"
)

const (
	registryIndexTimeout = 60 * time.Second
	ociIndexTag          = "latest"
)

// RegistryRef is a parsed --template value: registry/template@version with
// an optional version. A bare template name resolves to the default
// registry; a missing version resolves to latest.
// ~keep
type RegistryRef struct {
	Registry string
	Template string
	Version  string
}

// String returns the canonical registry/template@version form, omitting an
// empty version.
// ~keep
func (r RegistryRef) String() string {
	if r.Version == "" {
		return r.Registry + "/" + r.Template
	}
	return r.Registry + "/" + r.Template + "@" + r.Version
}

// ParseRegistryRef parses --template values. Slashes after the first belong
// to the template name, so nested template paths survive the split.
// ~keep
func ParseRegistryRef(raw string) (RegistryRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return RegistryRef{}, fmt.Errorf("template is empty: want registry/template[@version]")
	}
	ref, version, _ := strings.Cut(trimmed, "@")
	if strings.Contains(version, "@") {
		return RegistryRef{}, fmt.Errorf("template %q has more than one @", raw)
	}
	if strings.TrimSpace(version) != "" {
		if _, err := registry.ParseStrict(strings.TrimSpace(version)); err != nil {
			return RegistryRef{}, fmt.Errorf("template %q: %w", raw, err)
		}
	}
	head, template, _ := strings.Cut(ref, "/")
	head = strings.TrimSpace(head)
	template = strings.TrimSpace(template)
	if template == "" {
		head = registry.DefaultRegistryName
		template = strings.TrimSpace(ref)
	}
	if head == "" || template == "" {
		return RegistryRef{}, fmt.Errorf("template %q: want registry/template[@version]", raw)
	}
	out := RegistryRef{Registry: head, Template: template, Version: strings.TrimSpace(version)}
	if err := registry.ValidateTemplateName(out.Template); err != nil {
		return RegistryRef{}, fmt.Errorf("template %q: %w", raw, err)
	}
	return out, nil
}

// DefaultRegistryRef is turutan/default at latest: zero user config needed
// because the turutan name falls back to its OCI address.
// ~keep
func DefaultRegistryRef() RegistryRef {
	return RegistryRef{Registry: registry.DefaultRegistryName, Template: registry.DefaultTemplateName}
}

// ResolveTemplateFlag applies the --template layering: flag wins, then the
// TURUTAN_TEMPLATE environment, then the user config template, then the
// turutan/default default.
// ~keep
func ResolveTemplateFlag(flag string, envVal, userTemplate string) RegistryRef {
	for _, candidate := range []string{flag, envVal, userTemplate} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			if ref, err := ParseRegistryRef(trimmed); err == nil {
				return ref
			}
		}
	}
	return DefaultRegistryRef()
}

// ResolvedRegistry bundles a named registry with its auth and index.
// Root is the absolute local dir for fs registries and empty otherwise.
// ~keep
type ResolvedRegistry struct {
	Name string
	URI  registry.URI
	Auth registry.AuthConfig
	Root string
	Idx  *registry.RegistryIndex
}

// ResolveRegistry loads the index for a named registry reference.
// ~keep
func ResolveRegistry(ctx context.Context, registries map[string]string, name string) (*ResolvedRegistry, error) {
	uri, err := registry.ResolveRegistryURI(registries, name)
	if err != nil {
		return nil, err
	}
	auth := registry.ResolveAuth(name, uri)
	index, root, err := loadRegistryIndex(ctx, uri, auth)
	if err != nil {
		return nil, err
	}
	return &ResolvedRegistry{Name: name, URI: uri, Auth: auth, Root: root, Idx: index}, nil
}

func loadRegistryIndex(ctx context.Context, uri registry.URI, auth registry.AuthConfig) (*registry.RegistryIndex, string, error) {
	switch uri.Scheme {
	case registry.SchemeFS:
		return loadFSIndex(uri)
	case registry.SchemeHTTP, registry.SchemeHTTPS:
		return loadHTTPIndex(ctx, uri, auth)
	case registry.SchemeS3:
		return loadS3Index(ctx, uri, auth)
	case registry.SchemeGit:
		return loadGitIndex(ctx, uri, auth)
	case registry.SchemeOCI:
		return loadOCIIndex(ctx, uri, auth)
	default:
		return nil, "", fmt.Errorf("loading registry index: unknown scheme %q", uri.Scheme)
	}
}

func loadFSIndex(uri registry.URI) (*registry.RegistryIndex, string, error) {
	dir := uri.Path
	if uri.IsRelative() {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, "", fmt.Errorf("loading registry index: %w", err)
		}
		resolved, err := uri.ResolveRelativeTo(cwd)
		if err != nil {
			return nil, "", err
		}
		dir = resolved.Path
	}
	path := filepath.Join(dir, registry.IndexFileName)
	index, err := registry.LoadIndexFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("loading registry index: no %s in %q: %w",
			registry.IndexFileName, dir, err)
	}
	return index, dir, nil
}

func loadHTTPIndex(ctx context.Context, uri registry.URI, auth registry.AuthConfig) (*registry.RegistryIndex, string, error) {
	bases := []string{uri.String()}
	if !strings.HasSuffix(uri.Path, ".json") {
		trimmed := strings.TrimSuffix(uri.String(), "/")
		bases = []string{trimmed + "/" + registry.IndexFileName}
	}
	client := &http.Client{Timeout: registryIndexTimeout}
	var lastErr error
	for _, address := range bases {
		index, err := fetchHTTPIndex(ctx, client, address, auth)
		if err == nil {
			return index, "", nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

func fetchHTTPIndex(ctx context.Context, client *http.Client, address string, auth registry.AuthConfig) (*registry.RegistryIndex, error) {
	ctx, cancel := context.WithTimeout(ctx, registryIndexTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("loading registry index from %s: %w", address, err)
	}
	req.Header.Set("Accept", "application/json")
	switch auth.Kind {
	case registry.AuthKindToken:
		if token, ok := auth.BearerToken(); ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case registry.AuthKindBasic:
		if username, password, ok := auth.BasicAuth(); ok {
			req.SetBasicAuth(username, password)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loading registry index from %s: %w", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("loading registry index from %s: server answered %s", address, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("loading registry index from %s: %w", address, err)
	}
	index, err := registry.ParseIndex(data)
	if err != nil {
		return nil, fmt.Errorf("loading registry index from %s: %w", address, err)
	}
	return index, nil
}

func loadS3Index(ctx context.Context, uri registry.URI, auth registry.AuthConfig) (*registry.RegistryIndex, string, error) {
	factory := registry.DefaultS3ClientFactory
	client, err := factory(uri, auth)
	if err != nil {
		return nil, "", fmt.Errorf("loading registry index: %w", err)
	}
	keys := []string{strings.TrimSuffix(uri.Path, "/") + "/" + registry.IndexFileName}
	if strings.HasSuffix(uri.Path, ".json") {
		keys = []string{uri.Path}
	}
	var lastErr error
	for _, key := range keys {
		body, err := client.GetObject(ctx, uri.Bucket, key)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(body, 8<<20))
		_ = body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		index, err := registry.ParseIndex(data)
		if err != nil {
			lastErr = err
			continue
		}
		return index, "", nil
	}
	return nil, "", fmt.Errorf("loading registry index s3://%s/%s: %w", uri.Bucket, uri.Path, lastErr)
}

func loadGitIndex(ctx context.Context, uri registry.URI, _ registry.AuthConfig) (*registry.RegistryIndex, string, error) {
	remote := gitRemoteForURI(uri)
	tmp, err := os.MkdirTemp("", "turutan-registry-*")
	if err != nil {
		return nil, "", fmt.Errorf("loading registry index: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if _, err := git.Clone(ctx, remote, uri.Ref, tmp, git.ShallowDepth); err != nil {
		return nil, "", fmt.Errorf("loading registry index: %w", err)
	}
	base := tmp
	if strings.TrimSpace(uri.Subpath) != "" {
		joined := filepath.Join(tmp, filepath.FromSlash(uri.Subpath))
		abs, err := filepath.Abs(joined)
		if err != nil {
			return nil, "", fmt.Errorf("loading registry index: %w", err)
		}
		base = abs
	}
	if index, err := registry.LoadIndexFile(filepath.Join(base, registry.IndexFileName)); err == nil {
		return index, "", nil
	}
	return nil, "", fmt.Errorf("loading registry index: no %s in %q",
		registry.IndexFileName, base)
}

func gitRemoteForURI(uri registry.URI) string {
	host := uri.Host
	if uri.User != "" {
		host = uri.User + "@" + host
	}
	if uri.Transport == "git" {
		return "git://" + host + "/" + uri.Path
	}
	return uri.Transport + "://" + host + "/" + uri.Path
}

func loadOCIIndex(ctx context.Context, uri registry.URI, auth registry.AuthConfig) (*registry.RegistryIndex, string, error) {
	indexURI := uri
	indexURI.Path = strings.TrimSuffix(uri.Path, "/") + "/index"
	if indexURI.Ref == "" {
		indexURI.Ref = ociIndexTag
	}
	indexURI.Digest = ""
	backend := registry.NewOCIBackend(nil)
	tmp, err := os.MkdirTemp("", "turutan-registry-*")
	if err != nil {
		return nil, "", fmt.Errorf("loading registry index: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := backend.Fetch(ctx, indexURI, auth, tmp); err != nil {
		return nil, "", fmt.Errorf("loading registry index %s: %w", indexURI.String(), err)
	}
	if index, err := registry.LoadIndexFile(filepath.Join(tmp, registry.IndexFileName)); err == nil {
		return index, "", nil
	}
	return nil, "", fmt.Errorf("loading registry index %s: no %s in the index artifact",
		indexURI.String(), registry.IndexFileName)
}

// ResolveTemplateManifest returns the manifest for template at version, or
// the latest when version is empty. Older versions prefer their versioned
// manifest file templates/<name>/manifest/<version>.json: fs registries
// read it from disk, http(s)/s3 registries fetch it over the network, and
// other schemes return a clear error naming the expected file.
// ~keep
func ResolveTemplateManifest(rr *ResolvedRegistry, template, version string) (*registry.TemplateManifest, error) {
	entry, err := rr.Idx.FindTemplate(template)
	if err != nil {
		return nil, err
	}
	want := strings.TrimSpace(version)
	if want == "" {
		out := entry.Manifest()
		return &out, nil
	}
	if _, err := registry.ParseStrict(want); err != nil {
		return nil, err
	}
	if want == entry.Version {
		out := entry.Manifest()
		return &out, nil
	}
	found := false
	for _, release := range entry.Releases {
		if release == want {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("template %q has no release %q (releases: %s)",
			template, want, strings.Join(entry.Releases, ", "))
	}
	if rr.Root != "" {
		if versioned, err := registry.LoadVersionedManifest(rr.Root, template, want); err == nil {
			return versioned, nil
		}
	}
	switch rr.URI.Scheme {
	case registry.SchemeHTTP, registry.SchemeHTTPS:
		versioned, err := fetchHTTPVersionedManifest(rr, template, want)
		if err != nil {
			return nil, err
		}
		return versioned, nil
	case registry.SchemeS3:
		versioned, err := fetchS3VersionedManifest(rr, template, want)
		if err != nil {
			return nil, err
		}
		return versioned, nil
	}
	return nil, fmt.Errorf("template %q release %q needs its versioned manifest %s (only latest %q is cached)",
		template, want, registry.VersionedManifestPath(rr.Root, template, want), entry.Version)
}

func fetchHTTPVersionedManifest(rr *ResolvedRegistry, template, version string) (*registry.TemplateManifest, error) {
	base := strings.TrimSuffix(rr.URI.String(), "/")
	address := base + "/" + registry.ManifestObjectKey(template, version)
	ctx, cancel := context.WithTimeout(context.Background(), registryIndexTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest from %s: %w", address, err)
	}
	req.Header.Set("Accept", "application/json")
	switch rr.Auth.Kind {
	case registry.AuthKindToken:
		if token, ok := rr.Auth.BearerToken(); ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case registry.AuthKindBasic:
		if username, password, ok := rr.Auth.BasicAuth(); ok {
			req.SetBasicAuth(username, password)
		}
	}
	client := &http.Client{Timeout: registryIndexTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest from %s: %w", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("loading versioned manifest from %s: server answered %s", address, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest from %s: %w", address, err)
	}
	manifest, err := registry.ParseVersionedManifest(data)
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest from %s: %w", address, err)
	}
	return manifest, nil
}

func fetchS3VersionedManifest(rr *ResolvedRegistry, template, version string) (*registry.TemplateManifest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), registryIndexTimeout)
	defer cancel()
	client, err := registry.DefaultS3ClientFactory(rr.URI, rr.Auth)
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest: %w", err)
	}
	key := strings.TrimSuffix(rr.URI.Path, "/") + "/" + registry.ManifestObjectKey(template, version)
	body, err := client.GetObject(ctx, rr.URI.Bucket, key)
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest s3://%s/%s: %w", rr.URI.Bucket, key, err)
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest s3://%s/%s: %w", rr.URI.Bucket, key, err)
	}
	manifest, err := registry.ParseVersionedManifest(data)
	if err != nil {
		return nil, fmt.Errorf("loading versioned manifest s3://%s/%s: %w", rr.URI.Bucket, key, err)
	}
	return manifest, nil
}

// FetchTemplateBundle fetches the bundle for manifest into a temp dir,
// verifying the bundle digest against the manifest when present. It returns
// the bundle dir, the stored (pre-resolution) fetch URI recorded in state,
// and cleanup. Relative fs URIs resolve against the registry root for the
// fetch but the stored form stays relative so state files move with the
// checkout.
// ~keep
func FetchTemplateBundle(ctx context.Context, rr *ResolvedRegistry, manifest *registry.TemplateManifest) (string, string, func(), error) {
	uri := manifest.URI
	if uri.IsRelative() {
		if rr.Root == "" {
			return "", "", nil, fmt.Errorf("fetching template %q: relative uri %q needs a local registry root", manifest.Name, uri.Path)
		}
		resolved, err := uri.ResolveRelativeTo(rr.Root)
		if err != nil {
			return "", "", nil, err
		}
		uri = resolved
	}
	backend, err := registry.BackendFor(uri.Scheme)
	if err != nil {
		return "", "", nil, err
	}
	tmp, err := os.MkdirTemp("", "turutan-template-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("fetching template %q: %w", manifest.Name, err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	auth := registry.ResolveAuth(rr.Name, uri)
	if err := backend.Fetch(ctx, uri, auth, tmp); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if strings.TrimSpace(manifest.Digest) != "" {
		got, err := registry.DigestDir(tmp)
		if err != nil {
			cleanup()
			return "", "", nil, fmt.Errorf("fetching template %q: %w", manifest.Name, err)
		}
		if got != manifest.Digest {
			cleanup()
			return "", "", nil, fmt.Errorf("fetching template %q: digest mismatch: manifest wants %s", manifest.Name, manifest.Digest)
		}
	}
	return tmp, manifest.URI.String(), cleanup, nil
}

// RegistryStateRef returns the canonical registry/template@version ref.
// ~keep
func RegistryStateRef(registryName, template, version string) string {
	return registryName + "/" + template + "@" + version
}

// LoadProjectRegistries reads the named registry references for project
// operations from path, defaulting to an empty map when path is empty.
// ~keep
func LoadProjectRegistries(path string) map[string]string {
	regs, err := config.LoadRegistries(path)
	if err != nil || regs == nil {
		return map[string]string{}
	}
	return regs
}

// ResolveRegistryName extracts the registry name from a stored template ref
// of the form registry/template@version.
// ~keep
func ResolveRegistryName(stored string) (registryName, template, version string) {
	ref := strings.TrimSpace(stored)
	if at := strings.LastIndex(ref, "@"); at >= 0 {
		version = strings.TrimSpace(ref[at+1:])
		ref = ref[:at]
	}
	if slash := strings.Index(ref, "/"); slash >= 0 {
		return strings.TrimSpace(ref[:slash]), strings.TrimSpace(ref[slash+1:]), version
	}
	return registry.DefaultRegistryName, strings.TrimSpace(ref), version
}
