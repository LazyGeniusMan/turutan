// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

// baseStoreSubdir is the project-relative directory holding pristine base
// copies keyed by resolved identity: .turutan/templates/<sha>/ (spec §7,
// v2 3-way). Each copy is the rendered template at that identity, before
// any local edit, so update can merge base↔local↔new per file.
const baseStoreSubdir = ".turutan/templates"

// BasePath returns the base-copy directory for sha, refusing empty or
// escaping identities so a corrupt lock cannot write outside the store.
func BasePath(projectDir, sha string) (string, error) {
	if sha == "" {
		return "", fmt.Errorf("base store: empty identity")
	}
	rel, err := filter.SecureJoin(baseStoreSubdir, filter.ToSlash(sha))
	if err != nil {
		return "", fmt.Errorf("base store: %w", err)
	}
	return filepath.Join(projectDir, filepath.FromSlash(rel)), nil
}

// HasBase reports whether a pristine base copy exists for sha.
func HasBase(projectDir, sha string) bool {
	dir, err := BasePath(projectDir, sha)
	if err != nil {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// StoreBase copies the rendered template tree at srcDir into the base
// store under sha. .turutan and .git trees never enter the store;
// symlinks and special files (fifo/socket/device) are skipped so the
// base holds regular files only. Storing is fail-closed: an error aborts
// the caller (bootstrap/update) rather than leaving a partial base that
// would silently downgrade the next update to the v1 overlay.
func StoreBase(projectDir, sha, srcDir string) error {
	dst, err := BasePath(projectDir, sha)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("base store: resolving source: %w", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("base store: creating %q: %w", dst, err)
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
		for _, skipped := range []string{".turutan", ".git"} {
			if slash == skipped || len(slash) > len(skipped) && slash[:len(skipped)+1] == skipped+"/" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		target, err := filter.SafeJoin(dst, filepath.FromSlash(slash))
		if err != nil {
			return fmt.Errorf("base store: %w", err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			return nil
		case info.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		default:
			return nil
		}
	})
}

// ReadBaseFile reads one root-relative file from the base copy for sha
// through os.Root, refusing escapes and escaping symlinks.
func ReadBaseFile(projectDir, sha, name string) ([]byte, error) {
	dir, err := BasePath(projectDir, sha)
	if err != nil {
		return nil, err
	}
	return filter.ReadFileWithinRoot(dir, name)
}
