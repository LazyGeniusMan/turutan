// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LazyGeniusMan/turutan/internal/filter"
)

// Bundle media and artifact types label template bundles in OCI registries.
// ~keep
const (
	BundleMediaType    = "application/vnd.turutan.template.v1+tar+gzip"
	BundleArtifactType = "application/vnd.turutan.template.v1"
)

const (
	maxBundleBytes = 1 << 30
	maxBundleFiles = 100000
)

type bundleEntry struct {
	rel   string
	mode  fs.FileMode
	size  int64
	full  string
	isDir bool
}

func collectBundleEntries(srcDir string) ([]bundleEntry, error) {
	var entries []bundleEntry
	err := filepath.WalkDir(srcDir, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, full)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || (!d.Type().IsRegular() && !d.IsDir()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, bundleEntry{
			rel:   filepath.ToSlash(rel),
			mode:  info.Mode().Perm(),
			size:  info.Size(),
			full:  full,
			isDir: d.IsDir(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	return entries, nil
}

// WriteBundle archives srcDir deterministically into w and returns its digest.
// ~keep
func WriteBundle(srcDir string, w io.Writer) (string, error) {
	entries, err := collectBundleEntries(srcDir)
	if err != nil {
		return "", fmt.Errorf("collecting bundle entries of %q: %w", srcDir, err)
	}
	hash := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(w, hash))
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		header := &tar.Header{
			Name:       entry.rel,
			Mode:       int64(entry.mode),
			ModTime:    time.Time{},
			AccessTime: time.Time{},
			ChangeTime: time.Time{},
			Format:     tar.FormatPAX,
		}
		if entry.isDir {
			header.Typeflag = tar.TypeDir
			header.Name += "/"
		} else {
			header.Typeflag = tar.TypeReg
			header.Size = entry.size
		}
		if err := tw.WriteHeader(header); err != nil {
			return "", fmt.Errorf("writing bundle entry %q: %w", entry.rel, err)
		}
		if entry.isDir {
			continue
		}
		data, err := os.ReadFile(entry.full) // #nosec G304 -- entry.full comes from walking srcDir with symlinks skipped
		if err != nil {
			return "", fmt.Errorf("reading bundle entry %q: %w", entry.rel, err)
		}
		if _, err := tw.Write(data); err != nil {
			return "", fmt.Errorf("writing bundle entry %q: %w", entry.rel, err)
		}
	}
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("closing bundle archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("closing bundle compression: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// BundleBytes archives srcDir into memory and returns the bytes with its digest.
// ~keep
func BundleBytes(srcDir string) ([]byte, string, error) {
	var buf bytes.Buffer
	digest, err := WriteBundle(srcDir, &buf)
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), digest, nil
}

// DigestDir returns the deterministic bundle digest of srcDir without writing it.
// ~keep
func DigestDir(srcDir string) (string, error) {
	digest, err := WriteBundle(srcDir, io.Discard)
	if err != nil {
		return "", err
	}
	return digest, nil
}

// ExtractBundle unpacks a bundle archive into destDir, rejecting escapes and bombs.
// ~keep
func ExtractBundle(r io.Reader, destDir string) error {
	abs, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolving destination %q: %w", destDir, err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return fmt.Errorf("creating destination %q: %w", destDir, err)
	}
	gz, err := gzip.NewReader(io.LimitReader(r, maxBundleBytes+1))
	if err != nil {
		return fmt.Errorf("reading bundle: not a gzip archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var total int64
	var files int
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading bundle entry: %w", err)
		}
		files++
		if files > maxBundleFiles {
			return fmt.Errorf("extracting bundle: too many files (zip bomb guard)")
		}
		name := path.Clean(strings.TrimPrefix(header.Name, "./"))
		if name == "." {
			continue
		}
		target, err := filter.SafeJoin(abs, filepath.FromSlash(name))
		if err != nil {
			return fmt.Errorf("extracting bundle entry %q: %w", header.Name, err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("creating bundle dir %q: %w", name, err)
			}
		case tar.TypeReg:
			total += header.Size
			if total > maxBundleBytes {
				return fmt.Errorf("extracting bundle: too large (zip bomb guard)")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("creating parent of %q: %w", name, err)
			}
			data, err := io.ReadAll(io.LimitReader(tr, maxBundleBytes-total+1))
			if err != nil {
				return fmt.Errorf("reading bundle entry %q: %w", name, err)
			}
			if err := os.WriteFile(target, data, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("writing bundle entry %q: %w", name, err)
			}
		default:
			return fmt.Errorf("extracting bundle entry %q: unsupported type %q", name, rune(header.Typeflag))
		}
	}
}

// CopyTree replicates srcDir into destDir, skipping .git and symlinks.
// ~keep
func CopyTree(srcDir, destDir string) error {
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("resolving source %q: %w", srcDir, err)
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return fmt.Errorf("creating destination %q: %w", destDir, err)
	}
	return filepath.WalkDir(absSrc, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absSrc, full)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target, err := filter.SafeJoin(destDir, rel)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o750) // #nosec G122 -- target is SafeJoin-pinned to destDir from WalkDir-relative rel with symlinks skipped
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(full) // #nosec G304, G122 -- full comes from walking absSrc with symlinks skipped
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) // #nosec G703, G122 -- target is SafeJoin-pinned to destDir from WalkDir-relative rel
	})
}

// CopyTreeFiltered replicates srcDir minus release metadata, for clean fetches.
// ~keep
func CopyTreeFiltered(srcDir, destDir string) error {
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("resolving source %q: %w", srcDir, err)
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return fmt.Errorf("creating destination %q: %w", destDir, err)
	}
	return filepath.WalkDir(absSrc, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absSrc, full)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if slash == ".git" || strings.HasPrefix(slash, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if slash == "manifest" || strings.HasPrefix(slash, "manifest/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if slash == "releases" || strings.HasPrefix(slash, "releases/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target, err := filter.SafeJoin(destDir, rel) // #nosec G703 -- rel is WalkDir-relative to absSrc and SafeJoin-pinned to destDir
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o750) // #nosec G122 -- target is SafeJoin-pinned to destDir from WalkDir-relative rel with symlinks skipped
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(full) // #nosec G304,G122 -- full comes from walking absSrc with symlinks skipped; SafeJoin-pinned write below
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) // #nosec G703, G122 -- target is SafeJoin-pinned to destDir from WalkDir-relative rel
	})
}
