// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// wrapRootErr annotates root I/O failures except not-exist ones, which
// are returned raw: os.IsNotExist only unwraps one level of OS error
// types, so a fmt wrap would hide missing files from callers that probe
// existence (dirty guard, merge presence checks).
func wrapRootErr(op, name string, err error) error {
	if os.IsNotExist(err) {
		return err
	}
	return fmt.Errorf("%s %q: %w", op, name, err)
}

// rootName normalizes a root-relative path for os.Root access: forward
// slashes become OS separators so Windows and Unix callers share one form
// (the ToSlash discipline in reverse).
func rootName(name string) string {
	return filepath.FromSlash(ToSlash(name))
}

// ReadFileWithinRoot reads the root-relative file name through os.Root:
// lexical escapes and symlinks resolving outside rootDir are refused by
// the kernel-checked root instead of by string prefix checks.
func ReadFileWithinRoot(rootDir, name string) ([]byte, error) {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, fmt.Errorf("opening root %q: %w", rootDir, err)
	}
	defer root.Close()
	data, err := root.ReadFile(rootName(name))
	if err != nil {
		return nil, wrapRootErr("reading", name, err)
	}
	return data, nil
}

// WriteFileWithinRoot writes data to the root-relative file name through
// os.Root, creating parent directories first. Escaping names and symlinks
// resolving outside rootDir are refused.
func WriteFileWithinRoot(rootDir, name string, data []byte, perm fs.FileMode) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("opening root %q: %w", rootDir, err)
	}
	defer root.Close()
	rel := rootName(name)
	if dir := filepath.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return wrapRootErr("creating parent dir for", name, err)
		}
	}
	if err := root.WriteFile(rel, data, perm); err != nil {
		return wrapRootErr("writing", name, err)
	}
	return nil
}

// RemoveWithinRoot removes the root-relative name through os.Root, which
// refuses escapes and escaping symlinks.
func RemoveWithinRoot(rootDir, name string) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return fmt.Errorf("opening root %q: %w", rootDir, err)
	}
	defer root.Close()
	if err := root.Remove(rootName(name)); err != nil {
		return wrapRootErr("removing", name, err)
	}
	return nil
}

// StatWithinRoot stats the root-relative name through os.Root without
// following a final escaping symlink.
func StatWithinRoot(rootDir, name string) (fs.FileInfo, error) {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, fmt.Errorf("opening root %q: %w", rootDir, err)
	}
	defer root.Close()
	info, err := root.Lstat(rootName(name))
	if err != nil {
		return nil, wrapRootErr("statting", name, err)
	}
	return info, nil
}
