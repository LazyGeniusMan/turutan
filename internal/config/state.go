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
	"github.com/LazyGeniusMan/turutan/internal/filter"
)

const (
	StateFileName       = ".config/turutan/state.json"
	LockFileName        = ".config/turutan/lock.json"
	ManifestFileName    = ".turutan.yml"
	ManifestFileNameAlt = ".turutan.yaml"
	StateVersion        = 1
	TemplateLicenseMIT0 = "MIT-0"
)

const (
	legacyStateFileName = ".turutan.json"
	legacyLockFileName  = ".turutan.lock"
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
	Data            map[string]any `json:"data,omitempty"`
	DataSchema      int            `json:"dataSchema,omitempty"`
	Profiles        []string       `json:"profiles,omitempty"`
	Skip            []string       `json:"skip,omitempty"`
	Conflict        string         `json:"conflict,omitempty"`
	Engine          string         `json:"engine"`
	TemplateLicense string         `json:"templateLicense"`
}

type LockFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Profile string `json:"profile,omitempty"`
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
	If       string   `yaml:"if"`
	Includes []string `yaml:"includes"`
	Excludes []string `yaml:"excludes"`
}

// ManifestProfile describes one installable feature slice of a template.
// Templates use profiles to ship optional file sets behind a single opt-in
// id so projects only render the variants selected via native profiles.
// ~keep
type ManifestProfile struct {
	Label       string   `yaml:"label"`
	Description string   `yaml:"description"`
	Includes    []string `yaml:"includes"`
	Excludes    []string `yaml:"excludes"`
}

type Manifest struct {
	Source       string                     `yaml:"source"`
	MinEngine    string                     `yaml:"min-engine"`
	License      string                     `yaml:"license"`
	LicenseFiles []string                   `yaml:"license-files"`
	DocFiles     []string                   `yaml:"doc-files"`
	Ignore       []string                   `yaml:"ignore"`
	Preserve     []string                   `yaml:"preserve"`
	Conflict     string                     `yaml:"conflict"`
	Hooks        ManifestHooks              `yaml:"hooks"`
	Migrations   []ManifestMigration        `yaml:"migrations"`
	Inputs       map[string]ManifestInput   `yaml:"inputs"`
	Conditionals []ManifestConditional      `yaml:"conditionals"`
	Renames      map[string]string          `yaml:"renames"`
	Profiles     map[string]ManifestProfile `yaml:"profiles"`
}

func LoadState(fsys fs.FS) (*State, error) {
	data, err := readStateFile(fsys, StateFileName, legacyStateFileName)
	if err != nil {
		return nil, err
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
	state.Version = StateVersion
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
	data, err := readStateFile(fsys, LockFileName, legacyLockFileName)
	if err != nil {
		return nil, err
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
	if err := validateManifestInputs(manifest.Inputs); err != nil {
		return err
	}
	if err := validateManifestConditionals(manifest.Conditionals); err != nil {
		return err
	}
	if err := validateManifestProfiles(manifest.Profiles); err != nil {
		return err
	}
	if err := validateManifestRenames(manifest.Renames); err != nil {
		return err
	}
	if manifest.MinEngine != "" {
		if _, err := semver.NewConstraint(manifest.MinEngine); err != nil {
			return fmt.Errorf("invalid manifest: bad min-engine %q: %w", manifest.MinEngine, err)
		}
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

var manifestLicenseRe = regexp.MustCompile(`^[A-Za-z0-9.+:\-]+$`)

func validateManifestLicense(license string) error {
	if license == "" {
		return nil
	}
	trimmed := strings.TrimSpace(license)
	if trimmed == "" || !manifestLicenseRe.MatchString(trimmed) {
		return fmt.Errorf(
			"invalid manifest: bad license %q: must be a non-empty SPDX identifier "+
				"(letters, digits, and .+:-)", license)
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
			return fmt.Errorf("invalid manifest: %s[%d] is empty", key, i)
		}
		slash := filter.ToSlash(entry)
		if strings.HasPrefix(slash, "/") || filepath.IsAbs(entry) {
			return fmt.Errorf("invalid manifest: %s[%d] %q is not relative", key, i, entry)
		}
		for segment := range strings.SplitSeq(slash, "/") {
			if segment == ".." {
				return fmt.Errorf("invalid manifest: %s[%d] %q escapes its root", key, i, entry)
			}
		}
		if strings.HasSuffix(slash, ".tmpl") {
			return fmt.Errorf("invalid manifest: %s[%d] %q must not be a template", key, i, entry)
		}
		dest := ".config/turutan/" + slash
		if dest == ".config/turutan/state.json" ||
			dest == ".config/turutan/lock.json" ||
			dest == ".config/turutan/templates" ||
			strings.HasPrefix(dest, ".config/turutan/templates/") {
			return fmt.Errorf(
				"invalid manifest: %s[%d] %q collides with state artifacts", key, i, entry)
		}
		if seen[slash] {
			return fmt.Errorf("invalid manifest: %s[%d] %q is a duplicate", key, i, entry)
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
				"invalid manifest: doc-files[%d] %q is also listed in license-files", i, entry)
		}
	}
	return nil
}

func validateManifestInputs(inputs map[string]ManifestInput) error {
	for key, input := range inputs {
		if key == "" {
			return fmt.Errorf("invalid manifest: input with empty key")
		}
		if strings.TrimSpace(input.Label) == "" {
			return fmt.Errorf("invalid manifest: input %q is missing label", key)
		}
		switch input.Type {
		case "", "string", "array", "bool":
		default:
			return fmt.Errorf("invalid manifest: input %q has unknown type %q", key, input.Type)
		}
		for _, opt := range input.Options {
			if opt == "" {
				return fmt.Errorf("invalid manifest: input %q has an empty option", key)
			}
		}
		if len(input.Options) > 0 && input.Type == "bool" {
			return fmt.Errorf("invalid manifest: input %q options not meaningful for bool type", key)
		}
	}
	return nil
}

func validateManifestConditionals(conds []ManifestConditional) error {
	for i, cond := range conds {
		if strings.TrimSpace(cond.If) == "" {
			return fmt.Errorf("invalid manifest: conditionals[%d] is missing if", i)
		}
		if len(cond.Includes) == 0 && len(cond.Excludes) == 0 {
			return fmt.Errorf(
				"invalid manifest: conditionals[%d] has neither includes nor excludes: "+"must list at least one pattern", i)
		}
		if err := filter.ValidateGlobs(cond.Includes); err != nil {
			return fmt.Errorf("invalid manifest: bad conditional includes pattern: %w", err)
		}
		if err := filter.ValidateGlobs(cond.Excludes); err != nil {
			return fmt.Errorf("invalid manifest: bad conditional excludes pattern: %w", err)
		}
	}
	return nil
}

func validateManifestProfiles(profiles map[string]ManifestProfile) error {
	for id, profile := range profiles {
		if id == "" {
			return fmt.Errorf("invalid manifest: profile with empty id")
		}
		if err := filter.ValidateGlobs(profile.Includes); err != nil {
			return fmt.Errorf("invalid manifest: bad profile %q includes pattern: %w", id, err)
		}
		if err := filter.ValidateGlobs(profile.Excludes); err != nil {
			return fmt.Errorf("invalid manifest: bad profile %q excludes pattern: %w", id, err)
		}
	}
	return nil
}

func validateManifestRenames(renames map[string]string) error {
	for old, new := range renames {
		if strings.TrimSpace(old) == "" || strings.TrimSpace(new) == "" {
			return fmt.Errorf("invalid manifest: rename %q -> %q must have non-empty paths", old, new)
		}
		if old == new {
			return fmt.Errorf("invalid manifest: rename %q maps to itself", old)
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

// NormalizeProfiles deduplicates ids (keeping the first occurrence),
// forces core first and custom last, injecting both when missing.
// Bootstrap and update funnel every profiles value through it so
// the always-enabled pair stays ordered no matter the selection path.
// ~keep
func NormalizeProfiles(ids []string) []string {
	seen := map[string]bool{}
	rest := []string{}
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		if trimmed == "core" || trimmed == "custom" {
			continue
		}
		rest = append(rest, trimmed)
	}
	return append(append([]string{"core"}, rest...), "custom")
}

// DefaultProfiles is the code default when no selection layer provides one.
// The manifest offers the full set via its profiles map; core stays implicit
// and NormalizeProfiles injects the always-enabled pair.
// ~keep
var DefaultProfiles = []string{"core", "ai"}

// GetProfiles reports the profiles recorded for this project.
// State carries Profiles natively; there is no fallback into data.
// ~keep
func (s *State) GetProfiles() []string {
	if s == nil || len(s.Profiles) == 0 {
		return nil
	}
	out := make([]string, len(s.Profiles))
	copy(out, s.Profiles)
	return out
}

// ProfilesForPath reports which of the given profiles include path.
// Update and diff output attribute drift to profiles, so they need the
// owning ids without reimplementing manifest glob matching each time.
// ~keep
func ProfilesForPath(manifest *Manifest, profiles []string, path string) []string {
	if manifest == nil || len(manifest.Profiles) == 0 {
		return nil
	}
	ids := profiles
	if ids == nil {
		ids = make([]string, 0, len(manifest.Profiles))
		for id := range manifest.Profiles {
			ids = append(ids, id)
		}
	}
	slash := filter.ToSlash(path)
	var matched []string
	for _, id := range ids {
		profile, ok := manifest.Profiles[id]
		if !ok {
			continue
		}
		for _, pattern := range profile.Includes {
			ok, err := filter.Match(pattern, slash)
			if err != nil {
				continue
			}
			if ok {
				matched = append(matched, id)
				break
			}
		}
	}
	slices.Sort(matched)
	return matched
}

// IsIncluded reports the layered profile decision for path.
// Files matched by zero includes across all profiles are core-always and
// return true. Otherwise start excluded and apply each enabled profile in
// order: its includes in order set included, then its excludes in order
// clear it. Only enabled profiles participate; disabled ones are ignored.
// Both slash and its .tmpl-stripped form are matched so template sources
// and rendered destinations share one decision.
// ~keep
func (m *Manifest) IsIncluded(path string, enabled []string) (bool, error) {
	if m == nil || len(m.Profiles) == 0 {
		return true, nil
	}
	slash := filter.ToSlash(path)
	stripped := strings.TrimSuffix(slash, ".tmpl")
	matchBoth := func(pattern string) (bool, error) {
		ok, err := filter.Match(pattern, slash)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
		if stripped != slash {
			ok, err := filter.Match(pattern, stripped)
			if err != nil {
				return false, err
			}
			return ok, nil
		}
		return false, nil
	}
	core := true
	for _, profile := range m.Profiles {
		for _, pattern := range profile.Includes {
			matched, err := matchBoth(pattern)
			if err != nil {
				return false, err
			}
			if matched {
				core = false
				break
			}
		}
		if !core {
			break
		}
	}
	if core {
		return true, nil
	}
	included := false
	for _, id := range enabled {
		profile, ok := m.Profiles[id]
		if !ok {
			continue
		}
		for _, pattern := range profile.Includes {
			matched, err := matchBoth(pattern)
			if err != nil {
				return false, err
			}
			if matched {
				included = true
				break
			}
		}
		for _, pattern := range profile.Excludes {
			matched, err := matchBoth(pattern)
			if err != nil {
				return false, err
			}
			if matched {
				included = false
				break
			}
		}
	}
	return included, nil
}

// ResolveRenames follows the manifest renames chain for an old path.
// Templates rename files across releases, so lock and base lookups must map
// recorded paths forward instead of reporting every rename as delete+add.
// ~keep
func ResolveRenames(old string, renames map[string]string) string {
	if len(renames) == 0 || old == "" {
		return old
	}
	cur := old
	seen := map[string]bool{cur: true}
	for range len(renames) + 1 {
		next, ok := renames[cur]
		if !ok || next == "" || next == cur {
			return cur
		}
		if seen[next] {
			return cur
		}
		seen[next] = true
		cur = next
	}
	return cur
}

// ProfileDiff summarizes how the profile and input surface changed.
// Update uses it to ask only for genuinely new inputs instead of
// replaying every prompt on each template sync.
// ~keep
type ProfileDiff struct {
	Added     []string
	Removed   []string
	NewInputs []string
}

// DiffProfiles compares previously selected profiles and stored data against a new manifest.
// Sync needs the added/removed profile ids plus unseen input keys to decide
// what to ask and what to warn about before re-rendering.
// ~keep
func DiffProfiles(oldProfiles []string, oldData map[string]any, newManifest *Manifest) ProfileDiff {
	var diff ProfileDiff
	if newManifest == nil {
		return diff
	}
	oldSet := map[string]bool{}
	for _, id := range oldProfiles {
		oldSet[strings.TrimSpace(id)] = true
	}
	newSet := map[string]bool{"core": true, "custom": true}
	for id := range newManifest.Profiles {
		newSet[id] = true
	}
	for id := range newSet {
		if id == "custom" || oldSet[id] {
			continue
		}
		diff.Added = append(diff.Added, id)
	}
	for id := range oldSet {
		if id == "custom" || newSet[id] {
			continue
		}
		diff.Removed = append(diff.Removed, id)
	}
	slices.Sort(diff.Added)
	slices.Sort(diff.Removed)
	view := inputPredicateView(oldData, oldProfiles)
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
		diff.NewInputs = append(diff.NewInputs, key)
	}
	slices.Sort(diff.NewInputs)
	return diff
}

func inputPredicateView(data map[string]any, profiles []string) map[string]any {
	view := make(map[string]any, len(data)+1)
	for key, val := range data {
		view[key] = val
	}
	view["profiles"] = NormalizeProfiles(splitProfileIDs(profiles))
	return view
}

func splitProfileIDs(ids []string) []string {
	var out []string
	for _, id := range ids {
		for _, part := range strings.Split(id, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			out = append(out, trimmed)
		}
	}
	return out
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

func readStateFile(fsys fs.FS, name, legacy string) ([]byte, error) {
	data, err := fs.ReadFile(fsys, name)
	if err == nil {
		return data, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading %q: %w", name, err)
	}
	legacyData, legacyErr := fs.ReadFile(fsys, legacy)
	if legacyErr != nil {
		return nil, fmt.Errorf("reading %q: %w", name, err)
	}
	return legacyData, nil
}
