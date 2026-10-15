// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CivNode/diff3-go"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/registry"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

const maxDirtyShown = 5

type UpdateOptions struct {
	Ref            string
	Conflict       ConflictMode
	Force          bool
	DataFile       string
	Defaults       bool
	NonInteractive bool
	Verbose        bool
	Engine         string
	Preview        bool
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
}

type UpdateResult struct {
	Old       string
	New       string
	Updated   []string
	Conflicts []string
}

type fetchedBundle struct {
	dir     string
	uri     string
	version string
	digest  string
	cleanup func()
}

func Update(
	ctx context.Context,
	projectDir string,
	opts UpdateOptions,
) (*UpdateResult, error) {
	stdout, stderr, stdin := updateStreams(opts)
	state, lock, data, err := loadUpdateInputs(projectDir, opts)
	if err != nil {
		return nil, err
	}
	bundle, manifest, err := fetchUpdateTemplate(
		ctx, state, opts, stderr)
	if err != nil {
		return nil, err
	}
	if bundle.cleanup != nil {
		defer bundle.cleanup()
	}
	reportNewInputs(stderr, config.DiffInputs(data, manifest))
	if opts.Preview {
		return previewUpdate(
			projectDir, state, lock, bundle, manifest, data,
			opts, stdout, stderr, stdin)
	}
	rendered, old, fresh, merger, stage, err := stageUpdate(
		projectDir, state, bundle, manifest, data,
		opts, stdout, stderr, stdin)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if !opts.Force {
		if err := guardCleanTree(projectDir, lock, manifest, state, merger.preserved); err != nil {
			return nil, err
		}
	}
	updated, conflicts, err := merger.merge(rendered, lock)
	if err != nil {
		return nil, err
	}
	return finishUpdate(
		projectDir, state, lock, bundle, data, fresh,
		rendered, stage, manifest, stdout, updated, conflicts, old)
}

func filePresent(projectDir, path string) bool {
	_, err := filter.StatWithinRoot(projectDir, path)
	return err == nil
}

// reportNewInputs warns about template inputs the project has never
// answered, so sync can ask for them before re-rendering.
// ~keep
func reportNewInputs(stderr io.Writer, inputs []string) {
	if len(inputs) == 0 {
		return
	}
	if stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(stderr,
		"turutan: template adds new inputs: %s\n",
		strings.Join(inputs, ", "))
}

type previewEntry struct {
	Path string
	Kind string
}

func previewUpdate(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	bundle *fetchedBundle,
	manifest *config.Manifest,
	data map[string]any,
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
		bundle, stage, manifest, data, opts, stdout, stdin)
	if err != nil {
		return nil, err
	}
	preserved, err := preservedGlobs(data, manifest)
	if err != nil {
		return nil, err
	}
	old := state.ResolvedCommit
	fresh := bundle.digest
	if fresh == "" {
		fresh = strings.TrimPrefix(
			config.ComputeManifestHash(templateLockEntries(rendered)),
			"sha256:")
	}
	vlogf(stderr, opts.Verbose, "update: preview %s -> %s", old, fresh)
	plan := previewPlan(projectDir, rendered, lock, manifest, state, preserved)
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
	state *config.State,
	preserved []string,
) []previewEntry {
	lockMap := lockMapOf(lock)
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
		owned, err := userOwned(path, manifest, state, preserved)
		if err == nil && owned && filePresent(projectDir, path) {
			continue
		}
		plan = append(plan, previewEntry{Path: path, Kind: kind})
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
		key := entry.Kind
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

func lockMapOf(lock *config.Lock) map[string]config.LockFile {
	lockMap := make(map[string]config.LockFile, len(lock.Files))
	for _, entry := range lock.Files {
		if _, taken := lockMap[entry.Path]; taken {
			continue
		}
		lockMap[entry.Path] = entry
	}
	return lockMap
}

func finishUpdate(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	bundle *fetchedBundle,
	data map[string]any,
	fresh string,
	rendered map[string]renderedEntry,
	stage string,
	manifest *config.Manifest,
	stdout io.Writer,
	updated, conflicts []string,
	old string,
) (*UpdateResult, error) {
	if err := refreshStateLock(
		projectDir, state, lock, bundle, data, fresh, rendered, manifest,
	); err != nil {
		return nil, err
	}
	if err := StoreBase(projectDir, fresh, stage); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	result := &UpdateResult{
		Old: old, New: fresh,
		Updated: updated, Conflicts: conflicts,
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
	data, err := updateData(projectDir, state, opts)
	if err != nil {
		return nil, nil, nil, err
	}
	return state, lock, data, nil
}

func guardCleanTree(projectDir string, lock *config.Lock, manifest *config.Manifest, state *config.State, preserved []string) error {
	dirty, err := dirtyFiles(projectDir, lock, manifest, state, preserved)
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
	state *config.State,
	opts UpdateOptions,
	stderr io.Writer,
) (*fetchedBundle, *config.Manifest, error) {
	vlogf(stderr, opts.Verbose,
		"update: stored template %q version %q digest %s",
		state.Template, state.RequestedRef, state.ResolvedCommit)
	if state.SourceKind != "registry" {
		return nil, nil, fmt.Errorf("update: unknown sourceKind %q", state.SourceKind)
	}
	registryURI := strings.TrimSpace(state.Registry)
	if registryURI == "" {
		return nil, nil, fmt.Errorf("update: stored registry is empty")
	}
	storedRegistry, templateName, _, err := SplitStoredRef(state.Template)
	if err != nil {
		return nil, nil, fmt.Errorf("update: %w", err)
	}
	if storedRegistry != registryURI {
		return nil, nil, fmt.Errorf("update: stored template %q does not match registry %q", state.Template, registryURI)
	}
	if templateName == "" {
		return nil, nil, fmt.Errorf("update: stored template %q has no template name", state.Template)
	}
	rr, err := ResolveRegistry(ctx, registryURI)
	if err != nil {
		return nil, nil, fmt.Errorf("update: %w", err)
	}
	want := strings.TrimSpace(opts.Ref)
	if want == "" {
		entry, err := rr.Idx.FindTemplate(templateName)
		if err != nil {
			return nil, nil, fmt.Errorf("update: %w", err)
		}
		want = entry.Version
	}
	templateManifest, err := ResolveTemplateManifest(rr, templateName, want)
	if err != nil {
		return nil, nil, fmt.Errorf("update: %w", err)
	}
	bundleDir, bundleURI, cleanup, err := FetchTemplateBundle(ctx, rr, templateManifest)
	if err != nil {
		return nil, nil, fmt.Errorf("update: %w", err)
	}
	manifest, err := config.LoadManifest(os.DirFS(bundleDir))
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("update: %w", err)
	}
	if _, err := checkMinEngine(
		manifest, opts.Engine, stderr, opts.Verbose); err != nil {
		cleanup()
		return nil, nil, err
	}
	if opts.Conflict != "" {
		state.Conflict = string(opts.Conflict)
	}
	digest := strings.TrimSpace(templateManifest.Digest)
	if digest == "" {
		if computed, err := registry.DigestDir(bundleDir); err == nil {
			digest = computed
		}
	}
	digestHex := strings.TrimPrefix(digest, "sha256:")
	return &fetchedBundle{
		dir: bundleDir, uri: bundleURI, version: templateManifest.Version,
		digest: digestHex, cleanup: cleanup,
	}, manifest, nil
}

func stageUpdate(
	projectDir string,
	state *config.State,
	bundle *fetchedBundle,
	manifest *config.Manifest,
	data map[string]any,
	opts UpdateOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
) (map[string]renderedEntry, string, string, *merger, string, error) {
	stage, err := os.MkdirTemp("", "turutan-update-*")
	if err != nil {
		return nil, "", "", nil, "", fmt.Errorf(
			"update: creating staging dir: %w", err)
	}
	rendered, err := renderStaged(
		bundle, stage, manifest, data, opts, stdout, stdin)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, "", "", nil, "", err
	}
	preserved, err := preservedGlobs(data, manifest)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, "", "", nil, "", err
	}
	old := state.ResolvedCommit
	fresh := bundle.digest
	if fresh == "" {
		fresh = strings.TrimPrefix(
			config.ComputeManifestHash(templateLockEntries(rendered)),
			"sha256:")
	}
	logUpdateBase(stderr, opts.Verbose, projectDir, old, fresh)
	merger := newMerger(projectDir, state, manifest, preserved, opts, fresh, old, stderr)
	return rendered, old, fresh, merger, stage, nil
}

func renderStaged(
	bundle *fetchedBundle,
	stage string,
	manifest *config.Manifest,
	data map[string]any,
	opts UpdateOptions,
	stdout io.Writer,
	stdin io.Reader,
) (map[string]renderedEntry, error) {
	normalizeUpdateData(data, manifest, opts)
	if err := renderUpdateWithData(
		bundle.dir, stage, data, manifest, opts, stdout, stdin); err != nil {
		return nil, err
	}
	if err := plantLicenseFiles(bundle.dir, stage, manifest); err != nil {
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
	preserved []string,
	opts UpdateOptions,
	fresh, old string,
	stderr io.Writer,
) *merger {
	return &merger{
		projectDir: projectDir,
		state:      state,
		manifest:   manifest,
		preserved:  preserved,
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
	return ""
}

func updateStreams(opts UpdateOptions) (io.Writer, io.Writer, io.Reader) {
	return resolveStreams(opts.Stdout, opts.Stderr, opts.Stdin)
}

func updateData(projectDir string, state *config.State, opts UpdateOptions) (map[string]any, error) {
	data := map[string]any{}
	maps.Copy(data, state.Data)
	if opts.Defaults && data["project_name"] == nil {
		abs, err := filepath.Abs(projectDir)
		if err != nil {
			return nil, fmt.Errorf("update: resolving project dir: %w", err)
		}
		data["project_name"] = filepath.Base(abs)
	}
	if opts.DataFile != "" {
		overlay, err := LoadDataFile(opts.DataFile)
		if err != nil {
			return nil, err
		}
		maps.Copy(data, overlay)
	}
	return data, nil
}

func normalizeUpdateData(data map[string]any, manifest *config.Manifest, opts UpdateOptions) {
	_ = opts
	normalizeDataWithManifest(data, manifest)
	seedMissingDefaults(data, manifest)
	normalizeDataWithManifest(data, manifest)
}

func dirtyFiles(projectDir string, lock *config.Lock, manifest *config.Manifest, state *config.State, preserved []string) ([]string, error) {
	var dirty []string
	for _, entry := range lock.Files {
		owned, err := userOwned(entry.Path, manifest, state, preserved)
		if err != nil {
			return nil, err
		}
		if owned {
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

func withDigestPrefix(fresh string) string {
	if strings.HasPrefix(fresh, "sha256:") {
		return fresh
	}
	return "sha256:" + fresh
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
	preserved  []string
	conflict   ConflictMode
	newSHA     string
	baseDir    string
	stderr     io.Writer
}

func (m *merger) merge(rendered map[string]renderedEntry, lock *config.Lock) ([]string, []string, error) {
	lockMap := lockMapOf(lock)
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
	ignored, err := filter.MatchAny(m.manifest.Excludes, path)
	if err != nil {
		return false, false, fmt.Errorf("update: matching excludes globs: %w", err)
	}
	if ignored && !hasFresh {
		return false, false, nil
	}
	preserved, err := userOwned(path, m.manifest, m.state, m.preserved)
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
	if preserved {
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
	return nil, false, nil
}

func (m *merger) readBase(path string) ([]byte, bool, error) {
	base, err := filter.ReadFileWithinRoot(m.baseDir, path)
	if err == nil {
		return base, true, nil
	}
	if !os.IsNotExist(err) {
		return nil, true, fmt.Errorf("update: reading base copy of %q: %w", path, err)
	}
	return nil, false, nil
}

func (m *merger) mergeDropped(path string, locked config.LockFile, local []byte, localExists bool) (bool, bool, error) {
	if !localExists {
		return false, false, nil
	}
	preserved, err := userOwned(path, m.manifest, m.state, m.preserved)
	if err != nil {
		return false, false, err
	}
	if preserved {
		if _, err := fmt.Fprintf(m.stderr, "turutan: keeping preserved %q dropped by the template\n", path); err != nil {
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
	if preserved {
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

func renderUpdateWithData(
	fetchedDir, stage string,
	data map[string]any,
	manifest *config.Manifest,
	opts UpdateOptions,
	stdout io.Writer,
	stdin io.Reader,
) error {
	for range maxInputAttempts {
		if err := ensureInputValues(data, manifest, opts.NonInteractive, opts.Defaults, stdout, stdin, "update"); err != nil {
			return err
		}
		normalizeDataWithManifest(data, manifest)
		if err := os.RemoveAll(stage); err != nil {
			return fmt.Errorf("update: resetting staging dir: %w", err)
		}
		if err := os.MkdirAll(stage, 0o750); err != nil {
			return fmt.Errorf("update: creating staging dir: %w", err)
		}
		view := renderDataView(data)
		err := template.RenderDirWithConditionals(fetchedDir, stage, view, manifest.Includes, manifest.Excludes, manifest.Preserves, manifestConditionals(manifest))
		if err == nil {
			return nil
		}
		key, ok := AsMissingKey(err)
		if !ok {
			return err
		}
		if opts.NonInteractive {
			return fmt.Errorf("update: missing data for key %q (use --data-file or --defaults): %w", key, err)
		}
		if err := inputMissingKey(data, manifest, stdout, stdin, key); err != nil {
			return err
		}
	}
	return fmt.Errorf("update: too many missing values (over %d inputs)", maxInputAttempts)
}

func refreshStateLock(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	bundle *fetchedBundle,
	data map[string]any,
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
	for _, path := range paths {
		data, err := filter.ReadFileWithinRoot(projectDir, path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("update: reading merged file %q: %w", path, err)
		}
		final = append(final, config.FileEntry(path, data))
	}
	_, templateName, _, err := SplitStoredRef(state.Template)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	state.RequestedRef = bundle.version
	state.ResolvedCommit = fresh
	state.Data = data
	state.Template = RegistryStateRef(state.Registry, templateName, bundle.version)
	state.TemplateURI = bundle.uri
	state.TemplateDigest = withDigestPrefix(fresh)
	if err := config.SaveState(projectDir, state); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := config.SaveStateDataSchema(projectDir, manifest); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	lock.Source = state.Template
	lock.Ref = bundle.version
	lock.ResolvedSHA = fresh
	lock.Registry = state.Registry
	lock.URI = bundle.uri
	lock.Digest = withDigestPrefix(fresh)
	lock.Files = final
	if err := config.SaveLock(projectDir, lock); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}
