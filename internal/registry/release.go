// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReleaseBump is the version component incremented by `registry release`.
// Use ReleaseSame to rebuild the pinned version without bumping.
// ~keep
type ReleaseBump string

// Supported release bump kinds.
// ~keep
const (
	ReleaseMajor ReleaseBump = "major"
	ReleaseMinor ReleaseBump = "minor"
	ReleasePatch ReleaseBump = "patch"
	// ReleaseSame rebuilds the existing version in place (--force refresh
	// of the archive, manifest and index without a version bump).
	// ~keep
	ReleaseSame ReleaseBump = "same"
)

// ParseReleaseBump validates the bump argument of `registry release`.
// ~keep
func ParseReleaseBump(raw string) (ReleaseBump, error) {
	switch ReleaseBump(strings.TrimSpace(raw)) {
	case ReleaseMajor, ReleaseMinor, ReleasePatch, ReleaseSame:
		return ReleaseBump(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("invalid bump %q: want major|minor|patch|same", raw)
	}
}

// BumpVersion returns base with the bumped component incremented. The
// "same" bump returns base unchanged for in-place rebuilds.
// ~keep
func BumpVersion(base string, bump ReleaseBump) (string, error) {
	switch bump {
	case ReleaseMajor:
		return BumpMajor(base)
	case ReleaseMinor:
		return BumpMinor(base)
	case ReleasePatch:
		return BumpPatch(base)
	case ReleaseSame:
		if _, err := ParseStrict(base); err != nil {
			return "", err
		}
		return canonical(base), nil
	default:
		return "", fmt.Errorf("invalid bump %q: want major|minor|patch|same", bump)
	}
}

// NextReleaseVersion returns the version for a new release: 0.1.0 when the
// template is not indexed yet, otherwise the bumped latest version. The
// "same" bump returns the indexed latest version for in-place rebuilds.
// ~keep
func NextReleaseVersion(index *RegistryIndex, name string, bump ReleaseBump) (string, bool, error) {
	existing, err := index.FindTemplate(name)
	if err != nil {
		return registryStarterVersion(), false, nil
	}
	if bump == ReleaseSame {
		return existing.Version, true, nil
	}
	next, err := BumpVersion(existing.Version, bump)
	if err != nil {
		return "", true, err
	}
	return next, true, nil
}

func registryStarterVersion() string { return "0.1.0" }

// marshalJSONIndent encodes value as indented JSON without HTML escaping,
// so canonical "Name <email> (website)" author strings stay readable.
// ~keep
func marshalJSONIndent(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// VersionedManifestPath returns the canonical versioned manifest file for a
// template release: templates/<name>/manifest/<version>.json.
// ~keep
func VersionedManifestPath(root, name, version string) string {
	return filepath.Join(root, "templates", filepath.FromSlash(name), "manifest", version+".json")
}

// LoadVersionedManifest reads templates/<name>/manifest/<version>.json with
// strict JSON decoding.
// ~keep
func LoadVersionedManifest(root, name, version string) (*TemplateManifest, error) {
	path := VersionedManifestPath(root, name, version)
	data, err := os.ReadFile(path) // #nosec G304 -- path is built from the registry root plus validated template name/version
	if err != nil {
		return nil, fmt.Errorf("%w: %q@%s", ErrTemplateNotFound, name, version)
	}
	var manifest TemplateManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing versioned manifest %q: %w", path, err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("loading versioned manifest %q: %w", path, err)
	}
	return &manifest, nil
}

// ParseVersionedManifest decodes versioned manifest JSON bytes (for
// http/s3 fetches of templates/<name>/manifest/<version>.json).
// ~keep
func ParseVersionedManifest(data []byte) (*TemplateManifest, error) {
	var manifest TemplateManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing versioned manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// SaveVersionedManifest writes the canonical versioned manifest JSON file.
// ~keep
func SaveVersionedManifest(root string, manifest *TemplateManifest) (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	path := VersionedManifestPath(root, manifest.Name, manifest.Version)
	data, err := marshalJSONIndent(manifest)
	if err != nil {
		return "", fmt.Errorf("encoding versioned manifest %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing versioned manifest %q: %w", path, err)
	}
	return path, nil
}

// ManifestObjectKey returns the registry-relative object key for a
// versioned manifest file, for http/s3 fetches.
// ~keep
func ManifestObjectKey(name, version string) string {
	return "templates/" + name + "/manifest/" + version + ".json"
}

// UpsertIndexRelease records a release in the index: the entry latest
// fields follow the new manifest and the version moves to the front of the
// newest-first releases history.
// ~keep
func UpsertIndexRelease(index *RegistryIndex, manifest TemplateManifest) {
	for i := range index.Templates {
		if index.Templates[i].Name == manifest.Name {
			entry := &index.Templates[i]
			entry.Version = manifest.Version
			entry.Author = manifest.Author
			entry.Contributors = manifest.Contributors
			entry.URI = manifest.URI
			entry.Digest = manifest.Digest
			entry.MinEngine = manifest.MinEngine
			entry.ManifestHash = manifest.ManifestHash
			entry.License = manifest.License
			releases := []string{manifest.Version}
			for _, release := range entry.Releases {
				if release != manifest.Version {
					releases = append(releases, release)
				}
			}
			entry.Releases = releases
			return
		}
	}
	index.Templates = append(index.Templates, RegistryTemplate{
		Name:         manifest.Name,
		Version:      manifest.Version,
		Author:       manifest.Author,
		Contributors: manifest.Contributors,
		URI:          manifest.URI,
		Releases:     []string{manifest.Version},
		Digest:       manifest.Digest,
		MinEngine:    manifest.MinEngine,
		ManifestHash: manifest.ManifestHash,
		License:      manifest.License,
	})
}

// ReleaseSnapshotPath returns the registry-relative archive file holding
// the immutable bundle for a template version:
// templates/<name>/releases/<version>.tar.gz. The fs URI for a release is
// this relative path; Bundle writes the gzip-tar archive there and Fetch
// extracts it.
// ~keep
func ReleaseSnapshotPath(name, version string) string {
	return filepath.ToSlash(filepath.Join("templates", filepath.FromSlash(name), "releases", version+".tar.gz"))
}

// BuildReleaseURI resolves the release URI for a template version. An
// explicit override wins; otherwise fs defaults to the versioned archive
// path templates/<name>/releases/<version>.tar.gz (see
// ReleaseSnapshotPath) while git and oci adapt the indexed entry and
// http(s)/s3 reuse it. First releases for non-fs schemes require
// --uri-override because the host or bucket cannot be guessed.
// ~keep
func BuildReleaseURI(scheme, name, version, override string, existing *RegistryTemplate) (URI, error) {
	trimmed := strings.TrimSpace(scheme)
	if trimmed == "https" {
		trimmed = "http"
	}
	if overrideText := strings.TrimSpace(override); overrideText != "" {
		parsed, err := ParseURI(overrideText)
		if err != nil {
			return URI{}, err
		}
		return adaptOverrideURI(parsed, trimmed, name, version)
	}
	switch Scheme(trimmed) {
	case SchemeFS:
		return ParseURI(ReleaseSnapshotPath(name, version))
	case SchemeGit:
		if existing == nil {
			return URI{}, fmt.Errorf("git release needs --uri-override for the first release of %q", name)
		}
		if existing.URI.Scheme != SchemeGit {
			return URI{}, fmt.Errorf("git release of %q needs --uri-override: indexed uri is %q", name, existing.URI.String())
		}
		next := existing.URI
		next.Ref = "templates/" + name + "/" + version
		if strings.TrimSpace(next.Subpath) == "" {
			next.Subpath = "templates/" + name
		}
		if err := next.Validate(); err != nil {
			return URI{}, err
		}
		return next, nil
	case SchemeOCI:
		if existing == nil {
			return URI{}, fmt.Errorf("oci release needs --uri-override for the first release of %q", name)
		}
		if existing.URI.Scheme != SchemeOCI {
			return URI{}, fmt.Errorf("oci release of %q needs --uri-override: indexed uri is %q", name, existing.URI.String())
		}
		next := existing.URI
		next.Ref = version
		next.Digest = ""
		if err := next.Validate(); err != nil {
			return URI{}, err
		}
		return next, nil
	case SchemeHTTP, SchemeHTTPS:
		if existing == nil {
			return URI{}, fmt.Errorf("http release needs --uri-override for the first release of %q", name)
		}
		if existing.URI.Scheme != SchemeHTTP && existing.URI.Scheme != SchemeHTTPS {
			return URI{}, fmt.Errorf("http release of %q needs --uri-override: indexed uri is %q", name, existing.URI.String())
		}
		return existing.URI, nil
	case SchemeS3:
		if existing == nil {
			return URI{}, fmt.Errorf("s3 release needs --uri-override for the first release of %q", name)
		}
		if existing.URI.Scheme != SchemeS3 {
			return URI{}, fmt.Errorf("s3 release of %q needs --uri-override: indexed uri is %q", name, existing.URI.String())
		}
		return existing.URI, nil
	default:
		return URI{}, fmt.Errorf("invalid scheme %q: want fs|http|s3|git|oci", scheme)
	}
}

func adaptOverrideURI(parsed URI, scheme, name, version string) (URI, error) {
	family := func(s Scheme) string {
		if s == SchemeHTTP || s == SchemeHTTPS {
			return "http"
		}
		return string(s)
	}
	if scheme != "" && family(parsed.Scheme) != scheme {
		return URI{}, fmt.Errorf("uri %q uses scheme %q, want %q", parsed.String(), parsed.Scheme, scheme)
	}
	switch parsed.Scheme {
	case SchemeGit:
		parsed.Ref = "templates/" + name + "/" + version
		if strings.TrimSpace(parsed.Subpath) == "" {
			parsed.Subpath = "templates/" + name
		}
	case SchemeOCI:
		parsed.Ref = version
		parsed.Digest = ""
	}
	if err := parsed.Validate(); err != nil {
		return URI{}, err
	}
	return parsed, nil
}

// TemplateManifestHash returns the sha256 hash of the template authoring
// manifest (.turutan.yml or .turutan.yaml) in dir, for release provenance.
// ~keep
func TemplateManifestHash(dir string) (string, error) {
	for _, name := range []string{".turutan.yml", ".turutan.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- name is a fixed manifest file under the caller-supplied template dir
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		return "sha256:" + hex.EncodeToString(sum[:]), nil
	}
	return "", nil
}

// StageReleaseTree copies srcDir to a temp dir for bundling. Bundle
// collection skips the manifest/ and releases/ metadata subtrees, so
// archives never contain their own release history even though the
// authoring tree co-locates them.
// ~keep
func StageReleaseTree(srcDir string) (string, func(), error) {
	staged, err := os.MkdirTemp("", "turutan-release-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating release staging dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(staged) }
	if err := CopyTree(srcDir, staged); err != nil {
		cleanup()
		return "", nil, err
	}
	return staged, cleanup, nil
}

// ValidateTemplateName reports whether name is a usable template name.
// ~keep
func ValidateTemplateName(name string) error {
	if !validResourceName(name) {
		return fmt.Errorf("invalid template name %q: must match [a-z0-9._/-]", name)
	}
	return nil
}
