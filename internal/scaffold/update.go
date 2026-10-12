// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/CivNode/diff3-go"

	"github.com/Masterminds/semver/v3"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

const updateDeepenDepth = 50

const maxDirtyShown = 5

type UpdateOptions struct {
	Ref            string
	Conflict       ConflictMode
	Force          bool
	AnswersFile    string
	Defaults       bool
	AllowHooks     bool
	NonInteractive bool
	Verbose        bool
	Engine         string
	Enable         []string
	Disable        []string
	Preview        bool
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
}

type UpdateResult struct {
	Old        string
	New        string
	Updated    []string
	Conflicts  []string
	Migrations []string
}

func Update(
	ctx context.Context,
	projectDir string,
	opts UpdateOptions,
) (*UpdateResult, error) {
	stdout, stderr, stdin := updateStreams(opts)
	state, lock, answers, err := loadUpdateInputs(projectDir, opts)
	if err != nil {
		return nil, err
	}
	src, wantRef, fetched, manifest, err := fetchUpdateTemplate(
		ctx, projectDir, state, opts, stderr)
	if err != nil {
		return nil, err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	if err := reconcileProfiles(answers, manifest, opts, stderr); err != nil {
		return nil, err
	}
	if !opts.Force {
		if err := guardCleanTree(projectDir, lock, manifest, answers); err != nil {
			return nil, err
		}
	}
	if opts.Preview {
		return previewUpdate(
			projectDir, state, lock, fetched, manifest, answers,
			opts, stdout, stderr, stdin)
	}
	rendered, old, fresh, merger, skipMigrations, stage, err := stageUpdate(
		projectDir, state, fetched, manifest, answers,
		opts, stdout, stderr, stdin)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	updated, conflicts, err := merger.merge(rendered, lock)
	if err != nil {
		return nil, err
	}
	var migrated []string
	if !skipMigrations {
		migrated, err = runMigrations(projectDir, manifest, old, fresh, stderr)
		if err != nil {
			return nil, err
		}
	}
	return finishUpdate(
		projectDir, state, lock, src, wantRef, answers, fresh,
		rendered, stage, manifest, stdout, updated, conflicts, migrated, old)
}

// customOwned reports whether path is owned by the always-enabled custom
// profile. Custom-owned files seed once and are never overwritten, so every
// sync path consults it before writing, reporting, or dirty-checking.
// ~keep
func customOwned(manifest *config.Manifest, answers map[string]any, path string) bool {
	if manifest == nil {
		return false
	}
	enabled := config.NormalizeProfiles(config.GetProfiles(answers))
	return slices.Contains(config.ProfilesForPath(manifest, enabled, path), "custom")
}

func filePresent(projectDir, path string) bool {
	_, err := filter.StatWithinRoot(projectDir, path)
	return err == nil
}

func (m *merger) customOwned(path string) bool {
	if m == nil || m.state == nil {
		return false
	}
	return customOwned(m.manifest, m.state.Answers, path)
}

func reconcileProfiles(
	answers map[string]any,
	manifest *config.Manifest,
	opts UpdateOptions,
	stderr io.Writer,
) error {
	diff := relevantProfileDiff(manifest, config.DiffProfiles(answers, manifest))
	if err := reportProfileDiff(stderr, diff); err != nil {
		return err
	}
	if len(opts.Enable) == 0 && len(opts.Disable) == 0 {
		return nil
	}
	offered := offeredProfiles(manifest)
	known := map[string]bool{}
	for _, id := range offered {
		known[id] = true
	}
	for _, id := range opts.Enable {
		if !known[strings.TrimSpace(id)] {
			return fmt.Errorf(
				"update: unknown profile %q (offered: %s)",
				id, strings.Join(offered, ", "))
		}
	}
	for _, id := range opts.Disable {
		if !known[strings.TrimSpace(id)] {
			return fmt.Errorf(
				"update: unknown profile %q (offered: %s)",
				id, strings.Join(offered, ", "))
		}
	}
	for _, id := range opts.Disable {
		if trimmed := strings.TrimSpace(id); trimmed == "core" || trimmed == "custom" {
			return fmt.Errorf(
				"update: cannot disable always-enabled profile %q", trimmed)
		}
	}
	drop := map[string]bool{}
	for _, id := range opts.Disable {
		drop[strings.TrimSpace(id)] = true
	}
	var next []string
	present := map[string]bool{}
	for _, id := range config.GetProfiles(answers) {
		if drop[id] || present[id] {
			continue
		}
		present[id] = true
		next = append(next, id)
	}
	for _, id := range opts.Enable {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || present[trimmed] {
			continue
		}
		present[trimmed] = true
		next = append(next, trimmed)
	}
	if next == nil {
		next = []string{}
	}
	answers["enabled_profiles"] = next
	return nil
}

func reportProfileDiff(stderr io.Writer, diff config.ProfileDiff) error {
	if added := reportableProfiles(diff.Added); len(added) > 0 {
		if _, err := fmt.Fprintf(stderr,
			"turutan: template offers new profiles: %s\n",
			strings.Join(added, ", ")); err != nil {
			return err
		}
	}
	if len(diff.Removed) > 0 {
		if _, err := fmt.Fprintf(stderr,
			"turutan: template dropped profiles: %s\n",
			strings.Join(diff.Removed, ", ")); err != nil {
			return err
		}
	}
	if len(diff.NewPrompts) > 0 {
		if _, err := fmt.Fprintf(stderr,
			"turutan: template adds new prompts: %s\n",
			strings.Join(diff.NewPrompts, ", ")); err != nil {
			return err
		}
	}
	return nil
}

func offeredProfiles(manifest *config.Manifest) []string {
	seen := map[string]bool{"core": true, "custom": true}
	offered := []string{"core", "custom"}
	if manifest != nil {
		for id := range manifest.Profiles {
			if !seen[id] {
				seen[id] = true
				offered = append(offered, id)
			}
		}
	}
	sort.Strings(offered)
	return offered
}

func reportableProfiles(ids []string) []string {
	var out []string
	for _, id := range ids {
		if id == "core" || id == "custom" {
			continue
		}
		out = append(out, id)
	}
	return out
}

func relevantProfileDiff(
	manifest *config.Manifest,
	diff config.ProfileDiff,
) config.ProfileDiff {
	if manifest == nil || len(manifest.Profiles) == 0 {
		diff.Removed = nil
	}
	return diff
}

type previewEntry struct {
	Path     string
	Kind     string
	Profiles []string
}

func previewUpdate(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	fetched *template.Fetched,
	manifest *config.Manifest,
	answers map[string]any,
	opts UpdateOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
) (*UpdateResult, error) {
	stage, err := os.MkdirTemp("", "turutan-update-*")
	if err != nil {
		return nil, fmt.Errorf(
			"update: creating staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	rendered, err := renderStaged(
		fetched, stage, manifest, answers, opts, stdout, stdin)
	if err != nil {
		return nil, err
	}
	old := state.ResolvedCommit
	fresh := fetched.ResolvedCommit
	if fresh == "" {
		fresh = strings.TrimPrefix(
			config.ComputeManifestHash(templateLockEntries(rendered)),
			"sha256:")
	}
	vlogf(stderr, opts.Verbose, "update: preview %s -> %s", old, fresh)
	plan := previewPlan(projectDir, rendered, lock, manifest, answers)
	if err := printPreview(stdout, plan); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(plan))
	for _, entry := range plan {
		paths = append(paths, entry.Path)
	}
	return &UpdateResult{Old: old, New: fresh, Updated: paths}, nil
}

func previewPlan(
	projectDir string,
	rendered map[string]renderedEntry,
	lock *config.Lock,
	manifest *config.Manifest,
	answers map[string]any,
) []previewEntry {
	lockMap, _ := renamedLockMap(lock, manifest.Renames)
	paths := make([]string, 0, len(lockMap)+len(rendered))
	seen := map[string]bool{}
	for path := range lockMap {
		paths = append(paths, path)
		seen[path] = true
	}
	for path := range rendered {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	enabled := config.GetProfiles(answers)
	var plan []previewEntry
	for _, path := range paths {
		locked, inLock := lockMap[path]
		fresh, inRendered := rendered[path]
		var kind string
		switch {
		case inRendered && !inLock:
			kind = "added"
		case !inRendered && inLock:
			kind = "dropped"
		case config.FileEntry(path, fresh.Data).SHA256 != locked.SHA256:
			kind = "modified"
		default:
			continue
		}
		profiles := config.ProfilesForPath(manifest, enabled, path)
		if len(profiles) == 0 {
			profiles = []string{"core"}
		}
		if slices.Contains(profiles, "custom") && filePresent(projectDir, path) {
			continue
		}
		plan = append(plan, previewEntry{Path: path, Kind: kind, Profiles: profiles})
	}
	return plan
}

func printPreview(stdout io.Writer, plan []previewEntry) error {
	if len(plan) == 0 {
		if _, err := fmt.Fprintln(stdout, "turutan: preview: no changes"); err != nil {
			return err
		}
		return nil
	}
	groups := map[string][]previewEntry{}
	var order []string
	for _, entry := range plan {
		key := strings.Join(entry.Profiles, ",")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], entry)
	}
	sort.Strings(order)
	for _, key := range order {
		entries := groups[key]
		noun := "files"
		if len(entries) == 1 {
			noun = "file"
		}
		if _, err := fmt.Fprintf(stdout,
			"turutan: preview %s (%d %s):\n", key, len(entries), noun); err != nil {
			return err
		}
		for _, entry := range entries {
			var marker string
			switch entry.Kind {
			case "added":
				marker = "+"
			case "dropped":
				marker = "-"
			default:
				marker = "~"
			}
			if _, err := fmt.Fprintf(stdout, "  %s %s (%s)\n",
				marker, entry.Path, entry.Kind); err != nil {
				return err
			}
		}
	}
	return nil
}

func renamedLockMap(
	lock *config.Lock,
	renames map[string]string,
) (map[string]config.LockFile, map[string]string) {
	lockMap := make(map[string]config.LockFile, len(lock.Files))
	oldPaths := map[string]string{}
	for _, entry := range lock.Files {
		next := config.ResolveRenames(entry.Path, renames)
		if _, taken := lockMap[next]; taken {
			continue
		}
		moved := entry
		moved.Path = next
		lockMap[next] = moved
		if next != entry.Path {
			oldPaths[next] = entry.Path
		}
	}
	return lockMap, oldPaths
}

func finishUpdate(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	src *template.Source,
	wantRef string,
	answers map[string]any,
	fresh string,
	rendered map[string]renderedEntry,
	stage string,
	manifest *config.Manifest,
	stdout io.Writer,
	updated, conflicts, migrated []string,
	old string,
) (*UpdateResult, error) {
	if err := refreshStateLock(
		projectDir, state, lock, src, wantRef, answers, fresh, rendered, manifest,
	); err != nil {
		return nil, err
	}
	if err := StoreBase(projectDir, fresh, stage); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	result := &UpdateResult{
		Old: old, New: fresh,
		Updated: updated, Conflicts: conflicts, Migrations: migrated,
	}
	if _, err := fmt.Fprintf(stdout, "updated %s -> %s (%d files, %d conflicts)\n",
		shortSHA(old), shortSHA(fresh), len(updated), len(conflicts)); err != nil {
		return nil, err
	}
	return result, nil
}

func loadUpdateInputs(
	projectDir string,
	opts UpdateOptions,
) (*config.State, *config.Lock, map[string]any, error) {
	if err := opts.Conflict.Validate(); err != nil {
		return nil, nil, nil, err
	}
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update: %w", err)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("update: %w", err)
	}
	if err := filter.ValidateGlobs(state.Skip); err != nil {
		return nil, nil, nil, fmt.Errorf(
			"update: bad skip entry in state: %w", err)
	}
	answers, err := updateAnswers(projectDir, state, opts)
	if err != nil {
		return nil, nil, nil, err
	}
	return state, lock, answers, nil
}

func guardCleanTree(projectDir string, lock *config.Lock, manifest *config.Manifest, answers map[string]any) error {
	dirty, err := dirtyFiles(projectDir, lock, manifest, answers)
	if err != nil {
		return err
	}
	if len(dirty) == 0 {
		return nil
	}
	return fmt.Errorf(
		"update: dirty tree (%d files differ from lock; "+
			"use --force to overwrite): %s",
		len(dirty), strings.Join(firstN(dirty, maxDirtyShown), ", "))
}

func fetchUpdateTemplate(
	ctx context.Context,
	projectDir string,
	state *config.State,
	opts UpdateOptions,
	stderr io.Writer,
) (*template.Source, string, *template.Fetched, *config.Manifest, error) {
	vlogf(stderr, opts.Verbose,
		"update: stored commit %s requested ref %q",
		state.ResolvedCommit, state.RequestedRef)
	src, err := parseStoredSource(projectDir, state.Template)
	if err != nil {
		return nil, "", nil, nil, fmt.Errorf("update: %w", err)
	}
	wantRef := state.RequestedRef
	if opts.Ref != "" {
		wantRef = opts.Ref
		src.RequestedRef = opts.Ref
	}
	fetched, err := fetchWithDeepen(ctx, src)
	if err != nil {
		return nil, "", nil, nil, err
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		if fetched.Cleanup != nil {
			fetched.Cleanup()
		}
		return nil, "", nil, nil, fmt.Errorf("update: %w", err)
	}
	if _, err := checkMinEngine(
		manifest, opts.Engine, stderr, opts.Verbose); err != nil {
		if fetched.Cleanup != nil {
			fetched.Cleanup()
		}
		return nil, "", nil, nil, err
	}
	if opts.Conflict != "" {
		state.Conflict = string(opts.Conflict)
	}
	return src, wantRef, fetched, manifest, nil
}

func stageUpdate(
	projectDir string,
	state *config.State,
	fetched *template.Fetched,
	manifest *config.Manifest,
	answers map[string]any,
	opts UpdateOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
) (map[string]renderedEntry, string, string, *merger, bool, string, error) {
	stage, err := os.MkdirTemp("", "turutan-update-*")
	if err != nil {
		return nil, "", "", nil, false, "", fmt.Errorf(
			"update: creating staging dir: %w", err)
	}
	rendered, err := renderStaged(
		fetched, stage, manifest, answers, opts, stdout, stdin)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, "", "", nil, false, "", err
	}
	old := state.ResolvedCommit
	fresh := fetched.ResolvedCommit
	if fresh == "" {
		fresh = strings.TrimPrefix(
			config.ComputeManifestHash(templateLockEntries(rendered)),
			"sha256:")
	}
	logUpdateBase(stderr, opts.Verbose, projectDir, old, fresh)
	merger := newMerger(projectDir, state, manifest, opts, fresh, old, stderr)
	skip, err := gateMigrations(manifest, old, fresh, opts, stdout, stdin)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, "", "", nil, false, "", err
	}
	return rendered, old, fresh, merger, skip, stage, nil
}

func renderStaged(
	fetched *template.Fetched,
	stage string,
	manifest *config.Manifest,
	answers map[string]any,
	opts UpdateOptions,
	stdout io.Writer,
	stdin io.Reader,
) (map[string]renderedEntry, error) {
	normalizeUpdateAnswers(answers, manifest, opts)
	if err := renderUpdateWithAnswers(
		fetched.Dir, stage, answers, manifest, opts, stdout, stdin); err != nil {
		return nil, err
	}
	if err := plantLicenseFiles(fetched.Dir, stage, manifest); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if err := plantDocFiles(stage, manifest); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	rendered, err := walkRendered(stage, manifest)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	return rendered, nil
}

func newMerger(
	projectDir string,
	state *config.State,
	manifest *config.Manifest,
	opts UpdateOptions,
	fresh, old string,
	stderr io.Writer,
) *merger {
	return &merger{
		projectDir: projectDir,
		state:      state,
		manifest:   manifest,
		conflict:   effectiveConflict(opts.Conflict, manifest, state),
		newSHA:     fresh,
		baseDir:    baseDirFor(projectDir, old),
		stderr:     stderr,
	}
}

func logUpdateBase(
	stderr io.Writer,
	verbose bool,
	projectDir, old, fresh string,
) {
	if baseDirFor(projectDir, old) != "" {
		vlogf(stderr, verbose, "update: fresh commit %s base 3-way", fresh)
		return
	}
	vlogf(stderr, verbose,
		"update: fresh commit %s base overlay (no pristine copy)", fresh)
}

func baseDirFor(projectDir, old string) string {
	if old == "" {
		return ""
	}
	if dir, err := BasePath(projectDir, old); err == nil {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	if dir, err := basePathIn(projectDir, old, legacyBaseStoreSubdir); err == nil {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func updateStreams(opts UpdateOptions) (io.Writer, io.Writer, io.Reader) {
	return resolveStreams(opts.Stdout, opts.Stderr, opts.Stdin)
}

func updateAnswers(projectDir string, state *config.State, opts UpdateOptions) (map[string]any, error) {
	answers := map[string]any{}
	maps.Copy(answers, state.Answers)
	if opts.Defaults && answers["project_name"] == nil {
		abs, err := filepath.Abs(projectDir)
		if err != nil {
			return nil, fmt.Errorf("update: resolving project dir: %w", err)
		}
		answers["project_name"] = filepath.Base(abs)
	}
	if opts.AnswersFile != "" {
		overlay, err := LoadAnswersFile(opts.AnswersFile)
		if err != nil {
			return nil, err
		}
		maps.Copy(answers, overlay)
	}
	if s, ok := answers["enabled_profiles"].(string); ok {
		answers["enabled_profiles"] = splitDedupeList(s)
	}
	return answers, nil
}

func normalizeUpdateAnswers(answers map[string]any, manifest *config.Manifest, opts UpdateOptions) {
	_ = opts
	normalizeAnswersWithManifest(answers, manifest)
	seedMissingDefaults(answers, manifest)
	normalizeAnswersWithManifest(answers, manifest)
}

func dirtyFiles(projectDir string, lock *config.Lock, manifest *config.Manifest, answers map[string]any) ([]string, error) {
	var dirty []string
	for _, entry := range lock.Files {
		if customOwned(manifest, answers, entry.Path) {
			continue
		}
		data, err := filter.ReadFileWithinRoot(projectDir, entry.Path)
		if err != nil {
			if os.IsNotExist(err) {
				dirty = append(dirty, entry.Path)
				continue
			}
			return nil, fmt.Errorf("update: reading project file %q: %w", entry.Path, err)
		}
		if config.FileEntry(entry.Path, data).SHA256 != entry.SHA256 {
			dirty = append(dirty, entry.Path)
		}
	}
	sort.Strings(dirty)
	return dirty, nil
}

func firstN(paths []string, n int) []string {
	if len(paths) <= n {
		return paths
	}
	return paths[:n]
}

func fetchWithDeepen(
	ctx context.Context,
	src *template.Source,
) (*template.Fetched, error) {
	fetched, err := template.Fetch(ctx, src)
	if err == nil {
		return fetched, nil
	}
	if src.Kind != template.KindRemoteGit && src.Kind != template.KindLocalGit {
		return nil, fmt.Errorf("update: %w", err)
	}
	if src.Depth != template.DefaultDepth {
		return nil, fmt.Errorf("update: %w (template history is shallow; fetch with ?depth= to reach further back)", err)
	}
	retry := *src
	retry.Depth = updateDeepenDepth
	if refetched, retryErr := template.Fetch(ctx, &retry); retryErr == nil {
		return refetched, nil
	}
	return nil, fmt.Errorf("update: %w (template history is shallow; fetch with ?depth= to reach further back)", err)
}

func effectiveConflict(flag ConflictMode, manifest *config.Manifest, state *config.State) ConflictMode {
	if flag != "" {
		return flag
	}
	if manifest.Conflict != "" {
		return ConflictMode(manifest.Conflict)
	}
	if state != nil {
		switch ConflictMode(state.Conflict) {
		case ConflictInline, ConflictRej:
			return ConflictMode(state.Conflict)
		}
	}
	return ConflictInline
}

func templateLockEntries(rendered map[string]renderedEntry) []config.LockFile {
	files := make([]config.LockFile, 0, len(rendered))
	for _, entry := range rendered {
		files = append(files, config.FileEntry(entry.Path, entry.Data))
	}
	return files
}

type merger struct {
	projectDir string
	state      *config.State
	manifest   *config.Manifest
	conflict   ConflictMode
	newSHA     string
	baseDir    string
	oldPaths   map[string]string
	stderr     io.Writer
}

func (m *merger) merge(rendered map[string]renderedEntry, lock *config.Lock) ([]string, []string, error) {
	lockMap, oldPaths := renamedLockMap(lock, m.manifest.Renames)
	m.oldPaths = oldPaths
	paths := make([]string, 0, len(lockMap)+len(rendered))
	seen := map[string]bool{}
	for path := range lockMap {
		paths = append(paths, path)
		seen[path] = true
	}
	for path := range rendered {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var updated, conflicts []string
	for _, path := range paths {
		wrote, conflicted, err := m.mergeOne(
			path, lockMap[path], rendered[path],
			inLock(lockMap, path), inRendered(rendered, path))
		if err != nil {
			return nil, nil, err
		}
		wrote, err = m.followRename(path, wrote, inRendered(rendered, path))
		if err != nil {
			return nil, nil, err
		}
		if wrote {
			updated = append(updated, path)
		}
		if conflicted {
			conflicts = append(conflicts, path)
		}
	}
	sort.Strings(updated)
	sort.Strings(conflicts)
	return updated, conflicts, nil
}

func inLock(lockMap map[string]config.LockFile, path string) bool {
	_, ok := lockMap[path]
	return ok
}

func inRendered(rendered map[string]renderedEntry, path string) bool {
	_, ok := rendered[path]
	return ok
}

func (m *merger) mergeOne(
	path string,
	locked config.LockFile,
	fresh renderedEntry,
	hasLocked, hasFresh bool,
) (bool, bool, error) {
	ignored, err := filter.MatchAny(m.manifest.Ignore, path)
	if err != nil {
		return false, false, fmt.Errorf("update: matching ignore globs: %w", err)
	}
	if ignored {
		return false, false, nil
	}
	preserved, err := userOwned(path, m.manifest, m.state)
	if err != nil {
		return false, false, err
	}
	if _, err := filter.SafeJoin(m.projectDir, filepath.FromSlash(path)); err != nil {
		return false, false, fmt.Errorf("update: %w", err)
	}
	local, localExists, err := m.readLocal(path)
	if err != nil {
		return false, false, err
	}
	switch {
	case hasFresh && !hasLocked:
		return m.mergeAdded(path, fresh.Data, local, localExists, preserved)
	case !hasFresh && hasLocked:
		return m.mergeDropped(path, locked, local, localExists)
	default:
		return m.mergeBoth(path, locked, fresh.Data, local, localExists, preserved)
	}
}

func (m *merger) mergeAdded(path string, fresh, local []byte, localExists, preserved bool) (bool, bool, error) {
	if !localExists {
		if err := m.write(path, fresh); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	if bytes.Equal(local, fresh) {
		return false, false, nil
	}
	if preserved || m.customOwned(path) {
		return false, false, nil
	}
	if isBinary(local) || isBinary(fresh) {
		if err := m.write(path+".new", fresh); err != nil {
			return false, false, err
		}
		if _, err := fmt.Fprintf(m.stderr,
			"turutan: binary %q is new in the template but exists "+"locally; keeping local, wrote %q\n",
			path, path+".new"); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	return m.conflictBoth(path, local, fresh)
}

func (m *merger) readLocal(path string) ([]byte, bool, error) {
	local, err := filter.ReadFileWithinRoot(m.projectDir, path)
	if err == nil {
		return local, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("update: reading project file %q: %w", path, err)
	}
	old, ok := m.oldPaths[path]
	if !ok || old == path {
		return nil, false, nil
	}
	prior, err := filter.ReadFileWithinRoot(m.projectDir, old)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("update: reading project file %q: %w", old, err)
	}
	return prior, true, nil
}

func (m *merger) readBase(path string) ([]byte, bool, error) {
	base, err := filter.ReadFileWithinRoot(m.baseDir, path)
	if err == nil {
		return base, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("update: reading base copy of %q: %w", path, err)
	}
	old, ok := m.oldPaths[path]
	if !ok || old == path {
		return nil, false, nil
	}
	prior, err := filter.ReadFileWithinRoot(m.baseDir, old)
	if err == nil {
		return prior, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("update: reading base copy of %q: %w", old, err)
	}
	return nil, false, nil
}

func (m *merger) followRename(path string, wrote, hasFresh bool) (bool, error) {
	old, ok := m.oldPaths[path]
	if !ok || old == path {
		return wrote, nil
	}
	if _, err := filter.StatWithinRoot(m.projectDir, old); err != nil {
		if os.IsNotExist(err) {
			return wrote, nil
		}
		return wrote, fmt.Errorf("update: stating renamed file %q: %w", old, err)
	}
	if !wrote && hasFresh {
		preserved, err := userOwned(path, m.manifest, m.state)
		if err != nil {
			return wrote, err
		}
		if !preserved {
			data, err := filter.ReadFileWithinRoot(m.projectDir, old)
			if err != nil {
				return wrote, fmt.Errorf("update: reading renamed file %q: %w", old, err)
			}
			if err := m.write(path, data); err != nil {
				return wrote, err
			}
			wrote = true
		}
	}
	if !wrote {
		return false, nil
	}
	if err := filter.RemoveWithinRoot(m.projectDir, old); err != nil && !os.IsNotExist(err) {
		return wrote, fmt.Errorf("update: removing renamed file %q: %w", old, err)
	}
	m.pruneEmptyParents(old)
	return wrote, nil
}

func (m *merger) mergeDropped(path string, locked config.LockFile, local []byte, localExists bool) (bool, bool, error) {
	if !localExists {
		return false, false, nil
	}
	if m.customOwned(path) {
		if _, err := fmt.Fprintf(m.stderr, "turutan: keeping custom-owned %q dropped by the template\n", path); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	if config.FileEntry(path, local).SHA256 == locked.SHA256 {
		if err := filter.RemoveWithinRoot(m.projectDir, path); err != nil && !os.IsNotExist(err) {
			return false, false, fmt.Errorf("update: removing dropped file %q: %w", path, err)
		}
		m.pruneEmptyParents(path)
		return true, false, nil
	}
	if _, err := fmt.Fprintf(m.stderr, "turutan: keeping locally modified %q dropped by the template\n", path); err != nil {
		return false, false, err
	}
	return false, false, nil
}

func (m *merger) pruneEmptyParents(path string) {
	dir := filepath.Dir(filepath.FromSlash(path))
	for dir != "." && dir != "" {
		abs, err := filter.SafeJoin(m.projectDir, dir)
		if err != nil {
			return
		}
		info, err := os.Lstat(abs)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return
		}
		if err := os.Remove(abs); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func (m *merger) mergeBoth(
	path string,
	locked config.LockFile,
	fresh, local []byte,
	localExists, preserved bool,
) (bool, bool, error) {
	if preserved || m.customOwned(path) {
		return false, false, nil
	}
	if !localExists {
		return m.mergeRestored(path, locked, fresh)
	}
	lockHash := locked.SHA256
	localHash := config.FileEntry(path, local).SHA256
	freshHash := config.FileEntry(path, fresh).SHA256
	switch templateChanged, localChanged :=
		lockHash != freshHash, lockHash != localHash; {
	case !templateChanged && !localChanged:
		return false, false, nil
	case !templateChanged && localChanged:
		return false, false, nil
	case templateChanged && !localChanged:
		return m.writeFresh(path, fresh)
	default:
		return m.mergeBothChanged(path, local, fresh, preserved)
	}
}

func (m *merger) mergeRestored(
	path string,
	locked config.LockFile,
	fresh []byte,
) (bool, bool, error) {
	if config.FileEntry(path, fresh).SHA256 == locked.SHA256 {
		return false, false, nil
	}
	if err := m.write(path, fresh); err != nil {
		return false, false, err
	}
	if _, err := fmt.Fprintf(m.stderr,
		"turutan: restoring %q changed in the template "+
			"but deleted locally\n",
		path); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (m *merger) writeFresh(path string, fresh []byte) (bool, bool, error) {
	if err := m.write(path, fresh); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (m *merger) mergeBothChanged(
	path string,
	local, fresh []byte,
	preserved bool,
) (bool, bool, error) {
	if bytes.Equal(local, fresh) || preserved {
		return false, false, nil
	}
	if isBinary(local) || isBinary(fresh) {
		return m.mergeBinaryBoth(path, local, fresh)
	}
	if m.baseDir != "" {
		wrote, conflicted, handled, err := m.threeWay(path, local, fresh)
		if err != nil {
			return false, false, err
		}
		if handled {
			return wrote, conflicted, nil
		}
		if _, err := fmt.Fprintf(m.stderr,
			"turutan: no base copy for %q; using overlay merge\n", path); err != nil {
			return false, false, err
		}
	}
	return m.conflictBoth(path, local, fresh)
}

func (m *merger) mergeBinaryBoth(
	path string,
	_, fresh []byte,
) (bool, bool, error) {
	if err := m.write(path+".new", fresh); err != nil {
		return false, false, err
	}
	if _, err := fmt.Fprintf(m.stderr,
		"turutan: binary %q changed both locally and "+
			"in the template; keeping local, wrote %q\n",
		path, path+".new"); err != nil {
		return false, true, err
	}
	return false, true, nil
}

func (m *merger) threeWay(path string, local, fresh []byte) (wrote, conflicted, handled bool, err error) {
	base, found, err := m.readBase(path)
	if err != nil {
		return false, false, true, err
	}
	if !found {
		return false, false, false, nil
	}
	if bytes.Equal(base, local) {
		if err := m.write(path, fresh); err != nil {
			return false, false, true, err
		}
		return true, false, true, nil
	}
	if bytes.Equal(base, fresh) {
		return false, false, true, nil
	}
	merged, hadConflicts, mergeErr := diff3.Merge(string(base), string(local), string(fresh), diff3.Options{})
	if mergeErr != nil {
		return m.conflictBothAs(path, local, fresh, true)
	}
	if !hadConflicts {
		if err := m.write(path, []byte(merged)); err != nil {
			return false, false, true, err
		}
		return true, false, true, nil
	}
	return m.conflictBothAs(path, local, fresh, true)
}

func (m *merger) conflictBoth(path string, local, fresh []byte) (bool, bool, error) {
	wrote, conflicted, _, err := m.conflictBothAs(
		path, local, fresh, true)
	return wrote, conflicted, err
}

func (m *merger) conflictBothAs(
	path string,
	local, fresh []byte,
	handled bool,
) (wrote, conflicted, stillHandled bool, err error) {
	if m.conflict == ConflictRej {
		diff, changed, err := newFileDiff(path, local, fresh, false, false)
		if err != nil {
			return false, false, handled, err
		}
		if !changed {
			return false, false, handled, nil
		}
		if err := m.write(path+".rej", []byte(diff.Unified)); err != nil {
			return false, false, handled, err
		}
		if _, err := fmt.Fprintf(m.stderr, "turutan: conflict in %q; keeping local, wrote %q\n", path, path+".rej"); err != nil {
			return false, true, handled, err
		}
		return false, true, handled, nil
	}
	if err := m.write(path, inlineConflict(local, fresh, shortSHA(m.newSHA))); err != nil {
		return false, false, handled, err
	}
	if _, err := fmt.Fprintf(m.stderr, "turutan: conflict in %q; wrote inline markers\n", path); err != nil {
		return true, true, handled, err
	}
	return true, true, handled, nil
}

func inlineConflict(local, fresh []byte, short string) []byte {
	var out strings.Builder
	out.WriteString("<<<<<<< local\n")
	out.Write(local)
	if len(local) == 0 || local[len(local)-1] != '\n' {
		out.WriteString("\n")
	}
	out.WriteString("=======\n")
	out.Write(fresh)
	if len(fresh) == 0 || fresh[len(fresh)-1] != '\n' {
		out.WriteString("\n")
	}
	out.WriteString(">>>>>>> template-" + short + "\n")
	return []byte(out.String())
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) != -1
}

func (m *merger) write(rel string, data []byte) error {
	if _, err := filter.SafeJoin(m.projectDir, filepath.FromSlash(rel)); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := filter.WriteFileWithinRoot(m.projectDir, rel, data, 0o644); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

func renderUpdateWithAnswers(
	fetchedDir, stage string,
	answers map[string]any,
	manifest *config.Manifest,
	opts UpdateOptions,
	stdout io.Writer,
	stdin io.Reader,
) error {
	for range maxPromptAttempts {
		if err := ensurePromptAnswers(answers, manifest, opts.NonInteractive, opts.Defaults, stdout, stdin, "update"); err != nil {
			return err
		}
		normalizeAnswersWithManifest(answers, manifest)
		if err := os.RemoveAll(stage); err != nil {
			return fmt.Errorf("update: resetting staging dir: %w", err)
		}
		if err := os.MkdirAll(stage, 0o750); err != nil {
			return fmt.Errorf("update: creating staging dir: %w", err)
		}
		err := template.RenderDirWithConditionals(fetchedDir, stage, answers, manifestConditionals(manifest))
		if err == nil {
			return nil
		}
		key, ok := AsMissingKey(err)
		if !ok {
			return err
		}
		if opts.NonInteractive {
			return fmt.Errorf("update: missing answer for key %q (use --answers-file or --defaults): %w", key, err)
		}
		if err := promptMissingKey(answers, manifest, stdout, stdin, key); err != nil {
			return err
		}
	}
	return fmt.Errorf("update: too many missing answers (over %d prompts)", maxPromptAttempts)
}

func gateMigrations(
	manifest *config.Manifest,
	old, fresh string,
	opts UpdateOptions,
	stdout io.Writer,
	stdin io.Reader,
) (skip bool, err error) {
	var pending []string
	for _, migration := range manifest.Migrations {
		if len(migration.Run) == 0 {
			continue
		}
		if !matchesMigration(migration.From, old, fresh) {
			continue
		}
		pending = append(pending, migration.Run...)
	}
	if len(pending) == 0 {
		return false, nil
	}
	if opts.AllowHooks {
		return false, nil
	}
	if opts.NonInteractive {
		return false, fmt.Errorf(
			"update: template declares migrations: refusing in " + "--non-interactive mode without --allow-hooks")
	}
	allow, err := promptConfirm(stdout, stdin, fmt.Sprintf("template declares migrations %q; allow", pending))
	if err != nil {
		return false, err
	}
	if !allow {
		if _, err := fmt.Fprintf(stdout, "turutan: template migrations %q skipped (consent declined)\n", pending); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func runMigrations(
	projectDir string,
	manifest *config.Manifest,
	old, fresh string,
	stderr io.Writer,
) ([]string, error) {
	var executed []string
	for _, migration := range manifest.Migrations {
		if !matchesMigration(migration.From, old, fresh) {
			continue
		}
		for _, cmd := range migration.Run {
			if err := runMigrationCmd(projectDir, cmd, old, fresh, stderr); err != nil {
				return executed, err
			}
			executed = append(executed, cmd)
		}
	}
	return executed, nil
}

func matchesMigration(from, old, fresh string) bool {
	if from == "" {
		return true
	}
	if old != "" && (old == from || strings.HasPrefix(old, from) || strings.HasPrefix(from, old)) {
		return true
	}
	if fresh != "" && (fresh == from || strings.HasPrefix(fresh, from) || strings.HasPrefix(from, fresh)) {
		return true
	}
	constraint, err := semver.NewConstraint(from)
	if err != nil {
		return true
	}
	var versions []*semver.Version
	for _, ident := range []string{old, fresh} {
		if version, err := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(ident), "v")); err == nil {
			versions = append(versions, version)
		}
	}
	if len(versions) == 0 {
		return true
	}
	return slices.ContainsFunc(versions, constraint.Check)
}

func runMigrationCmd(projectDir, cmd, old, fresh string, stderr io.Writer) error {
	if cmd == "" {
		return fmt.Errorf("update: migration has an empty run command")
	}
	argv := []string{"-c", cmd}
	if !strings.ContainsAny(cmd, " \t\n|&;()<>$`\\\"'") {
		if safe, joinErr := filter.SafeJoin(projectDir, filepath.FromSlash(cmd)); joinErr == nil {
			if info, err := os.Stat(safe); err == nil && !info.IsDir() {
				argv = []string{safe}
			}
		}
	}
	executed := exec.Command("sh", argv...) // #nosec G204 -- runs template-declared migration gated by --allow-hooks consent with filtered env (no secrets)
	executed.Dir = projectDir
	executed.Env = hookEnv("TURUTAN_OLD="+old, "TURUTAN_NEW="+fresh)
	output, err := executed.CombinedOutput()
	if len(output) > 0 {
		if _, err := fmt.Fprintf(stderr, "turutan: migration %q output:\n%s", cmd, output); err != nil {
			return err
		}
	}
	if err != nil {
		return fmt.Errorf("update: migration %q failed: %w", cmd, err)
	}
	return nil
}

func refreshStateLock(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	src *template.Source,
	wantRef string,
	answers map[string]any,
	fresh string,
	rendered map[string]renderedEntry,
	manifest *config.Manifest,
) error {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	final := make([]config.LockFile, 0, len(paths))
	enabled := config.GetProfiles(answers)
	for _, path := range paths {
		data, err := filter.ReadFileWithinRoot(projectDir, path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("update: reading merged file %q: %w", path, err)
		}
		entry := config.FileEntry(path, data)
		if tags := config.ProfilesForPath(manifest, enabled, path); len(tags) > 0 {
			entry.Profile = strings.Join(tags, ",")
		}
		final = append(final, entry)
	}
	state.RequestedRef = wantRef
	state.ResolvedCommit = fresh
	state.Answers = answers
	stored := relativizeForStore(projectDir, src)
	state.Template = stored.String()
	if err := config.SaveState(projectDir, state); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	lock.Source = state.Template
	lock.Ref = wantRef
	lock.ResolvedSHA = fresh
	lock.Subpath = src.Subpath
	lock.Files = final
	if err := config.SaveLock(projectDir, lock); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}
