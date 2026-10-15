// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReleaseBump is the version component incremented by `registry release`.
// ~keep
type ReleaseBump string

// Supported release bump kinds.
// ~keep
const (
	ReleaseMajor ReleaseBump = "major"
	ReleaseMinor ReleaseBump = "minor"
	ReleasePatch ReleaseBump = "patch"
)

// ParseReleaseBump validates the bump argument of `registry release`.
// ~keep
func ParseReleaseBump(raw string) (ReleaseBump, error) {
	switch ReleaseBump(strings.TrimSpace(raw)) {
	case ReleaseMajor, ReleaseMinor, ReleasePatch:
		return ReleaseBump(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("invalid bump %q: want major|minor|patch", raw)
	}
}

// BumpVersion returns base with the bumped component incremented.
// ~keep
func BumpVersion(base string, bump ReleaseBump) (string, error) {
	switch bump {
	case ReleaseMajor:
		return BumpMajor(base)
	case ReleaseMinor:
		return BumpMinor(base)
	case ReleasePatch:
		return BumpPatch(base)
	default:
		return "", fmt.Errorf("invalid bump %q: want major|minor|patch", bump)
	}
}

// NextReleaseVersion returns the version for a new release: 0.1.0 when the
// template is not indexed yet, otherwise the bumped latest version.
// ~keep
func NextReleaseVersion(index *RegistryIndex, name string, bump ReleaseBump) (string, bool, error) {
	existing, err := index.FindTemplate(name)
	if err != nil {
		return registryStarterVersion(), false, nil
	}
	next, err := BumpVersion(existing.Version, bump)
	if err != nil {
		return "", true, err
	}
	return next, true, nil
}

func registryStarterVersion() string { return "0.1.0" }

// VersionedManifestPath returns the canonical versioned manifest file for a
// template release. The canonical extension is .yml.
// ~keep
func VersionedManifestPath(root, name, version string) string {
	return filepath.Join(root, "templates", filepath.FromSlash(name), "manifest", version+".yml")
}

// LoadVersionedManifest reads templates/<name>/manifest/<version>.yml,
// accepting a .yaml alternate on read.
// ~keep
func LoadVersionedManifest(root, name, version string) (*TemplateManifest, error) {
	for _, path := range []string{
		VersionedManifestPath(root, name, version),
		filepath.Join(root, "templates", filepath.FromSlash(name), "manifest", version+".yaml"),
	} {
		data, err := os.ReadFile(path) // #nosec G304 -- path is built from the registry root plus validated template name/version
		if err != nil {
			continue
		}
		var manifest TemplateManifest
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&manifest); err != nil {
			return nil, fmt.Errorf("parsing versioned manifest %q: %w", path, err)
		}
		if err := manifest.Validate(); err != nil {
			return nil, fmt.Errorf("loading versioned manifest %q: %w", path, err)
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("%w: %q@%s", ErrTemplateNotFound, name, version)
}

// SaveVersionedManifest writes the canonical versioned manifest file.
// ~keep
func SaveVersionedManifest(root string, manifest *TemplateManifest) (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	path := VersionedManifestPath(root, manifest.Name, manifest.Version)
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encoding versioned manifest %q: %w", path, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing versioned manifest %q: %w", path, err)
	}
	return path, nil
}

// UpsertIndexRelease records a release in the index: the entry latest
// fields follow the new manifest and the version is prepended to releases.
// ~keep
func UpsertIndexRelease(index *RegistryIndex, manifest TemplateManifest) {
	releases := []string{manifest.Version}
	for _, release := range manifest.Releases {
		if release != manifest.Version {
			releases = append(releases, release)
		}
	}
	manifest.Releases = releases
	for i := range index.Templates {
		if index.Templates[i].Name == manifest.Name {
			index.Templates[i] = manifest
			return
		}
	}
	index.Templates = append(index.Templates, manifest)
}

// ReleaseSnapshotPath returns the relative fs path holding the immutable
// bundle tree for a template version. Fs releases copy the staged tree
// here so pinned fetches stay immutable while the authoring tree moves on.
// ~keep
func ReleaseSnapshotPath(name, version string) string {
	return filepath.ToSlash(filepath.Join("templates", filepath.FromSlash(name), "releases", version))
}

// BuildReleaseURI resolves the release URI for a template version. An
// explicit override wins; otherwise fs defaults to the versioned snapshot
// path templates/<name>/releases/<version> (see ReleaseSnapshotPath) while
// git and oci adapt the indexed entry and http(s)/s3 reuse it. First
// releases for non-fs schemes require --uri-override because the host or
// bucket cannot be guessed.
// ~keep
func BuildReleaseURI(scheme, name, version, override string, existing *TemplateManifest) (URI, error) {
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

// StageReleaseTree copies srcDir to a temp dir excluding the versioned
// manifest subtree, so releases never bundle their own release history.
// ~keep
func StageReleaseTree(srcDir string) (string, func(), error) {
	staged, err := os.MkdirTemp("", "turutan-release-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating release staging dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(staged) }
	if err := CopyTreeFiltered(srcDir, staged); err != nil {
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
