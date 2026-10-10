// SPDX-License-Identifier: Apache-2.0

package config

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

const (
	StateFileName       = ".turutan.json"
	LockFileName        = ".turutan.lock"
	ManifestFileName    = ".turutan.yml"
	ManifestFileNameAlt = ".turutan.yaml"
	StateVersion        = 1
	TemplateLicenseMIT0 = "MIT-0"
)

var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

var hexPrefix = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

var manifestHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type State struct {
	Version         int            `json:"version"`
	Template        string         `json:"template"`
	SourceKind      string         `json:"sourceKind"`
	Subpath         string         `json:"subpath,omitempty"`
	RequestedRef    string         `json:"requestedRef,omitempty"`
	ResolvedCommit  string         `json:"resolvedCommit"`
	Answers         map[string]any `json:"answers,omitempty"`
	Skip            []string       `json:"skip,omitempty"`
	Conflict        string         `json:"conflict,omitempty"`
	Engine          string         `json:"engine"`
	TemplateLicense string         `json:"templateLicense"`
}

type LockFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Lock struct {
	Source       string     `json:"source"`
	Ref          string     `json:"ref,omitempty"`
	ResolvedSHA  string     `json:"resolved_sha"`
	Subpath      string     `json:"subpath,omitempty"`
	ManifestHash string     `json:"manifest_hash"`
	Files        []LockFile `json:"files"`
}

type ManifestHooks struct {
	Pre  []string `yaml:"pre"`
	Post []string `yaml:"post"`
}

type ManifestMigration struct {
	From string   `yaml:"from"`
	Run  []string `yaml:"run"`
}

type Manifest struct {
	Source     string              `yaml:"source"`
	MinEngine  string              `yaml:"min-engine"`
	Ignore     []string            `yaml:"ignore"`
	Preserve   []string            `yaml:"preserve"`
	Conflict   string              `yaml:"conflict"`
	Hooks      ManifestHooks       `yaml:"hooks"`
	Migrations []ManifestMigration `yaml:"migrations"`
}

func LoadState(fsys fs.FS) (*State, error) {
	data, err := fs.ReadFile(fsys, StateFileName)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", StateFileName, err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", StateFileName, err)
	}
	if err := ValidateState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveState(dir string, state *State) error {
	if err := ValidateState(state); err != nil {
		return err
	}
	return writeJSON(dir, StateFileName, state)
}

func ValidateState(state *State) error {
	if state.Version != StateVersion {
		return fmt.Errorf("unsupported state version %d: this engine reads v%d only", state.Version, StateVersion)
	}
	if state.Template == "" {
		return fmt.Errorf("invalid state: template is empty")
	}
	switch state.SourceKind {
	case "remote-git", "local-git":
		if !hex40.MatchString(state.ResolvedCommit) {
			return fmt.Errorf("invalid state: resolvedCommit %q is not a 40-hex commit SHA", state.ResolvedCommit)
		}
	case "filesystem":
		if !hex64.MatchString(state.ResolvedCommit) {
			return fmt.Errorf("invalid state: resolvedCommit %q is not a content identity", state.ResolvedCommit)
		}
	default:
		return fmt.Errorf("invalid state: unknown sourceKind %q", state.SourceKind)
	}
	if state.Engine == "" {
		return fmt.Errorf("invalid state: engine is empty")
	}
	if state.TemplateLicense == "" {
		return fmt.Errorf("invalid state: templateLicense is empty")
	}
	switch state.Conflict {
	case "", "inline", "rej":
	default:
		return fmt.Errorf("invalid state: unknown conflict mode %q", state.Conflict)
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
	slices.SortFunc(lock.Files, func(a, b LockFile) int { return cmp.Compare(a.Path, b.Path) })
	lock.ManifestHash = ComputeManifestHash(lock.Files)
	if err := ValidateLock(lock); err != nil {
		return err
	}
	return writeJSON(dir, LockFileName, lock)
}

func ValidateLock(lock *Lock) error {
	if lock == nil {
		return fmt.Errorf("invalid lock: lock is nil")
	}
	if lock.Source == "" {
		return fmt.Errorf("invalid lock: source is empty")
	}
	if !hex40.MatchString(lock.ResolvedSHA) && !hex64.MatchString(lock.ResolvedSHA) {
		return fmt.Errorf(
			"invalid lock: resolved_sha %q is not a 40-hex commit "+"SHA or 64-hex content identity",
			lock.ResolvedSHA)
	}
	if !manifestHashRe.MatchString(lock.ManifestHash) {
		return fmt.Errorf("invalid lock: manifest_hash %q is not a sha256: digest", lock.ManifestHash)
	}
	if !slices.IsSortedFunc(lock.Files, func(a, b LockFile) int { return cmp.Compare(a.Path, b.Path) }) {
		return fmt.Errorf("invalid lock: files are not sorted by path")
	}
	for i, file := range lock.Files {
		if file.Path == "" {
			return fmt.Errorf("invalid lock: files[%d] is missing path", i)
		}
		if !hex64.MatchString(file.SHA256) {
			return fmt.Errorf("invalid lock: files[%d] sha256 %q is not a 64-hex digest", i, file.SHA256)
		}
		if file.Bytes < 0 {
			return fmt.Errorf("invalid lock: files[%d] has negative bytes %d", i, file.Bytes)
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
		fmt.Fprintf(sum, "%s:%s:%d\n", file.Path, file.SHA256, file.Bytes)
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
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", name, err)
	}
	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func ValidateManifest(manifest *Manifest) error {
	switch manifest.Conflict {
	case "", "inline", "rej":
	default:
		return fmt.Errorf("invalid manifest: unknown conflict mode %q", manifest.Conflict)
	}
	if err := filter.ValidateGlobs(manifest.Ignore); err != nil {
		return fmt.Errorf("invalid manifest: bad ignore pattern: %w", err)
	}
	if err := filter.ValidateGlobs(manifest.Preserve); err != nil {
		return fmt.Errorf("invalid manifest: bad preserve pattern: %w", err)
	}
	if manifest.MinEngine != "" {
		if _, err := semver.NewConstraint(manifest.MinEngine); err != nil {
			return fmt.Errorf("invalid manifest: bad min-engine %q: %w", manifest.MinEngine, err)
		}
	}
	for i, migration := range manifest.Migrations {
		if len(migration.Run) == 0 {
			return fmt.Errorf("invalid manifest: migrations[%d] has empty run: must list at least one command", i)
		}
		if slices.Contains(migration.Run, "") {
			return fmt.Errorf("invalid manifest: migrations[%d] has an empty run command", i)
		}
		if err := validateMigrationFrom(migration.From); err != nil {
			return fmt.Errorf("invalid manifest: migrations[%d]: %w", i, err)
		}
	}
	return nil
}

func validateMigrationFrom(from string) error {
	if from == "" {
		return nil
	}
	if _, err := semver.NewConstraint(from); err == nil {
		return nil
	}
	if hexPrefix.MatchString(from) {
		return nil
	}
	return fmt.Errorf("bad migration from %q: must be a semver range or a 7-64 hex commit prefix", from)
}

func writeJSON(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %q: %w", name, err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		return fmt.Errorf("writing %q: %w", name, err)
	}
	return nil
}
