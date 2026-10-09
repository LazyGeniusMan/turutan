// SPDX-License-Identifier: Apache-2.0

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

// State/lock/manifest file names and schema constants.
const (
	// StateFileName is the project state file (spec §4.1).
	StateFileName = ".turutan.json"
	// LockFileName is the reproducibility lock file (spec §4.2).
	LockFileName = ".turutan.lock"
	// ManifestFileName is the template manifest name (spec §4.3).
	ManifestFileName = ".turutan.yml"
	// ManifestFileNameAlt is the accepted long-extension alias.
	ManifestFileNameAlt = ".turutan.yaml"
	// StateVersion is the current state/lock schema version; breaking
	// changes bump it with an explicit migration error.
	StateVersion = 1
	// TemplateLicenseMIT0 is the SPDX identifier recorded for M1
	// scaffolding output (spec §8.4: generated projects are MIT-0).
	TemplateLicenseMIT0 = "MIT-0"
)

// hex40 matches a 40-hex git commit SHA.
var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// hex64 matches a 64-hex sha256 digest (filesystem identity).
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// State is the .turutan.json project state (spec §4.1): origin,
// resolved commit, answers and provenance for later updates.
type State struct {
	Version         int            `json:"version"`
	Template        string         `json:"template"`
	SourceKind      string         `json:"sourceKind"`
	Subpath         string         `json:"subpath,omitempty"`
	RequestedRef    string         `json:"requestedRef,omitempty"`
	ResolvedCommit  string         `json:"resolvedCommit"`
	Answers         map[string]any `json:"answers,omitempty"`
	Skip            []string       `json:"skip,omitempty"`
	Engine          string         `json:"engine"`
	TemplateLicense string         `json:"templateLicense"`
}

// LockFile is one rendered file entry in the lock manifest.
type LockFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

// Lock is the .turutan.lock reproducibility record (spec §4.2).
// Files is always sorted by Path.
type Lock struct {
	Source       string     `json:"source"`
	Ref          string     `json:"ref,omitempty"`
	ResolvedSHA  string     `json:"resolved_sha"`
	Subpath      string     `json:"subpath,omitempty"`
	ManifestHash string     `json:"manifest_hash"`
	Files        []LockFile `json:"files"`
}

// ManifestHooks lists template-declared hook scripts. M1 only gates on
// their presence (consent); execution arrives later.
type ManifestHooks struct {
	Pre  []string `yaml:"pre"`
	Post []string `yaml:"post"`
}

// ManifestMigration is one ordered update migration (spec §§6.4,7):
// From optionally constrains the old identity the migration applies to
// (a commit prefix or semver range; empty means always apply), and Run
// lists shell commands executed in order in the project directory.
type ManifestMigration struct {
	From string   `yaml:"from"`
	Run  []string `yaml:"run"`
}

// Manifest is the .turutan.yml template manifest (spec §4.3).
type Manifest struct {
	Source     string              `yaml:"source"`
	MinEngine  string              `yaml:"min-engine"`
	Ignore     []string            `yaml:"ignore"`
	Preserve   []string            `yaml:"preserve"`
	Conflict   string              `yaml:"conflict"`
	Hooks      ManifestHooks       `yaml:"hooks"`
	Migrations []ManifestMigration `yaml:"migrations"`
}

// LoadState reads and validates .turutan.json from fsys (os.DirFS of the
// project dir in production, fstest.MapFS in tests).
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

// SaveState validates then writes .turutan.json into dir.
func SaveState(dir string, state *State) error {
	if err := ValidateState(state); err != nil {
		return err
	}
	return writeJSON(dir, StateFileName, state)
}

// ValidateState rejects unknown schema versions and missing identity.
// Git kinds require a 40-hex resolved commit; filesystem sources carry a
// 64-hex content identity instead (no commits exist to record).
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
	return nil
}

// LoadLock reads and validates .turutan.lock from fsys.
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

// SaveLock sorts files by path, stamps the manifest hash, validates, then
// writes .turutan.lock into dir.
func SaveLock(dir string, lock *Lock) error {
	sort.Slice(lock.Files, func(i, j int) bool { return lock.Files[i].Path < lock.Files[j].Path })
	lock.ManifestHash = ComputeManifestHash(lock.Files)
	if err := ValidateLock(lock); err != nil {
		return err
	}
	return writeJSON(dir, LockFileName, lock)
}

// ValidateLock rejects locks with missing identity or unsorted files.
func ValidateLock(lock *Lock) error {
	if lock.Source == "" {
		return fmt.Errorf("invalid lock: source is empty")
	}
	if lock.ResolvedSHA == "" {
		return fmt.Errorf("invalid lock: resolved_sha is empty")
	}
	if !sort.SliceIsSorted(lock.Files, func(i, j int) bool { return lock.Files[i].Path < lock.Files[j].Path }) {
		return fmt.Errorf("invalid lock: files are not sorted by path")
	}
	for _, file := range lock.Files {
		if file.Path == "" || file.SHA256 == "" {
			return fmt.Errorf("invalid lock: file entry %+v is missing path or sha256", file)
		}
	}
	return nil
}

// FileEntry builds a LockFile entry for path with content data.
func FileEntry(path string, data []byte) LockFile {
	sum := sha256.Sum256(data)
	return LockFile{Path: path, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
}

// ComputeManifestHash folds the sorted file entries into one
// "sha256:…" digest for drift detection.
func ComputeManifestHash(files []LockFile) string {
	sorted := make([]LockFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	sum := sha256.New()
	for _, file := range sorted {
		fmt.Fprintf(sum, "%s:%s:%d\n", file.Path, file.SHA256, file.Bytes)
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

// LoadManifest reads .turutan.yml (or .turutan.yaml) from the template
// root fsys. An absent manifest is not an error: it means defaults (no
// ignore/preserve, no engine floor, no hooks).
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

// ValidateManifest rejects unknown conflict modes, unparsable
// min-engine constraints, empty migration commands and glob patterns
// that could reach outside the subpath root (see filter.ValidateGlobs).
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
		if slices.Contains(migration.Run, "") {
			return fmt.Errorf("invalid manifest: migrations[%d] has an empty run command", i)
		}
	}
	return nil
}

// writeJSON marshals value with two-space indent into dir/name.
func writeJSON(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %q: %w", name, err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return fmt.Errorf("writing %q: %w", name, err)
	}
	return nil
}
