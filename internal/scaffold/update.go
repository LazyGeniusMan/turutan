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
	"slices"
	"sort"
	"strings"

	"github.com/CivNode/diff3-go"

	"github.com/Masterminds/semver/v3"

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
	// Verbose enables per-run diagnostics on Stderr.
	Verbose bool
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
	if err := filter.ValidateGlobs(state.Skip); err != nil {
		return nil, fmt.Errorf("update: bad skip entry in state: %w", err)
	}
	answers, err := updateAnswers(projectDir, state, opts)
	if err != nil {
		return nil, err
	}
	vlogf(stderr, opts.Verbose, "update: stored commit %s requested ref %q", state.ResolvedCommit, state.RequestedRef)
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
	if _, err := checkMinEngine(manifest, opts.Engine, stderr, opts.Verbose); err != nil {
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
	if baseDirFor(projectDir, old) != "" {
		vlogf(stderr, opts.Verbose, "update: fresh commit %s base 3-way", fresh)
	} else {
		vlogf(stderr, opts.Verbose, "update: fresh commit %s base overlay (no pristine copy)", fresh)
	}
	merger := &merger{
		projectDir: projectDir,
		state:      state,
		manifest:   manifest,
		conflict:   conflict,
		newSHA:     fresh,
		baseDir:    baseDirFor(projectDir, old),
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
	// The staged render is the pristine new base: future updates merge
	// against it. A missing old base only downgraded this run to the v1
	// overlay; storing the new base upgrades the next one.
	if err := StoreBase(projectDir, fresh, stage); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	result := &UpdateResult{Old: old, New: fresh, Updated: updated, Conflicts: conflicts, Migrations: migrated}
	fmt.Fprintf(stdout, "updated %s -> %s (%d files, %d conflicts)\n", shortSHA(old), shortSHA(fresh), len(updated), len(conflicts))
	return result, nil
}

// baseDirFor returns the pristine base-copy directory for the stored old
// identity, or "" when no base exists and the merge must use the v1
// overlay fallback (pre-M5 projects, or a deleted store).
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
// the lock (missing counts as dirty). Reads go through os.Root so symlinks
// escaping the project are refused instead of followed. User-only files
// outside the lock never count: the guard protects template-tracked content.
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
// baseDir is the pristine base copy (.turutan/templates/<old-sha>/) when
// present; an empty baseDir selects the v1 overlay for every file.
type merger struct {
	projectDir string
	state      *config.State
	manifest   *config.Manifest
	conflict   ConflictMode
	newSHA     string
	baseDir    string
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
// written and whether the path needs attention. The project file is read
// through os.Root (escaping symlinks refused); lock rebuilding happens
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

// mergeAdded handles a file the new template introduces.
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
	return m.conflictBoth(path, local, fresh)
}

// mergeDropped handles a file the new template no longer renders: an
// unchanged local copy is deleted, a locally modified one is kept as a
// user file with a stderr note (and leaves the lock).
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

// mergeBoth handles a path tracked in the lock and present in the fresh
// render. With a pristine base copy it merges base↔local↔new per file
// (template-only takes new, local-only keeps local, disjoint both-changed
// regions merge cleanly); without a base it falls back to the v1 overlay
// (both-changed always conflicts). Preserve globs and binary sidecars
// behave the same in both modes.
func (m *merger) mergeBoth(path string, locked config.LockFile, fresh, local []byte, localExists, preserved bool) (bool, bool, error) {
	if !localExists {
		if config.FileEntry(path, fresh).SHA256 == locked.SHA256 {
			return false, false, nil
		}
		if err := m.write(path, fresh); err != nil {
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
		if err := m.write(path, fresh); err != nil {
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
			if err := m.write(path+".new", fresh); err != nil {
				return false, false, err
			}
			fmt.Fprintf(m.stderr, "turutan: binary %q changed both locally and in the template; keeping local, wrote %q\n", path, path+".new")
			return false, true, nil
		}
		if m.baseDir != "" {
			wrote, conflicted, handled, err := m.threeWay(path, local, fresh)
			if err != nil {
				return false, false, err
			}
			if handled {
				return wrote, conflicted, nil
			}
			// No usable base entry (the file postdates the stored base):
			// fall through to the v1 overlay below.
			fmt.Fprintf(m.stderr, "turutan: no base copy for %q; using overlay merge\n", path)
		}
		return m.conflictBoth(path, local, fresh)
	}
}

// threeWay merges a both-changed text file against its pristine base copy.
// Single-side changes apply directly; disjoint both-side changes merge via
// diff3-go; overlapping changes fall back to the v1 conflict recorder so
// inline|rej UX stays uniform. handled=false means the base holds no entry
// for path and the caller must use the v1 overlay instead.

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

// conflictBothAs records an unmergeable text hunk per the effective mode.
// inline writes markers into the file, rej keeps local and writes the
// unified diff to <file>.rej.
func (m *merger) conflictBoth(path string, local, fresh []byte) (bool, bool, error) {
	wrote, conflicted, _, err := m.conflictBothAs(path, local, fresh, true)
	return wrote, conflicted, err
}

func (m *merger) conflictBothAs(path string, local, fresh []byte, handled bool) (wrote, conflicted bool, _ bool, err error) {
	if m.conflict == ConflictRej {
		diff, changed := newFileDiff(path, local, fresh, false, false)
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

// write stores data at the project-relative path through os.Root, which
// refuses escapes and escaping symlinks. Parent dirs are created first.
func (m *merger) write(rel string, data []byte) error {
	if _, err := filter.SafeJoin(m.projectDir, filepath.FromSlash(rel)); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := filter.WriteFileWithinRoot(m.projectDir, rel, data, 0o644); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}

// runMigrations executes the new manifest's migrations in order whose From
// range matches old→new. An empty From always applies; a commit-prefix
// From applies on prefix match; a semver From is checked against whichever
// of old/fresh parse as versions (see matchesMigration). Each Run entry
// executes in the project dir: a plain path to a file runs via sh,
// anything else via sh -c. It returns the executed commands.
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

// matchesMigration reports whether a migration applies to old→new. An
// empty From always applies, and a commit-prefix From applies on prefix
// match. Otherwise From parses as a Masterminds/semver constraint checked
// against whichever of old/fresh parse as versions (tag-derived
// identities): a satisfied range applies, an unsatisfied one skips. When
// neither identity carries version info (bare SHAs) the range cannot
// resolve without tag metadata, so it applies in manifest order
// (documented match-all); an unparsable From matches all the same way.
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

// runMigrationCmd executes one migration command in projectDir.
func runMigrationCmd(projectDir, cmd, old, fresh string, stderr io.Writer) error {
	if cmd == "" {
		return fmt.Errorf("update: migration has an empty run command")
	}
	argv := []string{"-c", cmd}
	if !strings.ContainsAny(cmd, " \t\n|&;()<>$`\\\"'") {
		// A manifest-declared command doubles as a script path only when
		// it stays inside the project; escaping names run via sh -c.
		if safe, joinErr := filter.SafeJoin(projectDir, filepath.FromSlash(cmd)); joinErr == nil {
			if info, err := os.Stat(safe); err == nil && !info.IsDir() {
				argv = []string{safe}
			}
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
