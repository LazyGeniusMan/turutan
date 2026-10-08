// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/Masterminds/sprig/v3"
)

// tmplSuffix marks files rendered as text/template; the suffix is stripped
// from the output name (go.mod.tmpl renders go.mod).
const tmplSuffix = ".tmpl"

// skippedTopDirs are never rendered: template metadata and VCS state.
var skippedTopDirs = []string{".turutan", ".git"}

// RenderFS renders the template tree in srcFS into dstDir with answers as
// the template data. Files ending in .tmpl execute as text/template with
// sprig functions (missing keys fail); every other regular file copies
// verbatim. The .turutan and .git trees are skipped. Symlinks cannot be
// resolved through a generic fs.FS and are rejected; use RenderDir for
// on-disk trees with symlinks.
func RenderFS(srcFS fs.FS, dstDir string, answers map[string]any) error {
	return fs.WalkDir(srcFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		slash := filter.ToSlash(path)
		for _, skipped := range skippedTopDirs {
			if slash == skipped || strings.HasPrefix(slash, skipped+"/") {
				if d.IsDir() {
					return fs.SkipDir
				}
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
			return os.MkdirAll(dst, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return renderFile(srcFS, slash, dst, answers)
	})
}

// RenderDir renders the on-disk template tree at srcDir into dstDir. It
// behaves like RenderFS except symlinks resolving inside srcDir are
// dereferenced (file content is copied) and symlinks escaping srcDir, as
// well as special files, are refused or skipped.
func RenderDir(srcDir, dstDir string, answers map[string]any) error {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("rendering template %q: %w", srcDir, err)
	}
	return filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filter.ToSlash(rel)
		for _, skipped := range skippedTopDirs {
			if slash == skipped || strings.HasPrefix(slash, skipped+"/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
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
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("rendering template: resolving symlink %q: %w", rel, err)
			}
			if err := filter.EnsureWithinRoot(abs, resolved); err != nil {
				return fmt.Errorf("rendering template: %w", err)
			}
			target, err := os.Stat(resolved)
			if err != nil {
				return err
			}
			if !target.Mode().IsRegular() {
				return fmt.Errorf("rendering template: symlink %q target is not a regular file", rel)
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return fmt.Errorf("rendering template: reading symlink target of %q: %w", rel, err)
			}
			return renderFileBytes(data, stripTmpl(dst), slash, answers, target.Mode().Perm())
		case info.IsDir():
			return os.MkdirAll(dst, 0o755)
		case info.Mode().IsRegular():
			return renderFile(os.DirFS(abs), slash, dst, answers)
		default:
			return nil
		}
	})
}

// renderFile renders or copies one srcFS entry to dst.
func renderFile(srcFS fs.FS, slash, dst string, answers map[string]any) error {
	data, err := fs.ReadFile(srcFS, slash)
	if err != nil {
		return fmt.Errorf("rendering template: reading %q: %w", slash, err)
	}
	return renderFileBytes(data, stripTmpl(dst), slash, answers, 0o644)
}

// renderFileBytes writes data to dst, executing it as a template when the
// source name carries the .tmpl suffix.
func renderFileBytes(data []byte, dst, slash string, answers map[string]any, perm fs.FileMode) error {
	if strings.HasSuffix(slash, tmplSuffix) {
		tmpl, err := template.New(slash).Option("missingkey=error").Funcs(sprig.TxtFuncMap()).Parse(string(data))
		if err != nil {
			return fmt.Errorf("rendering template: parsing %q: %w", slash, err)
		}
		var rendered strings.Builder
		if err := tmpl.Execute(&rendered, answers); err != nil {
			return fmt.Errorf("rendering template: executing %q: %w", slash, err)
		}
		data = []byte(rendered.String())
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Preserve the executable bit from template sources; rendered output
	// is otherwise a regular non-secret file.
	if perm == 0 {
		perm = 0o644
	}
	return os.WriteFile(dst, data, perm)
}

// stripTmpl removes the .tmpl suffix from an output path when present.
func stripTmpl(dst string) string {
	return strings.TrimSuffix(dst, tmplSuffix)
}
