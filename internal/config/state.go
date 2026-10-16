// SPDX-License-Identifier: Apache-2.0

package config

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"

	"github.com/LazyGeniusMan/turutan/internal/condition"
	"github.com/LazyGeniusMan/turutan/internal/engine"
	"github.com/LazyGeniusMan/turutan/internal/filter"
)

// State and lock artifact file names shared by the CLI and scaffold engine.
// ~keep
const (
	StateFileName       = ".config/turutan/state.yml"
	LockFileName        = ".config/turutan/lock.json"
	ManifestFileName    = "template.yml"
	ManifestFileNameAlt = "template.yaml"
	StateVersion        = 1
	TemplateLicenseMIT0 = "MIT-0"
)

var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

var manifestHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ErrInvalidState is matched with errors.Is when state fails validation.
// ~keep
var ErrInvalidState = errors.New("invalid state")

// ErrInvalidLock is matched with errors.Is when lock fails validation.
// ~keep
var ErrInvalidLock = errors.New("invalid lock")

// ErrInvalidManifest is matched with errors.Is when a manifest fails validation.
// ~keep
var ErrInvalidManifest = errors.New("invalid manifest")

// State is the persisted scaffold state written to turutan-state.yml.
// ~keep
type State struct {
	Version         int            `yaml:"version"`
	SchemaVersion   int            `yaml:"schema_version"`
	Schema          string         `yaml:"$schema,omitempty"`
	Template        string         `yaml:"template"`
	SourceKind      string         `yaml:"sourceKind"`
	Registry        string         `yaml:"registry,omitempty"`
	TemplateURI     string         `yaml:"templateURI,omitempty"`
	TemplateDigest  string         `yaml:"templateDigest,omitempty"`
	Subpath         string         `yaml:"subpath,omitempty"`
	RequestedRef    string         `yaml:"requestedRef,omitempty"`
	ResolvedCommit  string         `yaml:"resolvedCommit"`
	Data            map[string]any `yaml:"data,omitempty"`
	Skip            []string       `yaml:"skip,omitempty"`
	Conflict        string         `yaml:"conflict,omitempty"`
	EngineVersion   string         `yaml:"engine_version"`
	TemplateLicense string         `yaml:"templateLicense"`
}

// LockFile describes one file captured in the lock manifest.
// ~keep
type LockFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Lock is the persisted lock manifest written to turutan-lock.json.
// ~keep
type Lock struct {
	SchemaVersion int        `json:"schema_version"`
	Schema        string     `json:"$schema,omitempty"`
	EngineVersion string     `json:"engine_version"`
	Source        string     `json:"source"`
	Ref           string     `json:"ref,omitempty"`
	ResolvedSHA   string     `json:"resolved_sha"`
	Registry      string     `json:"registry,omitempty"`
	URI           string     `json:"uri,omitempty"`
	Digest        string     `json:"digest,omitempty"`
	Subpath       string     `json:"subpath,omitempty"`
	ManifestHash  string     `json:"manifest_hash"`
	Files         []LockFile `json:"files"`
}

// ManifestInput describes one template input value.
// ~keep
type ManifestInput struct {
	Label   string   `yaml:"label"`
	Default any      `yaml:"default"`
	Type    string   `yaml:"type"`
	Options []string `yaml:"options"`
	Help    string   `yaml:"help"`
	If      string   `yaml:"if,omitempty"`
}

type ManifestConditional struct {
	If        string   `yaml:"if"`
	Includes  []string `yaml:"includes"`
	Excludes  []string `yaml:"excludes"`
	Preserves []string `yaml:"preserves"`
}

type Manifest struct {
	SchemaVersion int                      `yaml:"schema_version"`
	Schema        string                   `yaml:"$schema,omitempty"`
	EngineVersion string                   `yaml:"engine_version"`
	License       string                   `yaml:"license"`
	LicenseFiles  []string                 `yaml:"license-files"`
	DocFiles      []string                 `yaml:"doc-files"`
	Includes      []string                 `yaml:"includes"`
	Excludes      []string                 `yaml:"excludes"`
	Preserves     []string                 `yaml:"preserves"`
	Conflict      string                   `yaml:"conflict"`
	Inputs        map[string]ManifestInput `yaml:"inputs"`
	Conditionals  []ManifestConditional    `yaml:"conditionals"`
}

func LoadState(fsys fs.FS) (*State, error) {
	data, err := fs.ReadFile(fsys, StateFileName)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", StateFileName, err)
	}
	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", StateFileName, err)
	}
	if err := ValidateState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveState(dir string, state *State) error {
	state.Version = StateVersion
	state.SchemaVersion = StateVersion
	state.Schema = StateSchemaRef
	bumped, err := engine.Bump(state.EngineVersion, engine.Current())
	if err != nil {
		return err
	}
	state.EngineVersion = bumped
	if err := ValidateState(state); err != nil {
		return err
	}
	if err := writeGeneratedSchema(dir, StateSchemaFileName, StateJSONSchema()); err != nil {
		return err
	}
	return writeYAML(dir, StateFileName, state)
}

func ValidateState(state *State) error {
	if state == nil {
		return fmt.Errorf("%w: state is nil", ErrInvalidState)
	}
	if state.Version != StateVersion {
		return fmt.Errorf("%w: unsupported version %d: this engine reads v%d only", ErrInvalidState, state.Version, StateVersion)
	}
	if state.SchemaVersion != StateVersion {
		return fmt.Errorf(
			"%w: unsupported schema_version %d: this engine reads v%d only",
			ErrInvalidState, state.SchemaVersion, StateVersion)
	}
	if state.Template == "" {
		return fmt.Errorf("%w: template is empty", ErrInvalidState)
	}
	switch state.SourceKind {
	case "registry":
		if strings.TrimSpace(state.Registry) == "" {
			return fmt.Errorf("%w: registry is empty", ErrInvalidState)
		}
		if !isRegistryURI(state.Registry) {
			return fmt.Errorf("%w: registry %q is not a URI", ErrInvalidState, state.Registry)
		}
		if !isURITemplateRef(state.Template, state.Registry) {
			return fmt.Errorf("%w: template %q is not a <registry-uri>/<template>@<version> ref", ErrInvalidState, state.Template)
		}
		if strings.TrimSpace(state.TemplateURI) == "" {
			return fmt.Errorf("%w: templateURI is empty", ErrInvalidState)
		}
		if !manifestHashRe.MatchString(state.TemplateDigest) {
			return fmt.Errorf("%w: templateDigest %q is not a sha256: digest", ErrInvalidState, state.TemplateDigest)
		}
		digestHex := strings.TrimPrefix(state.TemplateDigest, "sha256:")
		if state.ResolvedCommit != digestHex && state.ResolvedCommit != state.TemplateDigest {
			return fmt.Errorf("%w: resolvedCommit %q does not match templateDigest %q",
				ErrInvalidState, state.ResolvedCommit, state.TemplateDigest)
		}
		if state.RequestedRef == "" {
			return fmt.Errorf("%w: requestedRef is empty", ErrInvalidState)
		}
		if _, err := semver.NewVersion(strings.TrimSpace(state.RequestedRef)); err != nil {
			return fmt.Errorf("%w: requestedRef %q is not a semantic version: %w", ErrInvalidState, state.RequestedRef, err)
		}
	default:
		return fmt.Errorf("%w: unknown sourceKind %q", ErrInvalidState, state.SourceKind)
	}
	if err := engine.ValidateField(state.EngineVersion); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidState, err)
	}
	if state.TemplateLicense == "" {
		return fmt.Errorf("%w: templateLicense is empty", ErrInvalidState)
	}
	switch state.Conflict {
	case "", "inline", "rej":
	default:
		return fmt.Errorf("%w: unknown conflict mode %q", ErrInvalidState, state.Conflict)
	}
	return nil
}

func LoadLock(fsys fs.FS) (*Lock, error) {
	data, err := fs.ReadFile(fsys, LockFileName)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", LockFileName, err)
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", LockFileName, err)
	}
	if err := ValidateLock(&lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func SaveLock(dir string, lock *Lock) error {
	lock.SchemaVersion = StateVersion
	lock.Schema = LockSchemaRef
	bumped, err := engine.Bump(lock.EngineVersion, engine.Current())
	if err != nil {
		return err
	}
	lock.EngineVersion = bumped
	slices.SortFunc(lock.Files, func(a, b LockFile) int { return cmp.Compare(a.Path, b.Path) })
	lock.ManifestHash = ComputeManifestHash(lock.Files)
	if err := ValidateLock(lock); err != nil {
		return err
	}
	if err := writeGeneratedSchema(dir, LockSchemaFileName, LockJSONSchema()); err != nil {
		return err
	}
	return writeJSON(dir, LockFileName, lock)
}

func ValidateLock(lock *Lock) error {
	if lock == nil {
		return fmt.Errorf("%w: lock is nil", ErrInvalidLock)
	}
	if lock.SchemaVersion != StateVersion {
		return fmt.Errorf(
			"%w: unsupported schema_version %d: this engine reads v%d only",
			ErrInvalidLock, lock.SchemaVersion, StateVersion)
	}
	if err := engine.ValidateField(lock.EngineVersion); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLock, err)
	}
	if lock.Source == "" {
		return fmt.Errorf("%w: source is empty", ErrInvalidLock)
	}
	if lock.Registry != "" {
		if !isRegistryURI(lock.Registry) {
			return fmt.Errorf("%w: registry %q is not a URI", ErrInvalidLock, lock.Registry)
		}
		if !isURITemplateRef(lock.Source, lock.Registry) {
			return fmt.Errorf("%w: source %q is not a <registry-uri>/<template>@<version> ref", ErrInvalidLock, lock.Source)
		}
		if !hex64.MatchString(lock.ResolvedSHA) && !manifestHashRe.MatchString(lock.ResolvedSHA) {
			return fmt.Errorf(
				"%w: resolved_sha %q is not a 64-hex digest or sha256: digest",
				ErrInvalidLock, lock.ResolvedSHA)
		}
		if !manifestHashRe.MatchString(lock.Digest) {
			return fmt.Errorf("%w: digest %q is not a sha256: digest", ErrInvalidLock, lock.Digest)
		}
		if strings.TrimSpace(lock.URI) == "" {
			return fmt.Errorf("%w: uri is empty", ErrInvalidLock)
		}
		if strings.TrimSpace(lock.Ref) == "" {
			return fmt.Errorf("%w: ref is empty", ErrInvalidLock)
		}
	} else if !hex40.MatchString(lock.ResolvedSHA) && !hex64.MatchString(lock.ResolvedSHA) {
		return fmt.Errorf(
			"%w: resolved_sha %q is not a 40-hex commit "+"SHA or 64-hex content identity",
			ErrInvalidLock, lock.ResolvedSHA)
	}
	if !manifestHashRe.MatchString(lock.ManifestHash) {
		return fmt.Errorf("%w: manifest_hash %q is not a sha256: digest", ErrInvalidLock, lock.ManifestHash)
	}
	if !slices.IsSortedFunc(lock.Files, func(a, b LockFile) int { return cmp.Compare(a.Path, b.Path) }) {
		return fmt.Errorf("%w: files are not sorted by path", ErrInvalidLock)
	}
	for i, file := range lock.Files {
		if file.Path == "" {
			return fmt.Errorf("%w: files[%d] is missing path", ErrInvalidLock, i)
		}
		if !hex64.MatchString(file.SHA256) {
			return fmt.Errorf("%w: files[%d] sha256 %q is not a 64-hex digest", ErrInvalidLock, i, file.SHA256)
		}
		if file.Bytes < 0 {
			return fmt.Errorf("%w: files[%d] has negative bytes %d", ErrInvalidLock, i, file.Bytes)
		}
	}
	return nil
}

func FileEntry(path string, data []byte) LockFile {
	sum := sha256.Sum256(data)
	return LockFile{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
}

func ComputeManifestHash(files []LockFile) string {
	sorted := make([]LockFile, len(files))
	copy(sorted, files)
	slices.SortFunc(sorted, func(a, b LockFile) int { return cmp.Compare(a.Path, b.Path) })
	sum := sha256.New()
	for _, file := range sorted {
		_, _ = fmt.Fprintf(sum, "%s:%s:%d\n", file.Path, file.SHA256, file.Bytes)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

func LoadManifest(fsys fs.FS) (*Manifest, error) {
	name := ManifestFileName
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		name = ManifestFileNameAlt
		data, err = fs.ReadFile(fsys, name)
		if err != nil {
			return &Manifest{}, nil
		}
	}
	var manifest Manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", name, err)
	}
	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// ValidateManifest reports whether the template manifest is well-formed.
// ~keep
func ValidateManifest(manifest *Manifest) error {
	if manifest == nil {
		return fmt.Errorf("%w: manifest is nil", ErrInvalidManifest)
	}
	if manifest.SchemaVersion != StateVersion {
		return fmt.Errorf(
			"%w: unsupported schema_version %d: this engine reads v%d only",
			ErrInvalidManifest, manifest.SchemaVersion, StateVersion)
	}
	if err := engine.ValidateField(manifest.EngineVersion); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidManifest, err)
	}
	switch manifest.Conflict {
	case "", "inline", "rej":
	default:
		return fmt.Errorf("%w: unknown conflict mode %q", ErrInvalidManifest, manifest.Conflict)
	}
	if err := filter.ValidateGlobs(manifest.Includes); err != nil {
		return fmt.Errorf("%w: bad includes pattern: %w", ErrInvalidManifest, err)
	}
	if err := filter.ValidateGlobs(manifest.Excludes); err != nil {
		return fmt.Errorf("%w: bad excludes pattern: %w", ErrInvalidManifest, err)
	}
	if err := filter.ValidateGlobs(manifest.Preserves); err != nil {
		return fmt.Errorf("%w: bad preserves pattern: %w", ErrInvalidManifest, err)
	}
	if err := validateManifestInputs(manifest.Inputs); err != nil {
		return err
	}
	if err := validateManifestConditionals(manifest.Conditionals); err != nil {
		return err
	}
	if err := validateManifestLicense(manifest.License); err != nil {
		return err
	}
	if err := validateManifestLicenseFiles(manifest.LicenseFiles); err != nil {
		return err
	}
	if err := validateManifestDocFiles(manifest.DocFiles); err != nil {
		return err
	}
	if err := validateManifestPlantedOverlap(manifest.LicenseFiles, manifest.DocFiles); err != nil {
		return err
	}
	return nil
}

// SaveManifestFile stamps schema_version, the relative $schema reference and
// the running engine_version, validates, regenerates the template schema
// beside the registry root and writes template.yml. Manifest changes flow
// through reflection so the schema tracks the struct with no manual edit.
// ~keep
func SaveManifestFile(templateYMLPath, schemaPath string, manifest *Manifest) error {
	manifest.SchemaVersion = StateVersion
	bumped, err := engine.Bump(manifest.EngineVersion, engine.Current())
	if err != nil {
		return err
	}
	manifest.EngineVersion = bumped
	ref, err := TemplateSchemaRefFor(templateYMLPath, schemaPath)
	if err != nil {
		return fmt.Errorf("resolving template schema ref: %w", err)
	}
	manifest.Schema = ref
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	if err := writeSchemaFile(schemaPath, ManifestJSONSchema()); err != nil {
		return err
	}
	return writeYAMLFile(templateYMLPath, manifest)
}

var manifestLicenseRe = regexp.MustCompile(`^[A-Za-z0-9.+:\-]+$`)

func validateManifestLicense(license string) error {
	if license == "" {
		return nil
	}
	trimmed := strings.TrimSpace(license)
	if trimmed == "" || !manifestLicenseRe.MatchString(trimmed) {
		return fmt.Errorf(
			"%w: bad license %q: must be a non-empty SPDX identifier "+
				"(letters, digits, and .+:-)", ErrInvalidManifest, license)
	}
	return nil
}

func validateManifestLicenseFiles(files []string) error {
	return validateManifestPlantedFiles(files, "license-files")
}

func validateManifestDocFiles(files []string) error {
	return validateManifestPlantedFiles(files, "doc-files")
}

func validateManifestPlantedFiles(files []string, key string) error {
	seen := map[string]bool{}
	for i, entry := range files {
		if strings.TrimSpace(entry) == "" {
			return fmt.Errorf("%w: %s[%d] is empty", ErrInvalidManifest, key, i)
		}
		slash := filter.ToSlash(entry)
		if strings.HasPrefix(slash, "/") || filepath.IsAbs(entry) {
			return fmt.Errorf("%w: %s[%d] %q is not relative", ErrInvalidManifest, key, i, entry)
		}
		for segment := range strings.SplitSeq(slash, "/") {
			if segment == ".." {
				return fmt.Errorf("%w: %s[%d] %q escapes its root", ErrInvalidManifest, key, i, entry)
			}
		}
		if strings.HasSuffix(slash, ".tmpl") {
			return fmt.Errorf("%w: %s[%d] %q must not be a template", ErrInvalidManifest, key, i, entry)
		}
		dest := ".config/turutan/" + slash
		if dest == ".config/turutan/state.yml" ||
			dest == ".config/turutan/lock.json" ||
			dest == ".config/turutan/templates" ||
			strings.HasPrefix(dest, ".config/turutan/templates/") {
			return fmt.Errorf(
				"%w: %s[%d] %q collides with state artifacts", ErrInvalidManifest, key, i, entry)
		}
		if seen[slash] {
			return fmt.Errorf("%w: %s[%d] %q is a duplicate", ErrInvalidManifest, key, i, entry)
		}
		seen[slash] = true
	}
	return nil
}

func validateManifestPlantedOverlap(licenseFiles, docFiles []string) error {
	licensed := map[string]bool{}
	for _, entry := range licenseFiles {
		licensed[filter.ToSlash(entry)] = true
	}
	for i, entry := range docFiles {
		if licensed[filter.ToSlash(entry)] {
			return fmt.Errorf(
				"%w: doc-files[%d] %q is also listed in license-files", ErrInvalidManifest, i, entry)
		}
	}
	return nil
}

func validateManifestInputs(inputs map[string]ManifestInput) error {
	for key, input := range inputs {
		if key == "" {
			return fmt.Errorf("%w: input with empty key", ErrInvalidManifest)
		}
		if strings.TrimSpace(input.Label) == "" {
			return fmt.Errorf("%w: input %q is missing label", ErrInvalidManifest, key)
		}
		switch input.Type {
		case "", "string", "array", "bool":
		default:
			return fmt.Errorf("%w: input %q has unknown type %q", ErrInvalidManifest, key, input.Type)
		}
		for _, opt := range input.Options {
			if opt == "" {
				return fmt.Errorf("%w: input %q has an empty option", ErrInvalidManifest, key)
			}
		}
		if len(input.Options) > 0 && input.Type == "bool" {
			return fmt.Errorf("%w: input %q options not meaningful for bool type", ErrInvalidManifest, key)
		}
	}
	return nil
}

func validateManifestConditionals(conds []ManifestConditional) error {
	for i, cond := range conds {
		if strings.TrimSpace(cond.If) == "" {
			return fmt.Errorf("%w: conditionals[%d] is missing if", ErrInvalidManifest, i)
		}
		if len(cond.Includes) == 0 && len(cond.Excludes) == 0 && len(cond.Preserves) == 0 {
			return fmt.Errorf(
				"%w: conditionals[%d] has neither includes nor excludes nor preserves: "+"must list at least one pattern", ErrInvalidManifest, i)
		}
		if err := filter.ValidateGlobs(cond.Includes); err != nil {
			return fmt.Errorf("%w: bad conditional includes pattern: %w", ErrInvalidManifest, err)
		}
		if err := filter.ValidateGlobs(cond.Excludes); err != nil {
			return fmt.Errorf("%w: bad conditional excludes pattern: %w", ErrInvalidManifest, err)
		}
		if err := filter.ValidateGlobs(cond.Preserves); err != nil {
			return fmt.Errorf("%w: bad conditional preserves pattern: %w", ErrInvalidManifest, err)
		}
	}
	return nil
}

// DiffInputs compares stored data against a new manifest and reports input
// keys the project has never answered. Sync uses it to ask only for
// genuinely new inputs instead of replaying every prompt on each sync.
// ~keep
func DiffInputs(oldData map[string]any, newManifest *Manifest) []string {
	if newManifest == nil {
		return nil
	}
	view := make(map[string]any, len(oldData)+1)
	for key, val := range oldData {
		view[key] = val
	}
	if _, ok := view["profiles"]; !ok {
		view["profiles"] = []any{}
	}
	var out []string
	for key, def := range newManifest.Inputs {
		if oldData != nil {
			if _, ok := oldData[key]; ok {
				continue
			}
		}
		if strings.TrimSpace(def.If) != "" {
			ask, err := condition.EvaluatePredicate(def.If, view)
			if err == nil && !ask {
				continue
			}
		}
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func isRegistryURI(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "fs:")
}

func isURITemplateRef(template, registryURI string) bool {
	tmpl := strings.TrimSpace(template)
	reg := strings.TrimSuffix(strings.TrimSpace(registryURI), "/")
	if tmpl == "" || reg == "" {
		return false
	}
	if !isRegistryURI(tmpl) {
		return false
	}
	if !strings.Contains(tmpl, "@") {
		return false
	}
	return strings.HasPrefix(tmpl, reg+"/")
}

func writeJSON(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %q: %w", name, err)
	}
	data = append(data, '\n')
	parent := filepath.Dir(filepath.Join(dir, name))
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", name, err)
	}
	return nil
}

func writeYAML(dir, name string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding %q: %w", name, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	parent := filepath.Dir(filepath.Join(dir, name))
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", name, err)
	}
	return nil
}

func writeYAMLFile(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding %q: %w", path, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", path, err)
	}
	return nil
}

func writeSchemaFile(path string, schema map[string]any) error {
	return writeGeneratedSchema(filepath.Dir(path), filepath.Base(path), schema)
}
