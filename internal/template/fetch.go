// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/git"
)

// fullSHA matches a 40-hex full commit SHA.
var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// Fetched is a materialized template tree ready to render. Dir is the
// subpath-scoped content root: a temp clone for git kinds (removed by
// Cleanup) or the source subdir itself for filesystem sources (Cleanup
// nil). ResolvedCommit is the git commit SHA for git kinds and "" for
// filesystem sources, where the caller derives identity from content.
type Fetched struct {
	Dir            string
	Cleanup        func()
	Source         *Source
	ResolvedCommit string
}

// Fetch materializes src: remote-git clones depth=1 and checks out the
// resolved ref; local-git resolves offline then clones from disk;
// filesystem validates OpenRoot containment plus safeJoin and returns the
// subdir directly. The default template additionally consults the
// SHA-keyed cache (~/.cache/turutan/default/<sha>/): a full-SHA request
// with a warm cache works offline with no network.
func Fetch(src *Source) (*Fetched, error) {
	switch src.Kind {
	case KindRemoteGit:
		return fetchRemote(src)
	case KindLocalGit:
		return fetchLocalGit(src)
	case KindFilesystem:
		return fetchFilesystem(src)
	default:
		return nil, fmt.Errorf("fetching source %q: unknown kind %q", src.Raw, src.Kind)
	}
}

// fetchRemote resolves the requested ref over the network, reuses the
// default-template cache on hit, and otherwise shallow-clones.
func fetchRemote(src *Source) (*Fetched, error) {
	if IsDefaultSource(src) && fullSHA.MatchString(src.RequestedRef) {
		if dir, ok := defaultCacheDir(src.RequestedRef); ok {
			return &Fetched{Dir: dir, Source: src, ResolvedCommit: src.RequestedRef}, nil
		}
	}
	sha, err := git.ResolveRemoteRef(src.Repo, src.RequestedRef)
	if err != nil {
		if IsDefaultSource(src) {
			return nil, fmt.Errorf("fetching default template: %w (default template unavailable offline: no cached %q; connect once with network)",
				err, src.RequestedRef)
		}
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	if IsDefaultSource(src) {
		if dir, ok := defaultCacheDir(sha); ok {
			return &Fetched{Dir: dir, Source: src, ResolvedCommit: sha}, nil
		}
	}
	tmp, err := os.MkdirTemp("", "turutan-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: creating temp dir: %w", src.Raw, err)
	}
	cleanup := func() { os.RemoveAll(tmp) }
	if _, err := git.Clone(src.Repo, "", tmp, src.Depth); err != nil {
		cleanup()
		if IsDefaultSource(src) {
			return nil, fmt.Errorf("fetching default template: %w (default template unavailable offline: no cached %q; connect once with network)",
				err, src.RequestedRef)
		}
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	if err := git.Checkout(tmp, sha); err != nil {
		cleanup()
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	dir, err := subdir(tmp, src)
	if err != nil {
		cleanup()
		return nil, err
	}
	if IsDefaultSource(src) {
		populateDefaultCache(tmp, sha)
	}
	return &Fetched{Dir: dir, Cleanup: cleanup, Source: src, ResolvedCommit: sha}, nil
}

// fetchLocalGit resolves the ref offline inside the source repository,
// then clones from disk into a temp dir so rendering never mutates the
// source and ref checkouts stay hermetic.
func fetchLocalGit(src *Source) (*Fetched, error) {
	sha, err := git.ResolveLocal(src.Repo, src.RequestedRef)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	tmp, err := os.MkdirTemp("", "turutan-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: creating temp dir: %w", src.Raw, err)
	}
	cleanup := func() { os.RemoveAll(tmp) }
	if _, err := git.Clone(src.Repo, "", tmp, src.Depth); err != nil {
		cleanup()
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	if err := git.Checkout(tmp, sha); err != nil {
		cleanup()
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	dir, err := subdir(tmp, src)
	if err != nil {
		cleanup()
		return nil, err
	}
	return &Fetched{Dir: dir, Cleanup: cleanup, Source: src, ResolvedCommit: sha}, nil
}

// fetchFilesystem validates containment (os.OpenRoot plus safeJoin) and
// returns the subdir directly; symlinks resolving outside the source root
// are refused. ?ref= is meaningless without commits and is rejected.
func fetchFilesystem(src *Source) (*Fetched, error) {
	if src.RequestedRef != "" {
		return nil, fmt.Errorf("fetching source %q: ref %q is not supported for filesystem sources", src.Raw, src.RequestedRef)
	}
	abs, err := filepath.Abs(src.Repo)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: opening root: %w", src.Raw, err)
	}
	defer root.Close()
	rel := src.Subpath
	if rel == "" {
		rel = "."
	}
	info, err := root.Stat(rel)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: subpath %q: %w", src.Raw, src.Subpath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fetching source %q: subpath %q is not a directory", src.Raw, src.Subpath)
	}
	dir, err := filter.SafeJoin(abs, src.Subpath)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: resolving subpath: %w", src.Raw, err)
	}
	if err := filter.EnsureWithinRoot(abs, resolved); err != nil {
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	return &Fetched{Dir: dir, Source: src}, nil
}

// subdir scopes a fetched tree to the source subpath.
func subdir(root string, src *Source) (string, error) {
	if src.Subpath == "" {
		return root, nil
	}
	dir, err := filter.SafeJoin(root, filepath.FromSlash(src.Subpath))
	if err != nil {
		return "", fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("fetching source %q: subpath %q: %w", src.Raw, src.Subpath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("fetching source %q: subpath %q is not a directory", src.Raw, src.Subpath)
	}
	return dir, nil
}

// defaultCacheDir returns the cached default-template tree for sha.
func defaultCacheDir(sha string) (string, bool) {
	cache := config.ResolveCacheDir()
	if cache == "" {
		return "", false
	}
	dir := filepath.Join(cache, "default", strings.ToLower(sha))
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

// populateDefaultCache copies a freshly fetched default tree (minus .git)
// into the SHA-keyed cache on a best-effort basis: cache failures must
// never fail a successful fetch.
func populateDefaultCache(srcDir, sha string) {
	cache := config.ResolveCacheDir()
	if cache == "" {
		return
	}
	dst := filepath.Join(cache, "default", strings.ToLower(sha))
	if _, err := os.Stat(dst); err == nil {
		return
	}
	_ = copyDir(srcDir, dst)
}

// copyDir copies the file tree at src to dst, skipping .git metadata.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target, err := filter.SafeJoin(dst, rel)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}
