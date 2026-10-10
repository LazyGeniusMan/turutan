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

	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/Masterminds/sprig/v3"
)

const tmplSuffix = ".tmpl"

type MissingKeyError struct {
	Key string
	Err error
}

func (e *MissingKeyError) Error() string {
	return e.Err.Error()
}

func (e *MissingKeyError) Unwrap() error { return e.Err }

func parseMissingKey(err error) string {
	const marker = `no entry for key "`
	msg := err.Error()
	_, after, ok := strings.Cut(msg, marker)
	if !ok {
		return ""
	}
	key, _, ok := strings.Cut(after, `"`)
	if !ok {
		return ""
	}
	return key
}

var skippedDirNames = []string{".turutan", ".git"}

func isSkippedPath(slash string) bool {
	for segment := range strings.SplitSeq(slash, "/") {
		if slices.Contains(skippedDirNames, segment) {
			return true
		}
	}
	return false
}

func RenderFS(srcFS fs.FS, dstDir string, answers map[string]any) error {
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
		return renderFile(srcFS, slash, dst, answers)
	})
}

func RenderDir(srcDir, dstDir string, answers map[string]any) error {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("rendering template %q: %w", srcDir, err)
	}
	return filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		return renderDirEntry(abs, dstDir, path, d, err, answers)
	})
}

func renderDirEntry(
	abs, dstDir, path string,
	d fs.DirEntry,
	walkErr error,
	answers map[string]any,
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
		return renderSymlink(abs, path, rel, slash, dst, answers)
	case info.IsDir():
		return os.MkdirAll(dst, 0o750)
	case info.Mode().IsRegular():
		return renderFile(os.DirFS(abs), slash, dst, answers)
	default:
		return nil
	}
}

func renderSymlink(
	abs, path, rel, slash, dst string,
	answers map[string]any,
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
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf(
			"rendering template: reading symlink target of %q: %w",
			rel, err)
	}
	return renderFileBytes(
		data, stripTmpl(dst), slash, answers, target.Mode().Perm())
}

func renderFile(srcFS fs.FS, slash, dst string, answers map[string]any) error {
	data, err := fs.ReadFile(srcFS, slash)
	if err != nil {
		return fmt.Errorf("rendering template: reading %q: %w", slash, err)
	}
	return renderFileBytes(data, stripTmpl(dst), slash, answers, 0o644)
}

func templateFuncMap() map[string]any {
	funcs := sprig.TxtFuncMap()
	delete(funcs, "env")
	delete(funcs, "expandenv")
	return funcs
}

func renderFileBytes(data []byte, dst, slash string, answers map[string]any, perm fs.FileMode) error {
	if strings.HasSuffix(slash, tmplSuffix) {
		tmpl, err := template.New(slash).Option("missingkey=error").Funcs(templateFuncMap()).Parse(string(data))
		if err != nil {
			return fmt.Errorf("rendering template: parsing %q: %w", slash, err)
		}
		var rendered strings.Builder
		if err := tmpl.Execute(&rendered, answers); err != nil {
			wrapped := fmt.Errorf(
				"rendering template: executing %q: %w", slash, err)
			if key := parseMissingKey(err); key != "" {
				return &MissingKeyError{Key: key, Err: wrapped}
			}
			return wrapped
		}
		data = []byte(rendered.String())
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if perm == 0 {
		perm = 0o644
	}
	return os.WriteFile(dst, data, perm) // #nosec G703 -- dst is filter.SafeJoin-pinned under dstDir; analyzer does not model the pin
}

func stripTmpl(dst string) string {
	return strings.TrimSuffix(dst, tmplSuffix)
}
