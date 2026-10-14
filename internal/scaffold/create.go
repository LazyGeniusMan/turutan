// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

//go:embed all:createdata
var createFS embed.FS

const createRoot = "createdata"

const createHookScript = "hooks/post-setup.sh"

// CreateOptions tunes the Create template-authoring scaffold.
// ~keep
type CreateOptions struct {
	Force   bool
	Verbose bool
	Stdout  io.Writer
	Stderr  io.Writer
}

// Create scaffolds a new template-authoring project at dir.
// ~keep
func Create(dir string, opts CreateOptions) error {
	stdout, stderr, _ := resolveStreams(opts.Stdout, opts.Stderr, nil)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("create: resolving target %q: %w", dir, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("create: reading target %q: %w", dir, err)
		}
		if err := os.MkdirAll(abs, 0o750); err != nil {
			return fmt.Errorf("create: creating target %q: %w", dir, err)
		}
	} else if len(entries) > 0 && !opts.Force {
		return fmt.Errorf("create: target %q is not empty (use --force to overwrite)", dir)
	}
	count, err := writeCreateFiles(abs, opts.Verbose, stderr)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "created template project in %s (%d files)\n", abs, count); err != nil {
		return err
	}
	vlogf(stderr, opts.Verbose, "create: wrote %d file(s) to %s", count, abs)
	return nil
}

func writeCreateFiles(abs string, verbose bool, stderr io.Writer) (int, error) {
	count := 0
	err := fs.WalkDir(createFS, createRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, ok := strings.CutPrefix(filter.ToSlash(path), createRoot+"/")
		if !ok {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := createFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("create: reading embedded %q: %w", rel, err)
		}
		perm := 0o644
		if rel == createHookScript {
			perm = 0o755
		}
		if err := filter.WriteFileWithinRoot(abs, rel, data, fs.FileMode(perm)); err != nil {
			return fmt.Errorf("create: writing %q: %w", rel, err)
		}
		vlogf(stderr, verbose, "create: wrote %s", rel)
		count++
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
