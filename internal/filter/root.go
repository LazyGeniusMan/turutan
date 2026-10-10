// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func wrapRootErr(op, name string, err error) error {
	if os.IsNotExist(err) {
		return err
	}
	return fmt.Errorf("%s %q: %w", op, name, err)
}

func rootName(name string) string {
	return filepath.FromSlash(ToSlash(name))
}

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
