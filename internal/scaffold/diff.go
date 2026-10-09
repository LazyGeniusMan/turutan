// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

// DiffContextLines is the unified-diff context size (spec §5.2: Context 3).
const DiffContextLines = 3

// DiffOptions carries per-run knobs for the diff flow.
type DiffOptions struct {
	// Ref overrides the stored requestedRef for this run only; the stored
	// state is never modified.
	Ref string
}

// FileDiff is one drifted file: Unified holds the Context-3 unified diff
// between the project file (a/) and the freshly rendered template (b/).
// IsNew means the template added the file (or the project deleted it);
// IsDeleted means the template dropped a file the project still has.
type FileDiff struct {
	Path      string
	Unified   string
	Added     int
	Removed   int
	IsNew     bool
	IsDeleted bool
}

// ComputeDiff materializes the template at the target ref in a temp dir
// (never a checkout inside the project), renders it with the stored
// answers, and returns the per-file unified diffs against the project
// tree. It never mutates the project: state and lock are only read.
// Manifest ignore globs are skipped; files outside the template render
// (user files) are ignored.
func ComputeDiff(projectDir string, opts DiffOptions) ([]FileDiff, error) {
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	src, err := template.ParseSource(state.Template)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	if opts.Ref != "" {
		src.RequestedRef = opts.Ref
	}
	fetched, err := template.Fetch(src)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	stage, err := os.MkdirTemp("", "turutan-diff-*")
	if err != nil {
		return nil, fmt.Errorf("diff: creating staging dir: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := template.RenderDir(fetched.Dir, stage, state.Answers); err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	rendered, err := walkRendered(stage, manifest)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	var diffs []FileDiff
	for _, path := range renderedPaths(rendered) {
		entry := rendered[path]
		local, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(entry.Path)))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("diff: reading project file %q: %w", entry.Path, err)
			}
			diff, _ := newFileDiff(entry.Path, nil, entry.Data, true, false)
			diffs = append(diffs, diff)
			continue
		}
		if diff, changed := newFileDiff(entry.Path, local, entry.Data, false, false); changed {
			diffs = append(diffs, diff)
		}
	}
	for _, entry := range lock.Files {
		if _, ok := rendered[entry.Path]; ok {
			continue
		}
		ignored, err := filter.MatchAny(manifest.Ignore, entry.Path)
		if err != nil {
			return nil, fmt.Errorf("diff: %w", err)
		}
		if ignored {
			continue
		}
		local, err := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(entry.Path)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("diff: reading project file %q: %w", entry.Path, err)
		}
		diff, _ := newFileDiff(entry.Path, local, nil, false, true)
		diffs = append(diffs, diff)
	}
	return diffs, nil
}

// renderedEntry is one rendered template file with slash-relative path.
type renderedEntry struct {
	Path string
	Data []byte
}

// walkRendered lists every regular file in the staged render except
// manifest-ignored paths, keyed by slash-relative path.
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
		if !d.Type().IsRegular() {
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
		data, err := os.ReadFile(path)
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

// renderedPaths returns the sorted paths of a rendered listing.
func renderedPaths(rendered map[string]renderedEntry) []string {
	paths := make([]string, 0, len(rendered))
	for path := range rendered {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// newFileDiff builds the unified diff of one file: local is the project
// content (nil when absent), fresh the rendered template (nil when the
// template dropped the file). It returns false when the contents are
// identical. Headers use git-style a/ (project) and b/ (template) names
// with no timestamps so output is byte-stable for golden tests.
func newFileDiff(path string, local, fresh []byte, isNew, isDeleted bool) (FileDiff, bool) {
	if string(local) == string(fresh) {
		return FileDiff{}, false
	}
	diff := FileDiff{Path: path, IsNew: isNew, IsDeleted: isDeleted}
	// Empty content is zero lines: SplitLines("") yields one phantom
	// "\n" line, which would skew new/deleted-file hunks.
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
	_ = difflib.WriteUnifiedDiff(&rendered, difflib.UnifiedDiff{
		A:        aLines,
		FromFile: "a/" + path,
		B:        bLines,
		ToFile:   "b/" + path,
		Context:  DiffContextLines,
	})
	diff.Unified = rendered.String()
	return diff, true
}

// splitDiffLines splits s into difflib lines. SplitLines appends a
// spurious trailing "\n" element when s ends with a newline (and
// terminates a missing one), so the final element is dropped exactly when
// s ends with "\n"; otherwise every element is a real line. A missing
// trailing newline is therefore invisible to the diff (MVP tradeoff).
func splitDiffLines(s string) []string {
	lines := difflib.SplitLines(s)
	if strings.HasSuffix(s, "\n") && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// WriteDiff writes the plain (piped, byte-stable) rendering of diffs to w:
// each file's unified diff in path order. An empty set writes nothing.
func WriteDiff(w io.Writer, diffs []FileDiff) error {
	sorted := make([]FileDiff, len(diffs))
	copy(sorted, diffs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, diff := range sorted {
		if _, err := io.WriteString(w, diff.Unified); err != nil {
			return fmt.Errorf("diff: writing output: %w", err)
		}
	}
	return nil
}
