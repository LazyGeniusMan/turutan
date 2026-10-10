// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

const baseStoreSubdir = ".turutan/templates"

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

func HasBase(projectDir, sha string) bool {
	dir, err := BasePath(projectDir, sha)
	if err != nil {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func StoreBase(projectDir, sha, srcDir string) error {
	dst, err := BasePath(projectDir, sha)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("base store: resolving source: %w", err)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return fmt.Errorf("base store: creating %q: %w", dst, err)
	}
	return filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		return storeBaseEntry(abs, dst, path, d, err)
	})
}

func storeBaseEntry(
	abs, dst, path string,
	d fs.DirEntry,
	walkErr error,
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
	if skip, err := skipBaseMeta(slash, d); err != nil || skip {
		return err
	}
	target, err := filter.SafeJoin(dst, filepath.FromSlash(slash))
	if err != nil {
		return fmt.Errorf("base store: %w", err)
	}
	return writeBaseEntry(path, target)
}

func skipBaseMeta(slash string, d fs.DirEntry) (bool, error) {
	for _, skipped := range []string{".turutan", ".git"} {
		if slash == skipped ||
			len(slash) > len(skipped) &&
				slash[:len(skipped)+1] == skipped+"/" {
			if d.IsDir() {
				return true, filepath.SkipDir
			}
			return true, nil
		}
	}
	return false, nil
}

func writeBaseEntry(path, target string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return nil
	case info.IsDir():
		return os.MkdirAll(target, 0o750)
	case info.Mode().IsRegular():
		data, err := os.ReadFile(path) // #nosec G304 -- copies file found by WalkDir of validated source tree, not caller-controlled inclusion
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) // #nosec G703 -- target is SafeJoin-pinned under dst with symlinks skipped; no attacker-controlled path components on the dst side
	default:
		return nil
	}
}

func ReadBaseFile(projectDir, sha, name string) ([]byte, error) {
	dir, err := BasePath(projectDir, sha)
	if err != nil {
		return nil, err
	}
	return filter.ReadFileWithinRoot(dir, name)
}
