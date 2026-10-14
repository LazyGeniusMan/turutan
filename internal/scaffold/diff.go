// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

const DiffContextLines = 3

// DiffWhat selectors choose which side of a drift comparison to report.
// Update review needs upstream template changes and local edits separately,
// so diff --what scopes the same engine to either side or both.
// ~keep
const (
	// DiffWhatAll compares the fresh render against the worktree, including
	// files the new template dropped, for the default full review.
	// ~keep
	DiffWhatAll = "all"
	// DiffWhatUpstream compares the fresh render against the recorded base so
	// reviewers see template-side changes without local edits in the way.
	// ~keep
	DiffWhatUpstream = "upstream"
	// DiffWhatLocal compares the recorded base against the worktree so
	// reviewers see local edits without upstream template churn in the way.
	// ~keep
	DiffWhatLocal = "local"
)

type DiffOptions struct {
	Ref     string
	Engine  string
	NoColor bool
	Verbose bool
	What    string
	Stderr  io.Writer
}

type FileDiff struct {
	Path      string
	Unified   string
	Added     int
	Removed   int
	IsNew     bool
	IsDeleted bool
}

func ComputeDiff(
	ctx context.Context,
	projectDir string,
	opts DiffOptions,
) ([]FileDiff, error) {
	what, err := normalizeDiffWhat(opts.What)
	if err != nil {
		return nil, err
	}
	state, lock, src, err := loadDiffInputs(projectDir, opts)
	if err != nil {
		return nil, err
	}
	switch what {
	case DiffWhatUpstream:
		return computeUpstreamDiff(ctx, projectDir, state, src, opts)
	case DiffWhatLocal:
		return computeLocalDiff(projectDir, state, lock, opts)
	default:
		return computeFullDiff(ctx, projectDir, state, lock, src, opts)
	}
}

func normalizeDiffWhat(what string) (string, error) {
	trimmed := strings.TrimSpace(what)
	if trimmed == "" {
		return DiffWhatAll, nil
	}
	switch trimmed {
	case DiffWhatAll, DiffWhatUpstream, DiffWhatLocal:
		return trimmed, nil
	default:
		return "", fmt.Errorf(
			"diff: unknown --what %q (want all|upstream|local)", what)
	}
}

func computeFullDiff(
	ctx context.Context,
	projectDir string,
	state *config.State,
	lock *config.Lock,
	src *template.Source,
	opts DiffOptions,
) ([]FileDiff, error) {
	rendered, manifest, err := renderDiffTemplate(
		ctx, src, state, opts)
	if err != nil {
		return nil, err
	}
	diffs, err := collectDiffs(projectDir, rendered, lock, manifest, state)
	if err != nil {
		return nil, err
	}
	vlogf(opts.Stderr, opts.Verbose,
		"diff: compared %d rendered file(s), %d drifted",
		len(rendered), len(diffs))
	return diffs, nil
}

func computeUpstreamDiff(
	ctx context.Context,
	projectDir string,
	state *config.State,
	src *template.Source,
	opts DiffOptions,
) ([]FileDiff, error) {
	rendered, manifest, err := renderDiffTemplate(
		ctx, src, state, opts)
	if err != nil {
		return nil, err
	}
	base, err := readBaseEntries(projectDir, state.ResolvedCommit)
	if err != nil {
		return nil, err
	}
	byNew := make(map[string][]byte, len(base))
	for path, data := range base {
		byNew[config.ResolveRenames(path, manifest.Renames)] = data
	}
	var diffs []FileDiff
	for _, path := range renderedPaths(rendered) {
		diff, changed, err := newFileDiff(
			path, byNew[path], rendered[path].Data, false, false)
		if err != nil {
			return nil, err
		}
		if changed {
			if _, ok := byNew[path]; !ok {
				diff.IsNew = true
			}
			diffs = append(diffs, diff)
		}
	}
	for _, path := range basePaths(byNew) {
		if _, ok := rendered[path]; ok {
			continue
		}
		ignored, err := filter.MatchAny(manifest.Excludes, path)
		if err != nil {
			return nil, fmt.Errorf("diff: %w", err)
		}
		if ignored {
			continue
		}
		diff, _, err := newFileDiff(path, byNew[path], nil, false, true)
		if err != nil {
			return nil, err
		}
		diffs = append(diffs, diff)
	}
	vlogf(opts.Stderr, opts.Verbose,
		"diff: compared %d base file(s) with %d rendered file(s), %d drifted",
		len(byNew), len(rendered), len(diffs))
	return diffs, nil
}

func computeLocalDiff(
	projectDir string,
	state *config.State,
	lock *config.Lock,
	opts DiffOptions,
) ([]FileDiff, error) {
	base, err := readBaseEntries(projectDir, state.ResolvedCommit)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(base)+len(lock.Files))
	seen := map[string]bool{}
	for path := range base {
		paths = append(paths, path)
		seen[path] = true
	}
	for _, entry := range lock.Files {
		if !seen[entry.Path] {
			paths = append(paths, entry.Path)
			seen[entry.Path] = true
		}
	}
	sort.Strings(paths)
	var diffs []FileDiff
	for _, path := range paths {
		local, err := filter.ReadFileWithinRoot(projectDir, path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf(
					"diff: reading project file %q: %w", path, err)
			}
			diff, _, err := newFileDiff(path, base[path], nil, false, true)
			if err != nil {
				return nil, err
			}
			diffs = append(diffs, diff)
			continue
		}
		diff, changed, err := newFileDiff(
			path, base[path], local, len(base[path]) == 0 && len(local) > 0, false)
		if err != nil {
			return nil, err
		}
		if changed {
			diffs = append(diffs, diff)
		}
	}
	vlogf(opts.Stderr, opts.Verbose,
		"diff: compared %d base file(s) with the worktree, %d drifted",
		len(base), len(diffs))
	return diffs, nil
}

func readBaseEntries(projectDir, sha string) (map[string][]byte, error) {
	dir := baseDirFor(projectDir, sha)
	if dir == "" {
		return nil, fmt.Errorf(
			"diff: no base copy for %q; run turutan update to record one",
			shortSHA(sha))
	}
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." || d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if filter.IsSpecialFile(info) || !info.Mode().IsRegular() {
			return nil
		}
		slash := filter.ToSlash(rel)
		data, err := os.ReadFile(path) // #nosec G304, G122 -- reads file found by WalkDir of this run's base copy under .turutan/templates, not caller-controlled inclusion
		if err != nil {
			return err
		}
		out[slash] = data
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("diff: reading base copy: %w", err)
	}
	return out, nil
}

func basePaths(base map[string][]byte) []string {
	paths := make([]string, 0, len(base))
	for path := range base {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func loadDiffInputs(
	projectDir string,
	opts DiffOptions,
) (*config.State, *config.Lock, *template.Source, error) {
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("diff: %w", err)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("diff: %w", err)
	}
	src, err := parseStoredSource(projectDir, state.Template)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("diff: %w", err)
	}
	if opts.Ref != "" {
		src.RequestedRef = opts.Ref
	}
	vlogf(opts.Stderr, opts.Verbose,
		"diff: template %q ref %q", state.Template, src.RequestedRef)
	return state, lock, src, nil
}

func renderDiffTemplate(
	ctx context.Context,
	src *template.Source,
	state *config.State,
	opts DiffOptions,
) (map[string]renderedEntry, *config.Manifest, error) {
	fetched, err := template.Fetch(ctx, src)
	if err != nil {
		return nil, nil, fmt.Errorf("diff: %w", err)
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return nil, nil, fmt.Errorf("diff: %w", err)
	}
	if _, err := checkMinEngine(
		manifest, opts.Engine, opts.Stderr, opts.Verbose); err != nil {
		return nil, nil, err
	}
	stage, err := os.MkdirTemp("", "turutan-diff-*")
	if err != nil {
		return nil, nil, fmt.Errorf("diff: creating staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	data := map[string]any{}
	maps.Copy(data, state.Data)
	seedMissingDefaults(data, manifest)
	normalizeDataWithManifest(data, manifest)
	view := renderDataView(data)
	if err := template.RenderDirWithConditionals(fetched.Dir, stage, view, manifest.Excludes, manifestConditionals(manifest)); err != nil {
		return nil, nil, fmt.Errorf("diff: %w", err)
	}
	if err := plantLicenseFiles(fetched.Dir, stage, manifest); err != nil {
		return nil, nil, fmt.Errorf("diff: %w", err)
	}
	if err := plantDocFiles(stage, manifest); err != nil {
		return nil, nil, fmt.Errorf("diff: %w", err)
	}
	rendered, err := walkRendered(stage, manifest)
	if err != nil {
		return nil, nil, fmt.Errorf("diff: %w", err)
	}
	return rendered, manifest, nil
}

func collectDiffs(
	projectDir string,
	rendered map[string]renderedEntry,
	lock *config.Lock,
	manifest *config.Manifest,
	state *config.State,
) ([]FileDiff, error) {
	preserved, err := preservedGlobs(state.Data, manifest)
	if err != nil {
		return nil, err
	}
	var diffs []FileDiff
	for _, path := range renderedPaths(rendered) {
		diff, changed, err := diffRenderedOne(projectDir, rendered[path], manifest, state, preserved)
		if err != nil {
			return nil, err
		}
		if changed {
			diffs = append(diffs, diff)
		}
	}
	for _, entry := range lock.Files {
		diff, changed, err := diffDroppedOne(projectDir, rendered, manifest, entry, preserved)
		if err != nil {
			return nil, err
		}
		if changed {
			diffs = append(diffs, diff)
		}
	}
	return diffs, nil
}

func diffRenderedOne(
	projectDir string,
	entry renderedEntry,
	manifest *config.Manifest,
	state *config.State,
	preserved []string,
) (FileDiff, bool, error) {
	local, err := filter.ReadFileWithinRoot(projectDir, entry.Path)
	if err != nil {
		if !os.IsNotExist(err) {
			return FileDiff{}, false, fmt.Errorf(
				"diff: reading project file %q: %w", entry.Path, err)
		}
		diff, _, err := newFileDiff(entry.Path, nil, entry.Data, true, false)
		if err != nil {
			return FileDiff{}, false, err
		}
		return diff, true, nil
	}
	if state != nil {
		owned, err := userOwned(entry.Path, manifest, state, preserved)
		if err != nil {
			return FileDiff{}, false, err
		}
		if owned {
			return FileDiff{}, false, nil
		}
	}
	diff, changed, err := newFileDiff(entry.Path, local, entry.Data, false, false)
	if err != nil {
		return FileDiff{}, false, err
	}
	return diff, changed, nil
}

func diffDroppedOne(
	projectDir string,
	rendered map[string]renderedEntry,
	manifest *config.Manifest,
	entry config.LockFile,
	preserved []string,
) (FileDiff, bool, error) {
	if _, ok := rendered[entry.Path]; ok {
		return FileDiff{}, false, nil
	}
	ignored, err := filter.MatchAny(manifest.Excludes, entry.Path)
	if err != nil {
		return FileDiff{}, false, fmt.Errorf("diff: %w", err)
	}
	if ignored {
		return FileDiff{}, false, nil
	}
	local, err := filter.ReadFileWithinRoot(projectDir, entry.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileDiff{}, false, nil
		}
		return FileDiff{}, false, fmt.Errorf(
			"diff: reading project file %q: %w", entry.Path, err)
	}
	kept, err := isPreservedPath(entry.Path, manifest, preserved)
	if err != nil {
		return FileDiff{}, false, fmt.Errorf("diff: %w", err)
	}
	if kept {
		return FileDiff{}, false, nil
	}
	diff, _, err := newFileDiff(entry.Path, local, nil, false, true)
	if err != nil {
		return FileDiff{}, false, err
	}
	return diff, true, nil
}

type renderedEntry struct {
	Path string
	Data []byte
}

func walkRendered(stage string, manifest *config.Manifest) (map[string]renderedEntry, error) {
	out := map[string]renderedEntry{}
	err := filepath.WalkDir(stage, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return err
		}
		if rel == "." || d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if info, err := d.Info(); err != nil {
			return err
		} else if filter.IsSpecialFile(info) {
			return nil
		} else if !info.Mode().IsRegular() {
			return nil
		}
		slash := filter.ToSlash(rel)
		data, err := os.ReadFile(path) // #nosec G304, G122 -- WalkDir over this run's 0700 staging dir with symlinks skipped; staging is owner-only so no cross-user TOCTOU boundary
		if err != nil {
			return err
		}
		out[slash] = renderedEntry{Path: slash, Data: data}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func renderedPaths(rendered map[string]renderedEntry) []string {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func newFileDiff(path string, local, fresh []byte, isNew, isDeleted bool) (FileDiff, bool, error) {
	if string(local) == string(fresh) {
		return FileDiff{}, false, nil
	}
	diff := FileDiff{Path: path, IsNew: isNew, IsDeleted: isDeleted}
	var aLines, bLines []string
	if len(local) > 0 {
		aLines = splitDiffLines(string(local))
	}
	if len(fresh) > 0 {
		bLines = splitDiffLines(string(fresh))
	}
	matcher := difflib.NewMatcher(aLines, bLines)
	for _, group := range matcher.GetGroupedOpCodes(DiffContextLines) {
		for _, op := range group {
			switch op.Tag {
			case 'r':
				diff.Removed += op.I2 - op.I1
				diff.Added += op.J2 - op.J1
			case 'd':
				diff.Removed += op.I2 - op.I1
			case 'i':
				diff.Added += op.J2 - op.J1
			}
		}
	}
	var rendered strings.Builder
	if err := difflib.WriteUnifiedDiff(&rendered, difflib.UnifiedDiff{
		A:        aLines,
		FromFile: "a/" + path,
		B:        bLines,
		ToFile:   "b/" + path,
		Context:  DiffContextLines,
	}); err != nil {
		return FileDiff{}, false, fmt.Errorf("diff: rendering unified: %w", err)
	}
	diff.Unified = rendered.String()
	return diff, true, nil
}

func splitDiffLines(s string) []string {
	lines := difflib.SplitLines(s)
	if strings.HasSuffix(s, "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type WriteOptions struct {
	NoColor bool
}

func WriteDiff(w io.Writer, diffs []FileDiff, opts WriteOptions) error {
	_ = opts.NoColor
	sorted := make([]FileDiff, len(diffs))
	copy(sorted, diffs)
	slices.SortFunc(sorted, func(a, b FileDiff) int { return strings.Compare(a.Path, b.Path) })
	for _, diff := range sorted {
		if _, err := io.WriteString(w, diff.Unified); err != nil {
			return fmt.Errorf("diff: writing output: %w", err)
		}
	}
	return nil
}
