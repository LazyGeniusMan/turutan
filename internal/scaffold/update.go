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
		ctx, state, opts, stderr)
	if err != nil {
		return nil, err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	rendered, old, fresh, merger, skipMigrations, stage, err := stageUpdate(
		projectDir, state, fetched, manifest, answers,
		opts, stdout, stderr, stdin)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
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
		rendered, stage, stdout, updated, conflicts, migrated, old)
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
	stdout io.Writer,
	updated, conflicts, migrated []string,
	old string,
) (*UpdateResult, error) {
	if err := refreshStateLock(
		projectDir, state, lock, src, wantRef, answers, fresh, rendered,
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
	fmt.Fprintf(stdout, "updated %s -> %s (%d files, %d conflicts)\n",
		shortSHA(old), shortSHA(fresh), len(updated), len(conflicts))
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
	if !opts.Force {
		if err := guardCleanTree(projectDir, lock); err != nil {
			return nil, nil, nil, err
		}
	}
	return state, lock, answers, nil
}

func guardCleanTree(projectDir string, lock *config.Lock) error {
	dirty, err := dirtyFiles(projectDir, lock)
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
) (*template.Source, string, *template.Fetched, *config.Manifest, error) {
	vlogf(stderr, opts.Verbose,
		"update: stored commit %s requested ref %q",
		state.ResolvedCommit, state.RequestedRef)
	src, err := template.ParseSource(state.Template)
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
	if err := renderUpdateWithAnswers(
		fetched.Dir, stage, answers, opts, stdout, stdin); err != nil {
		return nil, err
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
	if old == "" || !HasBase(projectDir, old) {
		return ""
	}
	dir, err := BasePath(projectDir, old)
	if err != nil {
		return ""
	}
	return dir
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
	return answers, nil
}

func dirtyFiles(projectDir string, lock *config.Lock) ([]string, error) {
	var dirty []string
	for _, entry := range lock.Files {
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
	stderr     io.Writer
}

func (m *merger) merge(rendered map[string]renderedEntry, lock *config.Lock) ([]string, []string, error) {
	lockMap := make(map[string]config.LockFile, len(lock.Files))
	for _, entry := range lock.Files {
		lockMap[entry.Path] = entry
	}
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
	local, err := filter.ReadFileWithinRoot(m.projectDir, path)
	if err != nil && !os.IsNotExist(err) {
		return false, false, fmt.Errorf("update: reading project file %q: %w", path, err)
	}
	localExists := err == nil
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
		fmt.Fprintf(m.stderr,
			"turutan: binary %q is new in the template but exists "+"locally; keeping local, wrote %q\n",
			path, path+".new")
		return false, true, nil
	}
	return m.conflictBoth(path, local, fresh)
}

func (m *merger) mergeDropped(path string, locked config.LockFile, local []byte, localExists bool) (bool, bool, error) {
	if !localExists {
		return false, false, nil
	}
	if config.FileEntry(path, local).SHA256 == locked.SHA256 {
		if err := filter.RemoveWithinRoot(m.projectDir, path); err != nil && !os.IsNotExist(err) {
			return false, false, fmt.Errorf("update: removing dropped file %q: %w", path, err)
		}
		return true, false, nil
	}
	fmt.Fprintf(m.stderr, "turutan: keeping locally modified %q dropped by the template\n", path)
	return false, false, nil
}

func (m *merger) mergeBoth(
	path string,
	locked config.LockFile,
	fresh, local []byte,
	localExists, preserved bool,
) (bool, bool, error) {
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
	fmt.Fprintf(m.stderr,
		"turutan: restoring %q changed in the template "+
			"but deleted locally\n",
		path)
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
		fmt.Fprintf(m.stderr,
			"turutan: no base copy for %q; using overlay merge\n", path)
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
	fmt.Fprintf(m.stderr,
		"turutan: binary %q changed both locally and "+
			"in the template; keeping local, wrote %q\n",
		path, path+".new")
	return false, true, nil
}

func (m *merger) threeWay(path string, local, fresh []byte) (wrote, conflicted, handled bool, err error) {
	base, err := filter.ReadFileWithinRoot(m.baseDir, path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, false, nil
		}
		return false, false, true, fmt.Errorf("update: reading base copy of %q: %w", path, err)
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
		fmt.Fprintf(m.stderr, "turutan: conflict in %q; keeping local, wrote %q\n", path, path+".rej")
		return false, true, handled, nil
	}
	if err := m.write(path, inlineConflict(local, fresh, shortSHA(m.newSHA))); err != nil {
		return false, false, handled, err
	}
	fmt.Fprintf(m.stderr, "turutan: conflict in %q; wrote inline markers\n", path)
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
	opts UpdateOptions,
	stdout io.Writer,
	stdin io.Reader,
) error {
	for range maxPromptAttempts {
		if err := os.RemoveAll(stage); err != nil {
			return fmt.Errorf("update: resetting staging dir: %w", err)
		}
		if err := os.MkdirAll(stage, 0o750); err != nil {
			return fmt.Errorf("update: creating staging dir: %w", err)
		}
		err := template.RenderDir(fetchedDir, stage, answers)
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
		value, promptErr := promptValue(stdout, stdin, key)
		if promptErr != nil {
			return promptErr
		}
		answers[key] = value
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
	if !promptConfirm(stdout, stdin, fmt.Sprintf("template declares migrations %q; allow", pending)) {
		fmt.Fprintf(stdout, "turutan: template migrations %q skipped (consent declined)\n", pending)
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
		fmt.Fprintf(stderr, "turutan: migration %q output:\n%s", cmd, output)
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
	state.RequestedRef = wantRef
	state.ResolvedCommit = fresh
	state.Answers = answers
	state.Template = src.String()
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
