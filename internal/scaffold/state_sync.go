// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

var pinnedSHARe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type SyncOptions struct {
	Conflict       ConflictMode
	Force          bool
	AllowHooks     bool
	Preview        bool
	NonInteractive bool
	Verbose        bool
	Engine         string
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
}

type InputOptions struct {
	Conflict       ConflictMode
	Force          bool
	DataFile       string
	Defaults       bool
	AllowHooks     bool
	Preview        bool
	NonInteractive bool
	Verbose        bool
	Engine         string
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
}

func StateSync(
	ctx context.Context,
	projectDir string,
	opts SyncOptions,
) (*UpdateResult, error) {
	stdout, stderr, stdin := resolveStreams(opts.Stdout, opts.Stderr, opts.Stdin)
	if err := opts.Conflict.Validate(); err != nil {
		return nil, err
	}
	state, lock, err := loadSyncState(projectDir)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	maps.Copy(data, state.Data)
	src, fetched, manifest, err := fetchPinnedTemplate(
		ctx, projectDir, state, "sync", opts.Engine, stderr, opts.Verbose)
	if err != nil {
		return nil, err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	if opts.Conflict != "" {
		state.Conflict = string(opts.Conflict)
	}
	uopts := syncUpdateOptions(opts, stdout, stderr, stdin)
	if opts.Preview {
		return previewStateSync(
			projectDir, state, lock, fetched, manifest, data,
			uopts, stdout, stderr, stdin)
	}
	return runStateSync(
		ctx, projectDir, state, lock, src, fetched, manifest, data,
		uopts, stdout, stderr, stdin, "")
}

func StateInput(
	ctx context.Context,
	projectDir string,
	opts InputOptions,
) (*UpdateResult, error) {
	stdout, stderr, stdin := resolveStreams(opts.Stdout, opts.Stderr, opts.Stdin)
	if err := opts.Conflict.Validate(); err != nil {
		return nil, err
	}
	if opts.NonInteractive && !opts.Defaults && opts.DataFile == "" {
		return nil, fmt.Errorf(
			"state input: non-interactive mode requires " +
				"--data-file or --defaults")
	}
	state, lock, err := loadSyncState(projectDir)
	if err != nil {
		return nil, err
	}
	src, fetched, manifest, err := fetchPinnedTemplate(
		ctx, projectDir, state, "state input", opts.Engine, stderr, opts.Verbose)
	if err != nil {
		return nil, err
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	if opts.Conflict != "" {
		state.Conflict = string(opts.Conflict)
	}
	orig := map[string]any{}
	maps.Copy(orig, state.Data)
	data := map[string]any{}
	maps.Copy(data, state.Data)
	if opts.Defaults && data["project_name"] == nil {
		abs, err := filepath.Abs(projectDir)
		if err != nil {
			return nil, fmt.Errorf("state input: resolving project dir: %w", err)
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
	if opts.NonInteractive {
		if err := ensureInputValues(
			data, manifest, true, opts.Defaults, stdout, stdin, "state input"); err != nil {
			return nil, err
		}
	} else {
		if err := repromptAllInputs(data, manifest, stdout, stdin); err != nil {
			return nil, err
		}
	}
	normalizeDataWithManifest(data, manifest)
	changed := changedDataKeys(orig, data)
	uopts := inputUpdateOptions(opts, stdout, stderr, stdin)
	if opts.Preview {
		return previewStateSync(
			projectDir, state, lock, fetched, manifest, data,
			uopts, stdout, stderr, stdin)
	}
	return runStateSync(
		ctx, projectDir, state, lock, src, fetched, manifest, data,
		uopts, stdout, stderr, stdin, formatChangedKeys(changed))
}

func loadSyncState(projectDir string) (*config.State, *config.Lock, error) {
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, fmt.Errorf("sync: %w", err)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, fmt.Errorf("sync: %w", err)
	}
	if err := filter.ValidateGlobs(state.Skip); err != nil {
		return nil, nil, fmt.Errorf(
			"sync: bad skip entry in state: %w", err)
	}
	return state, lock, nil
}

func syncUpdateOptions(
	opts SyncOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
) UpdateOptions {
	return UpdateOptions{
		Conflict:       opts.Conflict,
		Force:          opts.Force,
		AllowHooks:     opts.AllowHooks,
		Preview:        opts.Preview,
		NonInteractive: opts.NonInteractive,
		Verbose:        opts.Verbose,
		Engine:         opts.Engine,
		Stdout:         stdout,
		Stderr:         stderr,
		Stdin:          stdin,
	}
}

func inputUpdateOptions(
	opts InputOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
) UpdateOptions {
	return UpdateOptions{
		Conflict:       opts.Conflict,
		Force:          opts.Force,
		DataFile:       opts.DataFile,
		Defaults:       opts.Defaults,
		AllowHooks:     opts.AllowHooks,
		Preview:        opts.Preview,
		NonInteractive: opts.NonInteractive,
		Verbose:        opts.Verbose,
		Engine:         opts.Engine,
		Stdout:         stdout,
		Stderr:         stderr,
		Stdin:          stdin,
	}
}

func runStateSync(
	_ context.Context,
	projectDir string,
	state *config.State,
	lock *config.Lock,
	src *template.Source,
	fetched *template.Fetched,
	manifest *config.Manifest,
	data map[string]any,
	uopts UpdateOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
	changedLine string,
) (*UpdateResult, error) {
	stage, err := os.MkdirTemp("", "turutan-sync-*")
	if err != nil {
		return nil, fmt.Errorf("sync: creating staging dir: %w", err)
	}
	rendered, err := renderStaged(
		fetched, stage, manifest, data, uopts, stdout, stdin)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	preserved, err := preservedGlobs(data, manifest)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	old := state.ResolvedCommit
	fresh := old
	vlogf(stderr, uopts.Verbose, "sync: pinned commit %s", old)
	merger := newMerger(
		projectDir, state, manifest, preserved, uopts, fresh, old, stderr)
	skip, err := gateMigrations(manifest, old, fresh, uopts, stdout, stdin)
	if err != nil {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if !uopts.Force {
		if err := guardCleanTree(
			projectDir, lock, manifest, state, merger.preserved); err != nil {
			return nil, err
		}
	}
	updated, conflicts, err := merger.merge(rendered, lock)
	if err != nil {
		return nil, err
	}
	var migrated []string
	if !skip {
		migrated, err = runMigrations(projectDir, manifest, old, fresh, stderr)
		if err != nil {
			return nil, err
		}
	}
	if changedLine != "" {
		if _, err := fmt.Fprintln(stdout, changedLine); err != nil {
			return nil, err
		}
	}
	return finishStateSync(
		projectDir, state, lock, data, fresh, rendered, stage,
		stdout, updated, conflicts, migrated, old)
}

func previewStateSync(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	fetched *template.Fetched,
	manifest *config.Manifest,
	data map[string]any,
	uopts UpdateOptions,
	stdout, stderr io.Writer,
	stdin io.Reader,
) (*UpdateResult, error) {
	stage, err := os.MkdirTemp("", "turutan-sync-*")
	if err != nil {
		return nil, fmt.Errorf("sync: creating staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	rendered, err := renderStaged(
		fetched, stage, manifest, data, uopts, stdout, stdin)
	if err != nil {
		return nil, err
	}
	preserved, err := preservedGlobs(data, manifest)
	if err != nil {
		return nil, err
	}
	old := state.ResolvedCommit
	vlogf(stderr, uopts.Verbose, "sync: preview %s", old)
	plan := previewPlan(projectDir, rendered, lock, manifest, state, preserved)
	if err := printPreview(stdout, plan); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(plan))
	for _, entry := range plan {
		paths = append(paths, entry.Path)
	}
	return &UpdateResult{Old: old, New: old, Updated: paths}, nil
}

func finishStateSync(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	data map[string]any,
	fresh string,
	rendered map[string]renderedEntry,
	stage string,
	stdout io.Writer,
	updated, conflicts, migrated []string,
	old string,
) (*UpdateResult, error) {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	final := make([]config.LockFile, 0, len(paths))
	for _, path := range paths {
		content, err := filter.ReadFileWithinRoot(projectDir, path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("sync: reading merged file %q: %w", path, err)
		}
		final = append(final, config.FileEntry(path, content))
	}
	state.Data = data
	if err := config.SaveState(projectDir, state); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	lock.Source = state.Template
	lock.Ref = state.RequestedRef
	lock.ResolvedSHA = fresh
	lock.Subpath = state.Subpath
	lock.Files = final
	if err := config.SaveLock(projectDir, lock); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	if err := StoreBase(projectDir, fresh, stage); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	result := &UpdateResult{
		Old: old, New: fresh,
		Updated: updated, Conflicts: conflicts, Migrations: migrated,
	}
	if _, err := fmt.Fprintf(stdout, "synced %s (%d files, %d conflicts)\n",
		shortSHA(old), len(updated), len(conflicts)); err != nil {
		return nil, err
	}
	return result, nil
}

func fetchPinnedTemplate(
	ctx context.Context,
	projectDir string,
	state *config.State,
	op, engine string,
	stderr io.Writer,
	verbose bool,
) (*template.Source, *template.Fetched, *config.Manifest, error) {
	vlogf(stderr, verbose,
		"%s: pinned commit %s requested ref %q",
		op, state.ResolvedCommit, state.RequestedRef)
	src, err := parseStoredSource(projectDir, state.Template)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	var fetched *template.Fetched
	switch template.SourceKind(state.SourceKind) {
	case template.KindRemoteGit:
		fetched, err = fetchPinnedGit(ctx, src, state.ResolvedCommit, true, op)
	case template.KindLocalGit:
		fetched, err = fetchPinnedGit(ctx, src, state.ResolvedCommit, false, op)
	case template.KindFilesystem:
		fetched, err = template.Fetch(ctx, src)
		if err == nil && fetched != nil {
			fetched.ResolvedCommit = state.ResolvedCommit
		}
	default:
		return nil, nil, nil, fmt.Errorf(
			"%s: unknown sourceKind %q", op, state.SourceKind)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		if fetched.Cleanup != nil {
			fetched.Cleanup()
		}
		return nil, nil, nil, fmt.Errorf("%s: %w", op, err)
	}
	if _, err := checkMinEngine(manifest, engine, stderr, verbose); err != nil {
		if fetched.Cleanup != nil {
			fetched.Cleanup()
		}
		return nil, nil, nil, err
	}
	return src, fetched, manifest, nil
}

func fetchPinnedGit(
	ctx context.Context,
	src *template.Source,
	sha string,
	useCache bool,
	op string,
) (*template.Fetched, error) {
	if useCache && template.IsDefaultSource(src) && pinnedSHARe.MatchString(sha) {
		if dir, ok := pinnedDefaultCache(sha); ok {
			sub, err := pinnedSubdir(dir, src, op)
			if err == nil {
				return &template.Fetched{
					Dir: sub, Source: src, ResolvedCommit: strings.ToLower(sha),
				}, nil
			}
		}
	}
	depth := src.Depth
	if depth <= 0 {
		depth = template.DefaultDepth
	}
	_, dir, cleanup, err := clonePinnedAt(ctx, src, sha, depth, op)
	if err == nil {
		return &template.Fetched{
			Dir: dir, Cleanup: cleanup, Source: src, ResolvedCommit: sha,
		}, nil
	}
	if depth != template.DefaultDepth {
		return nil, err
	}
	_, dir, cleanup, retryErr := clonePinnedAt(
		ctx, src, sha, updateDeepenDepth, op)
	if retryErr == nil {
		return &template.Fetched{
			Dir: dir, Cleanup: cleanup, Source: src, ResolvedCommit: sha,
		}, nil
	}
	return nil, fmt.Errorf(
		"%w (template history is shallow; fetch with ?depth= to reach further back)", err)
}

func clonePinnedAt(
	ctx context.Context,
	src *template.Source,
	sha string,
	depth int,
	op string,
) (string, string, func(), error) {
	tmp, err := os.MkdirTemp("", "turutan-pinned-*")
	if err != nil {
		return "", "", nil, fmt.Errorf(
			"%s: creating temp dir: %w", op, err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if _, err := git.Clone(ctx, src.Repo, "", tmp, depth); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("%s: %w", op, err)
	}
	if err := git.Checkout(tmp, sha); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("%s: %w", op, err)
	}
	dir, err := pinnedSubdir(tmp, src, op)
	if err != nil {
		cleanup()
		return "", "", nil, err
	}
	return tmp, dir, cleanup, nil
}

func pinnedSubdir(root string, src *template.Source, op string) (string, error) {
	if src.Subpath == "" {
		return root, nil
	}
	dir, err := filter.SafeJoin(root, filepath.FromSlash(src.Subpath))
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf(
			"%s: fetching source %q: subpath %q: %w", op, src.Raw, src.Subpath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf(
			"%s: fetching source %q: subpath %q is not a directory",
			op, src.Raw, src.Subpath)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf(
			"%s: fetching source %q: resolving subpath: %w", op, src.Raw, err)
	}
	if err := filter.EnsureWithinRoot(root, resolved); err != nil {
		return "", fmt.Errorf("%s: fetching source %q: %w", op, src.Raw, err)
	}
	return dir, nil
}

func pinnedDefaultCache(sha string) (string, bool) {
	cache := config.ResolveCacheDir()
	if cache == "" {
		return "", false
	}
	dir := filepath.Join(cache, "default", strings.ToLower(sha))
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

func repromptAllInputs(
	data map[string]any,
	manifest *config.Manifest,
	stdout io.Writer,
	stdin io.Reader,
) error {
	if manifest == nil {
		return nil
	}
	order := inputOrder(manifest)
	asked := map[string]bool{}
	force := false
	for range maxInputAttempts {
		deferred := 0
		progressed := false
		for _, key := range order {
			if asked[key] {
				continue
			}
			def := manifest.Inputs[key]
			ask, err := ShouldAskInput(manifest, key, renderDataView(data))
			if err != nil {
				if _, ok := AsMissingKey(err); ok {
					if !force {
						deferred++
						continue
					}
				} else {
					return err
				}
			} else if !ask {
				asked[key] = true
				progressed = true
				continue
			}
			disp := def
			if stored, ok := data[key]; ok {
				disp.Default = stored
			}
			value, err := inputForKey(stdout, stdin, key, disp)
			if err != nil {
				return err
			}
			data[key] = value
			asked[key] = true
			progressed = true
		}
		if deferred == 0 {
			return nil
		}
		if !progressed {
			if force {
				return fmt.Errorf(
					"state input: too many missing values (over %d inputs)",
					maxInputAttempts)
			}
			force = true
		}
	}
	return fmt.Errorf(
		"state input: too many missing values (over %d inputs)",
		maxInputAttempts)
}

func changedDataKeys(old, fresh map[string]any) []string {
	seen := map[string]bool{}
	var keys []string
	for key := range old {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range fresh {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	var changed []string
	for _, key := range keys {
		if !inputValuesEqual(old[key], fresh[key]) {
			changed = append(changed, key)
		}
	}
	slices.Sort(changed)
	return changed
}

func formatChangedKeys(changed []string) string {
	if len(changed) == 0 {
		return "turutan: input unchanged"
	}
	return fmt.Sprintf(
		"turutan: input updated keys: %s", strings.Join(changed, ", "))
}

func inputValuesEqual(first, second any) bool {
	if reflect.DeepEqual(first, second) {
		return true
	}
	if isSliceValue(first) || isSliceValue(second) {
		return slices.Equal(
			normalizeArrayValue(first), normalizeArrayValue(second))
	}
	return fmt.Sprint(first) == fmt.Sprint(second)
}

func isSliceValue(val any) bool {
	switch val.(type) {
	case []string, []any:
		return true
	default:
		return false
	}
}
