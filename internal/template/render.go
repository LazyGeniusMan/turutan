// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/LazyGeniusMan/turutan/internal/condition"
	"github.com/LazyGeniusMan/turutan/internal/filter"
)

const tmplSuffix = ".tmpl"

// MissingKeyError reports the data key absent when a render or predicate
// executed. It aliases condition.MissingKeyError so config can share the
// same retry-path type without an import cycle.
// ~keep
type MissingKeyError = condition.MissingKeyError

func parseMissingKey(err error) string {
	return condition.ParseMissingKey(err)
}

type Conditional struct {
	If   string
	Skip []string
}

var skippedDirNames = []string{".git"}

const skippedStateSubtree = ".config/turutan"

var skippedFileNames = []string{".turutan.yml", ".turutan.yaml"}

func isSkippedPath(slash string) bool {
	if slash == skippedStateSubtree ||
		strings.HasPrefix(slash, skippedStateSubtree+"/") {
		return true
	}
	for segment := range strings.SplitSeq(slash, "/") {
		if slices.Contains(skippedDirNames, segment) {
			return true
		}
		if slices.Contains(skippedFileNames, segment) {
			return true
		}
	}
	return false
}

func containsFunc(a, b any) bool {
	return condition.Contains(a, b)
}

func templateFuncMap() map[string]any {
	return condition.TemplateFuncMap()
}

// EvaluatePredicate evaluates one `if` predicate against view with the same
// funcs and missingkey=error option as EvaluateSkip. Empty predicates
// report true so callers ask rather than silently skip.
// ~keep
func EvaluatePredicate(ifExpr string, view map[string]any) (bool, error) {
	return condition.EvaluatePredicate(ifExpr, view)
}

// ParseConditionalBool reports the boolean meaning of a rendered predicate.
// ~keep
func ParseConditionalBool(s string) (bool, error) {
	return condition.ParseConditionalBool(s)
}

// EvaluateSkip evaluates conditional skip globs against data.
// ~keep
func EvaluateSkip(data map[string]any, conds []Conditional) (map[string]bool, error) {
	skipSet := map[string]bool{}
	for i, cond := range conds {
		if cond.If == "" {
			continue
		}
		tmpl, err := template.New(fmt.Sprintf("conditional[%d]", i)).
			Option("missingkey=error").
			Funcs(templateFuncMap()).
			Parse(cond.If)
		if err != nil {
			return nil, fmt.Errorf("evaluating conditional[%d]: parsing: %w", i, err)
		}
		var rendered strings.Builder
		if err := tmpl.Execute(&rendered, data); err != nil {
			wrapped := fmt.Errorf("evaluating conditional[%d]: %w", i, err)
			if key := parseMissingKey(err); key != "" {
				return nil, &MissingKeyError{Key: key, Err: wrapped}
			}
			return nil, wrapped
		}
		enabled, err := parseConditionalBool(strings.TrimSpace(rendered.String()))
		if err != nil {
			return nil, fmt.Errorf("evaluating conditional[%d]: %w", i, err)
		}
		if !enabled {
			continue
		}
		for _, glob := range cond.Skip {
			skipSet[glob] = true
		}
	}
	return skipSet, nil
}

func parseConditionalBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "", "no":
		return false, nil
	default:
		return false, fmt.Errorf("conditional result %q is not a boolean (want true/false)", s)
	}
}

func skipGlobs(skipSet map[string]bool) []string {
	globs := make([]string, 0, len(skipSet))
	for glob := range skipSet {
		globs = append(globs, glob)
	}
	slices.Sort(globs)
	return globs
}

func matchesSkip(globs []string, slash string) (bool, error) {
	if len(globs) == 0 {
		return false, nil
	}
	stripped := strings.TrimSuffix(slash, tmplSuffix)
	matched, err := filter.MatchAny(globs, slash)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	if stripped != slash {
		matched, err := filter.MatchAny(globs, stripped)
		if err != nil {
			return false, err
		}
		return matched, nil
	}
	return false, nil
}

// RenderFS renders a filesystem template with data.
// ~keep
func RenderFS(srcFS fs.FS, dstDir string, data map[string]any) error {
	return RenderFSWithConditionals(srcFS, dstDir, data, nil, nil)
}

// RenderFSWithConditionals renders srcFS to dstDir, applying explicit
// conditionals plus the layered profile include decision. A nil include
// means every non-metadata path is rendered.
// ~keep
func RenderFSWithConditionals(srcFS fs.FS, dstDir string, data map[string]any, conds []Conditional, include func(string) (bool, error)) error {
	skipSet, err := EvaluateSkip(data, conds)
	if err != nil {
		return err
	}
	globs := skipGlobs(skipSet)
	return fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		slash := filter.ToSlash(path)
		if isSkippedPath(slash) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		skip, err := matchesSkip(globs, slash)
		if err != nil {
			return fmt.Errorf("rendering template: matching conditional skip: %w", err)
		}
		if skip {
			return nil
		}
		if include != nil {
			ok, err := include(slash)
			if err != nil {
				return fmt.Errorf("rendering template: matching profile includes: %w", err)
			}
			if !ok {
				return nil
			}
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("rendering template: symlink %q unsupported via FS renderer (use RenderDir)", path)
		}
		dst, err := filter.SafeJoin(dstDir, filepath.FromSlash(slash))
		if err != nil {
			return fmt.Errorf("rendering template: %w", err)
		}
		if d.IsDir() {
			return os.MkdirAll(dst, 0o750) // #nosec G122 -- SafeJoin-pinned render into this run's fresh staging dir; no attacker-controlled path components on the dst side
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return renderFile(srcFS, slash, dst, data)
	})
}

// RenderDir renders a directory template with data.
// ~keep
func RenderDir(srcDir, dstDir string, data map[string]any) error {
	return RenderDirWithConditionals(srcDir, dstDir, data, nil, nil)
}

// RenderDirWithConditionals renders srcDir to dstDir, applying explicit
// conditionals plus the layered profile include decision. A nil include
// means every non-metadata path is rendered. Directories are never pruned:
// files are evaluated individually so a later profile can re-include a
// child of an excluded subtree.
// ~keep
func RenderDirWithConditionals(srcDir, dstDir string, data map[string]any, conds []Conditional, include func(string) (bool, error)) error {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("rendering template %q: %w", srcDir, err)
	}
	skipSet, err := EvaluateSkip(data, conds)
	if err != nil {
		return err
	}
	globs := skipGlobs(skipSet)
	return filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		return renderDirEntryWithConditionals(abs, dstDir, path, d, err, data, globs, include)
	})
}

func renderDirEntry(
	abs, dstDir, path string,
	d fs.DirEntry,
	walkErr error,
	data map[string]any,
) error {
	return renderDirEntryWithConditionals(abs, dstDir, path, d, walkErr, data, nil, nil)
}

func renderDirEntryWithConditionals(
	abs, dstDir, path string,
	d fs.DirEntry,
	walkErr error,
	data map[string]any,
	globs []string,
	include func(string) (bool, error),
) error {
	if walkErr != nil {
		return walkErr
	}
	rel, err := filepath.Rel(abs, path)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	slash := filter.ToSlash(rel)
	if isSkippedPath(slash) {
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	skip, err := matchesSkip(globs, slash)
	if err != nil {
		return fmt.Errorf("rendering template: matching conditional skip: %w", err)
	}
	if skip {
		return nil
	}
	if include != nil {
		ok, err := include(slash)
		if err != nil {
			return fmt.Errorf("rendering template: matching profile includes: %w", err)
		}
		if !ok {
			return nil
		}
	}
	dst, err := filter.SafeJoin(dstDir, filepath.FromSlash(slash))
	if err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return renderSymlink(abs, path, rel, slash, dst, data)
	case info.IsDir():
		return os.MkdirAll(dst, 0o750)
	case info.Mode().IsRegular():
		return renderFile(os.DirFS(abs), slash, dst, data)
	default:
		return nil
	}
}

func renderSymlink(
	abs, path, rel, slash, dst string,
	data map[string]any,
) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf(
			"rendering template: resolving symlink %q: %w", rel, err)
	}
	if err := filter.EnsureWithinRoot(abs, resolved); err != nil {
		return fmt.Errorf("rendering template: %w", err)
	}
	target, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !target.Mode().IsRegular() {
		return fmt.Errorf(
			"rendering template: symlink %q target is not a "+
				"regular file",
			rel)
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf(
			"rendering template: reading symlink target of %q: %w",
			rel, err)
	}
	return renderFileBytes(
		content, stripTmpl(dst), slash, data, target.Mode().Perm())
}

func renderFile(srcFS fs.FS, slash, dst string, data map[string]any) error {
	content, err := fs.ReadFile(srcFS, slash)
	if err != nil {
		return fmt.Errorf("rendering template: reading %q: %w", slash, err)
	}
	return renderFileBytes(content, stripTmpl(dst), slash, data, 0o644)
}

func renderFileBytes(content []byte, dst, slash string, data map[string]any, perm fs.FileMode) error {
	if strings.HasSuffix(slash, tmplSuffix) {
		tmpl, err := template.New(slash).Option("missingkey=error").Funcs(templateFuncMap()).Parse(string(content))
		if err != nil {
			return fmt.Errorf("rendering template: parsing %q: %w", slash, err)
		}
		var rendered strings.Builder
		if err := tmpl.Execute(&rendered, data); err != nil {
			wrapped := fmt.Errorf(
				"rendering template: executing %q: %w", slash, err)
			if key := parseMissingKey(err); key != "" {
				return &MissingKeyError{Key: key, Err: wrapped}
			}
			return wrapped
		}
		content = []byte(rendered.String())
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if perm == 0 {
		perm = 0o644
	}
	return os.WriteFile(dst, content, perm) // #nosec G703 -- dst is filter.SafeJoin-pinned under dstDir; analyzer does not model the pin
}

func stripTmpl(dst string) string {
	return strings.TrimSuffix(dst, tmplSuffix)
}
