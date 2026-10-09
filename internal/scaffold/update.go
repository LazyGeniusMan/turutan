// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

// updateDeepenDepth is the retry fetch depth when a historical --ref sits
// outside the default depth-1 shallow boundary (M2 noted the boundary;
// M3 retries deeper before failing with a ?depth= hint).
const updateDeepenDepth = 50

// maxDirtyShown bounds how many dirty paths appear in the guard error;
// the full count is always reported so the message stays short.
const maxDirtyShown = 5

// UpdateOptions carries per-run knobs for the update flow.
type UpdateOptions struct {
	// Ref overrides the stored requestedRef for this run; the stored
	// RequestedRef advances to Ref on success.
	Ref string
	// Conflict selects the conflict record mode; empty means the
	// manifest default (or inline when the manifest is silent).
	Conflict ConflictMode
	// Force bypasses the clean-tree guard (lock hashes match workdir).
	Force bool
	// AnswersFile overlays stored answers (file wins) for the re-render.
	AnswersFile string
	// Defaults seeds project_name from the project dir when the stored
	// answers plus the overlay still miss it.
	Defaults bool
	// Engine is the bare CLI version for the min-engine gate.
	Engine string
	// Stdout/Stderr/Stdin default to the OS streams when nil.
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

// UpdateResult reports the merge outcome: Old/New are the stored and
// freshly resolved identities, Updated lists written paths (including
// inline-conflicted files), Conflicts lists paths needing attention
// (inline markers, .rej or .new sidecars), Migrations lists executed
// migration commands in manifest order.
type UpdateResult struct {
	Old        string
	New        string
	Updated    []string
	Conflicts  []string
	Migrations []string
}

// Update merges template changes into the project (spec §§6.4,7):
//
//	require clean tree unless --force → fetch new template @ resolved
//	ref → render with stored answers (+ overlay) → v1 overlay merge
//	(write new, take-new on template-only change, keep local otherwise,
//	inline|rej on both-changed) → run ordered migrations → refresh
//	.turutan.json/.lock.
//
// Run `turutan diff` before `turutan update` to review pending drift.
// Binary files never merge (keep local + <file>.new + warn); preserve
// globs and state skip entries always prefer local.
func Update(projectDir string, opts UpdateOptions) (*UpdateResult, error) {
	stdout, stderr := updateStreams(opts)
	if err := opts.Conflict.Validate(); err != nil {
		return nil, err
	}
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	answers, err := updateAnswers(projectDir, state, opts)
	if err != nil {
		return nil, err
	}
	if !opts.Force {
		if dirty, err := dirtyFiles(projectDir, lock); err != nil {
			return nil, err
		} else if len(dirty) > 0 {
			return nil, fmt.Errorf("update: dirty tree (%d files differ from lock; use --force to overwrite): %s",
				len(dirty), strings.Join(firstN(dirty, maxDirtyShown), ", "))
		}
	}
	src, err := template.ParseSource(state.Template)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	wantRef := state.RequestedRef
	if opts.Ref != "" {
		wantRef = opts.Ref
		src.RequestedRef = opts.Ref
	}
	fetched, err := fetchWithDeepen(src)
	if err != nil {
		return nil, err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if _, err := checkMinEngine(manifest, opts.Engine, stderr); err != nil {
		return nil, err
	}
	conflict := effectiveConflict(opts.Conflict, manifest)
	stage, err := os.MkdirTemp("", "turutan-update-*")
	if err != nil {
		return nil, fmt.Errorf("update: creating staging dir: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := template.RenderDir(fetched.Dir, stage, answers); err != nil {
		if key := missingKey(err); key != "" {
			return nil, fmt.Errorf("update: missing answer for key %q (use --answers-file or --defaults): %w", key, err)
		}
		return nil, err
	}
	rendered, err := walkRendered(stage, manifest)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	old := state.ResolvedCommit
	fresh := fetched.ResolvedCommit
	if fresh == "" {
		fresh = strings.TrimPrefix(config.ComputeManifestHash(templateLockEntries(rendered)), "sha256:")
	}
	merger := &merger{
		projectDir: projectDir,
		state:      state,
		manifest:   manifest,
		conflict:   conflict,
		newSHA:     fresh,
		stderr:     stderr,
	}
	updated, conflicts, err := merger.merge(rendered, lock)
	if err != nil {
		return nil, err
	}
	migrated, err := runMigrations(projectDir, manifest, old, fresh, stderr)
	if err != nil {
		return nil, err
	}
	if err := refreshStateLock(projectDir, state, lock, src, wantRef, answers, fresh, rendered); err != nil {
		return nil, err
	}
	result := &UpdateResult{Old: old, New: fresh, Updated: updated, Conflicts: conflicts, Migrations: migrated}
	fmt.Fprintf(stdout, "updated %s -> %s (%d files, %d conflicts)\n", shortSHA(old), shortSHA(fresh), len(updated), len(conflicts))
	return result, nil
}

// updateStreams resolves the effective output streams.
func updateStreams(opts UpdateOptions) (io.Writer, io.Writer) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	return stdout, stderr
}

// updateAnswers starts from the stored answers, seeds project_name when
// --defaults asks and overlays --answers-file (file wins).
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

// dirtyFiles lists lock-tracked paths whose workdir content differs from
// the lock (missing counts as dirty). User-only files outside the lock
// never count: the guard protects template-tracked content.
func dirtyFiles(projectDir string, lock *config.Lock) ([]string, error) {
	var dirty []string
	for _, entry := range lock.Files {
		data, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(entry.Path)))
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

// firstN returns the first n entries for short error messages.
func firstN(paths []string, n int) []string {
	if len(paths) <= n {
		return paths
	}
	return paths[:n]
}

// fetchWithDeepen fetches src, retrying once with a deeper history when a
// historical ref sits outside the depth-1 shallow boundary. Filesystem
// sources never retry (no refs, no depth).
func fetchWithDeepen(src *template.Source) (*template.Fetched, error) {
	fetched, err := template.Fetch(src)
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
	if refetched, retryErr := template.Fetch(&retry); retryErr == nil {
		return refetched, nil
	}
	return nil, fmt.Errorf("update: %w (template history is shallow; fetch with ?depth= to reach further back)", err)
}

// effectiveConflict resolves the conflict mode: per-run flag wins, then
// the manifest default, then inline.
func effectiveConflict(flag ConflictMode, manifest *config.Manifest) ConflictMode {
	if flag != "" {
		return flag
	}
	if manifest.Conflict != "" {
		return ConflictMode(manifest.Conflict)
	}
	return ConflictInline
}

// templateLockEntries converts a staged render into lock entries for the
// filesystem content identity (template bytes, not workdir).
func templateLockEntries(rendered map[string]renderedEntry) []config.LockFile {
	files := make([]config.LockFile, 0, len(rendered))
	for _, entry := range rendered {
		files = append(files, config.FileEntry(entry.Path, entry.Data))
	}
	return files
}

// merger holds per-run merge state so the hot loop stays small.
type merger struct {
	projectDir string
	state      *config.State
	manifest   *config.Manifest
	conflict   ConflictMode
	newSHA     string
	stderr     io.Writer
}

// merge applies the v1 overlay over the union of lock and new paths and
// returns the updated and conflicted path lists (both sorted).
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
		wrote, conflicted, err := m.mergeOne(path, lockMap[path], rendered[path], inLock(lockMap, path), inRendered(rendered, path))
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

// inLock reports whether path is tracked in the lock map.
func inLock(lockMap map[string]config.LockFile, path string) bool {
	_, ok := lockMap[path]
	return ok
}

// inRendered reports whether path arrived in the fresh render.
func inRendered(rendered map[string]renderedEntry, path string) bool {
	_, ok := rendered[path]
	return ok
}

// mergeOne merges a single path and reports whether the main file was
// written and whether the path needs attention. Lock rebuilding happens
// after the full loop (plus migrations), so this only touches workdir.
func (m *merger) mergeOne(path string, locked config.LockFile, fresh renderedEntry, hasLocked, hasFresh bool) (bool, bool, error) {
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
	dst, err := filter.SafeJoin(m.projectDir, filepath.FromSlash(path))
	if err != nil {
		return false, false, fmt.Errorf("update: %w", err)
	}
	local, err := os.ReadFile(dst)
	if err != nil && !os.IsNotExist(err) {
		return false, false, fmt.Errorf("update: reading project file %q: %w", path, err)
	}
	localExists := err == nil
	switch {
	case hasFresh && !hasLocked:
		return m.mergeAdded(path, dst, fresh.Data, local, localExists, preserved)
	case !hasFresh && hasLocked:
		return m.mergeDropped(path, dst, locked, local, localExists)
	default:
		return m.mergeBoth(path, dst, locked, fresh.Data, local, localExists, preserved)
	}
}

// mergeAdded handles a file the new template introduces.
func (m *merger) mergeAdded(path, dst string, fresh, local []byte, localExists, preserved bool) (bool, bool, error) {
	if !localExists {
		if err := writeFile(dst, fresh); err != nil {
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
	return m.conflictBoth(path, dst, local, fresh)
}

// mergeDropped handles a file the new template no longer renders: an
// unchanged local copy is deleted, a locally modified one is kept as a
// user file with a stderr note (and leaves the lock).
func (m *merger) mergeDropped(path, dst string, locked config.LockFile, local []byte, localExists bool) (bool, bool, error) {
	if !localExists {
		return false, false, nil
	}
	if config.FileEntry(path, local).SHA256 == locked.SHA256 {
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return false, false, fmt.Errorf("update: removing dropped file %q: %w", path, err)
		}
		return true, false, nil
	}
	fmt.Fprintf(m.stderr, "turutan: keeping locally modified %q dropped by the template\n", path)
	return false, false, nil
}

// mergeBoth handles a path tracked in the lock and present in the fresh
// render following the overlay rules: template-only change takes new,
// local-only change keeps local, both-changed conflicts (or prefers local
// for preserve globs and binary sidecars).
func (m *merger) mergeBoth(path, dst string, locked config.LockFile, fresh, local []byte, localExists, preserved bool) (bool, bool, error) {
	if !localExists {
		if config.FileEntry(path, fresh).SHA256 == locked.SHA256 {
			return false, false, nil
		}
		if err := writeFile(dst, fresh); err != nil {
			return false, false, err
		}
		fmt.Fprintf(m.stderr, "turutan: restoring %q changed in the template but deleted locally\n", path)
		return true, false, nil
	}
	lockHash := locked.SHA256
	localHash := config.FileEntry(path, local).SHA256
	freshHash := config.FileEntry(path, fresh).SHA256
	templateChanged := lockHash != freshHash
	localChanged := lockHash != localHash
	switch {
	case !templateChanged && !localChanged:
		return false, false, nil
	case !templateChanged && localChanged:
		return false, false, nil
	case templateChanged && !localChanged:
		if err := writeFile(dst, fresh); err != nil {
			return false, false, err
		}
		return true, false, nil
	default:
		if bytes.Equal(local, fresh) {
			return false, false, nil
		}
		if preserved {
			return false, false, nil
		}
		if isBinary(local) || isBinary(fresh) {
			if err := writeFile(dst+".new", fresh); err != nil {
				return false, false, err
			}
			fmt.Fprintf(m.stderr, "turutan: binary %q changed both locally and in the template; keeping local, wrote %q\n", path, path+".new")
			return false, true, nil
		}
		return m.conflictBoth(path, dst, local, fresh)
	}
}

// conflictBoth records an unmergeable text hunk per the effective mode:
// inline writes markers into the file, rej keeps local and writes the
// unified diff to <file>.rej.
func (m *merger) conflictBoth(path, dst string, local, fresh []byte) (bool, bool, error) {
	if m.conflict == ConflictRej {
		diff, changed := newFileDiff(path, local, fresh, false, false)
		if !changed {
			return false, false, nil
		}
		if err := writeFile(dst+".rej", []byte(diff.Unified)); err != nil {
			return false, false, err
		}
		fmt.Fprintf(m.stderr, "turutan: conflict in %q; keeping local, wrote %q\n", path, path+".rej")
		return false, true, nil
	}
	if err := writeFile(dst, inlineConflict(local, fresh, shortSHA(m.newSHA))); err != nil {
		return false, false, err
	}
	fmt.Fprintf(m.stderr, "turutan: conflict in %q; wrote inline markers\n", path)
	return true, true, nil
}

// inlineConflict renders local and fresh with conflict markers: the local
// body between <<<<<<< local and =======, the template body between
// ======= and >>>>>>> template-<sha>.
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

// isBinary reports whether data looks binary: a NUL byte anywhere means
// git-style binary (never merged, always sidecarred).
func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) != -1
}

// writeFile creates parent dirs and writes data with regular file perms.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("update: creating parent dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("update: writing %q: %w", filepath.Base(path), err)
	}
	return nil
}

// runMigrations executes the new manifest's migrations in order whose From
// range matches old→new. An empty From always applies; a commit-prefix
// From applies on prefix match; any other (semver-style) range applies
// for M3 v1 with tag-range filtering deferred to the base-store milestone.
// Each Run entry executes in the project dir: a plain path to a file runs
// via sh, anything else via sh -c. It returns the executed commands.
func runMigrations(projectDir string, manifest *config.Manifest, old, fresh string, stderr io.Writer) ([]string, error) {
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

// matchesMigration reports whether a migration applies to old→new.
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
	// Semver-style ranges cannot resolve against commit identities
	// without tag metadata; M3 v1 applies them in manifest order.
	return true
}

// runMigrationCmd executes one migration command in projectDir.
func runMigrationCmd(projectDir, cmd, old, fresh string, stderr io.Writer) error {
	if cmd == "" {
		return fmt.Errorf("update: migration has an empty run command")
	}
	argv := []string{"-c", cmd}
	if !strings.ContainsAny(cmd, " \t\n|&;()<>$`\\\"'") {
		if info, err := os.Stat(filepath.Join(projectDir, filepath.FromSlash(cmd))); err == nil && !info.IsDir() {
			argv = []string{filepath.Join(projectDir, filepath.FromSlash(cmd))}
		}
	}
	executed := exec.Command("sh", argv...)
	executed.Dir = projectDir
	executed.Env = append(os.Environ(), "TURUTAN_OLD="+old, "TURUTAN_NEW="+fresh)
	output, err := executed.CombinedOutput()
	if len(output) > 0 {
		fmt.Fprintf(stderr, "turutan: migration %q output:\n%s", cmd, output)
	}
	if err != nil {
		return fmt.Errorf("update: migration %q failed: %w", cmd, err)
	}
	return nil
}

// refreshStateLock advances the stored identities and rebuilds the lock
// from the final workdir (post-merge, post-migrations) so the tree is
// clean: every fresh-template path contributes its workdir hash.
func refreshStateLock(projectDir string, state *config.State, lock *config.Lock, src *template.Source, wantRef string, answers map[string]any, fresh string, rendered map[string]renderedEntry) error {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	// The staged render already excluded manifest-ignored paths, so every
	// entry here is lock-eligible; paths deleted by the merge (dropped
	// template files, user deletions) are absent from workdir and skipped.
	final := make([]config.LockFile, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(path)))
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
	// Keep the stored locator in sync when --ref moved it. src carries
	// the original depth (deepen retries copy it), so String persists
	// only a user-requested ?depth=.
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
