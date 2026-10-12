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
	"strings"

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
	AnswersSchema   int            `json:"answersSchema,omitempty"`
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

// ManifestPrompt declares a user-visible template variable and its input rules. ~keep
type ManifestPrompt struct {
	Prompt  string   `yaml:"prompt"`
	Default any      `yaml:"default"`
	Type    string   `yaml:"type"`
	Options []string `yaml:"options"`
	Help    string   `yaml:"help"`
}

// ManifestConditional skips template paths when its If expression renders truthy. ~keep
type ManifestConditional struct {
	If   string   `yaml:"if"`
	Skip []string `yaml:"skip"`
}

// ManifestProfile owns a set of template paths under one profile ID. ~keep
type ManifestProfile struct {
	Label       string   `yaml:"label"`
	Description string   `yaml:"description"`
	Files       []string `yaml:"files"`
}

type Manifest struct {
	Source       string                     `yaml:"source"`
	MinEngine    string                     `yaml:"min-engine"`
	Ignore       []string                   `yaml:"ignore"`
	Preserve     []string                   `yaml:"preserve"`
	Conflict     string                     `yaml:"conflict"`
	Hooks        ManifestHooks              `yaml:"hooks"`
	Migrations   []ManifestMigration        `yaml:"migrations"`
	Prompts      map[string]ManifestPrompt  `yaml:"prompts"`
	Conditionals []ManifestConditional      `yaml:"conditionals"`
	Renames      map[string]string          `yaml:"renames"`
	Profiles     map[string]ManifestProfile `yaml:"profiles"`
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
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", name, err)
	}
	if err := ValidateManifest(&manifest); err != nil {
		return nil, err
	}
	normalizeManifest(&manifest)
	return &manifest, nil
}

// normalizeManifest synthesizes Conditionals from Profiles so Profiles
// remains the single source of truth for profile-owned paths. ~keep
func normalizeManifest(manifest *Manifest) {
	if manifest == nil || len(manifest.Profiles) == 0 {
		return
	}
	ids := make([]string, 0, len(manifest.Profiles))
	for id := range manifest.Profiles {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if hasConditionalForProfile(manifest.Conditionals, id) {
			continue
		}
		files := append([]string(nil), manifest.Profiles[id].Files...)
		manifest.Conditionals = append(manifest.Conditionals, ManifestConditional{
			If:   fmt.Sprintf(`{{ not (contains .enabled_profiles "%s") }}`, id),
			Skip: files,
		})
	}
}

func hasConditionalForProfile(conds []ManifestConditional, id string) bool {
	needle := `"` + id + `"`
	for _, cond := range conds {
		if strings.Contains(cond.If, needle) {
			return true
		}
	}
	return false
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
	if err := validateManifestPrompts(manifest.Prompts); err != nil {
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

func validateManifestPrompts(prompts map[string]ManifestPrompt) error {
	for key, prompt := range prompts {
		if key == "" {
			return fmt.Errorf("invalid manifest: prompt with empty key")
		}
		switch prompt.Type {
		case "", "string", "array", "bool":
		default:
			return fmt.Errorf("invalid manifest: prompt %q has unknown type %q", key, prompt.Type)
		}
		for _, opt := range prompt.Options {
			if opt == "" {
				return fmt.Errorf("invalid manifest: prompt %q has an empty option", key)
			}
		}
		if len(prompt.Options) > 0 && prompt.Type == "bool" {
			return fmt.Errorf("invalid manifest: prompt %q options not meaningful for bool type", key)
		}
	}
	return nil
}

func validateManifestConditionals(conds []ManifestConditional) error {
	for i, cond := range conds {
		if len(cond.Skip) > 0 && cond.If == "" {
			return fmt.Errorf("invalid manifest: conditionals[%d] has skip without if", i)
		}
		if err := filter.ValidateGlobs(cond.Skip); err != nil {
			return fmt.Errorf("invalid manifest: bad conditional skip pattern: %w", err)
		}
	}
	return nil
}

func validateManifestProfiles(profiles map[string]ManifestProfile) error {
	for id, profile := range profiles {
		if id == "" {
			return fmt.Errorf("invalid manifest: profile with empty id")
		}
		if err := filter.ValidateGlobs(profile.Files); err != nil {
			return fmt.Errorf("invalid manifest: bad profile %q files pattern: %w", id, err)
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

// GetProfiles derives enabled profile IDs from answers["enabled_profiles"]. ~keep
func GetProfiles(answers map[string]any) []string {
	if answers == nil {
		return nil
	}
	val, ok := answers["enabled_profiles"]
	if !ok || val == nil {
		return nil
	}
	var raw []string
	switch v := val.(type) {
	case []string:
		raw = v
	case []any:
		for _, el := range v {
			raw = append(raw, strings.Split(fmt.Sprint(el), ",")...)
		}
	case string:
		raw = strings.Split(v, ",")
	default:
		raw = strings.Split(strings.TrimSpace(fmt.Sprint(val)), ",")
	}
	var out []string
	seen := map[string]bool{}
	for _, el := range raw {
		trimmed := strings.TrimSpace(el)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

// GetProfiles returns stored profiles, falling back to answers. ~keep
func (s *State) GetProfiles() []string {
	if s == nil {
		return nil
	}
	if len(s.Profiles) > 0 {
		out := make([]string, len(s.Profiles))
		copy(out, s.Profiles)
		return out
	}
	return GetProfiles(s.Answers)
}

// ProfilesForPath lists enabled profiles owning path via files globs. ~keep
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
		for _, pattern := range profile.Files {
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

// ResolveRenames follows old through renames chains to its current path. ~keep
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

// ProfileDiff reports template-side profile and prompt additions. ~keep
type ProfileDiff struct {
	Added      []string
	Removed    []string
	NewPrompts []string
}

// DiffProfiles compares stored answers against a new manifest. ~keep
func DiffProfiles(oldAnswers map[string]any, newManifest *Manifest) ProfileDiff {
	var diff ProfileDiff
	if newManifest == nil {
		return diff
	}
	oldSet := map[string]bool{}
	for _, id := range GetProfiles(oldAnswers) {
		oldSet[id] = true
	}
	newSet := map[string]bool{"core": true}
	for id := range newManifest.Profiles {
		newSet[id] = true
	}
	for id := range newSet {
		if !oldSet[id] {
			diff.Added = append(diff.Added, id)
		}
	}
	for id := range oldSet {
		if !newSet[id] {
			diff.Removed = append(diff.Removed, id)
		}
	}
	slices.Sort(diff.Added)
	slices.Sort(diff.Removed)
	for key := range newManifest.Prompts {
		if oldAnswers == nil {
			diff.NewPrompts = append(diff.NewPrompts, key)
			continue
		}
		if _, ok := oldAnswers[key]; !ok {
			diff.NewPrompts = append(diff.NewPrompts, key)
		}
	}
	slices.Sort(diff.NewPrompts)
	return diff
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
