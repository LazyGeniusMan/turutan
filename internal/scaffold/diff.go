// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"io/fs"
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

type DiffOptions struct {
	Ref     string
	Engine  string
	NoColor bool
	Verbose bool
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
	state, lock, src, err := loadDiffInputs(projectDir, opts)
	if err != nil {
		return nil, err
	}
	rendered, manifest, err := renderDiffTemplate(
		ctx, src, state, opts)
	if err != nil {
		return nil, err
	}
	diffs, err := collectDiffs(projectDir, rendered, lock, manifest)
	if err != nil {
		return nil, err
	}
	vlogf(opts.Stderr, opts.Verbose,
		"diff: compared %d rendered file(s), %d drifted",
		len(rendered), len(diffs))
	return diffs, nil
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
	src, err := template.ParseSource(state.Template)
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
	defer os.RemoveAll(stage)
	if err := template.RenderDir(fetched.Dir, stage, state.Answers); err != nil {
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
) ([]FileDiff, error) {
	var diffs []FileDiff
	for _, path := range renderedPaths(rendered) {
		diff, changed, err := diffRenderedOne(projectDir, rendered[path])
		if err != nil {
			return nil, err
		}
		if changed {
			diffs = append(diffs, diff)
		}
	}
	for _, entry := range lock.Files {
		diff, changed, err := diffDroppedOne(projectDir, rendered, manifest, entry)
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
) (FileDiff, bool, error) {
	if _, ok := rendered[entry.Path]; ok {
		return FileDiff{}, false, nil
	}
	ignored, err := filter.MatchAny(manifest.Ignore, entry.Path)
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
		ignored, err := filter.MatchAny(manifest.Ignore, slash)
		if err != nil {
			return fmt.Errorf("matching ignore globs: %w", err)
		}
		if ignored {
			return nil
		}
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
