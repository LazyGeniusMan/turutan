// SPDX-License-Identifier: Apache-2.0

package template

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/git"
)

var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type Fetched struct {
	Dir            string
	Cleanup        func()
	Source         *Source
	ResolvedCommit string
}

func Fetch(ctx context.Context, src *Source) (*Fetched, error) {
	switch src.Kind {
	case KindRemoteGit:
		return fetchRemote(ctx, src)
	case KindLocalGit:
		return fetchLocalGit(ctx, src)
	case KindFilesystem:
		return fetchFilesystem(src)
	default:
		return nil, fmt.Errorf("fetching source %q: unknown kind %q", src.Raw, src.Kind)
	}
}

func fetchRemote(ctx context.Context, src *Source) (*Fetched, error) {
	if dir, ok := cachedDefault(src); ok {
		return dir, nil
	}
	sha, err := git.ResolveRemoteRef(ctx, src.Repo, src.RequestedRef)
	if err != nil {
		return remoteResolveFallback(src, err)
	}
	if IsDefaultSource(src) {
		if dir, ok := defaultCacheDir(sha); ok {
			return &Fetched{
				Dir: dir, Source: src, ResolvedCommit: sha,
			}, nil
		}
	}
	return cloneRemote(ctx, src, sha)
}

func remoteResolveFallback(src *Source, err error) (*Fetched, error) {
	if !IsDefaultSource(src) {
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	if dir, cached, ok := latestDefaultCacheDir(); ok {
		return &Fetched{Dir: dir, Source: src, ResolvedCommit: cached}, nil
	}
	return nil, offlineDefaultError(err, src)
}

func cachedDefault(src *Source) (*Fetched, bool) {
	if !IsDefaultSource(src) || !fullSHA.MatchString(src.RequestedRef) {
		return nil, false
	}
	dir, ok := defaultCacheDir(src.RequestedRef)
	if !ok {
		return nil, false
	}
	return &Fetched{
		Dir: dir, Source: src, ResolvedCommit: src.RequestedRef,
	}, true
}

func offlineDefaultError(err error, src *Source) error {
	return fmt.Errorf(
		"fetching default template: %w "+
			"(default template unavailable offline: no cached %q; "+
			"connect once with network)",
		err, src.RequestedRef)
}

func cloneRemote(
	ctx context.Context,
	src *Source,
	sha string,
) (*Fetched, error) {
	tmp, err := os.MkdirTemp("", "turutan-fetch-*")
	if err != nil {
		return nil, fmt.Errorf(
			"fetching source %q: creating temp dir: %w", src.Raw, err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if _, err := git.Clone(ctx, src.Repo, "", tmp, src.Depth); err != nil {
		cleanup()
		if IsDefaultSource(src) {
			return nil, offlineDefaultError(err, src)
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

func fetchLocalGit(ctx context.Context, src *Source) (*Fetched, error) {
	sha, err := git.ResolveLocal(src.Repo, src.RequestedRef)
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	tmp, err := os.MkdirTemp("", "turutan-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("fetching source %q: creating temp dir: %w", src.Raw, err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	if _, err := git.Clone(ctx, src.Repo, "", tmp, src.Depth); err != nil {
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

func fetchFilesystem(src *Source) (*Fetched, error) {
	if src.RequestedRef != "" {
		return nil, fmt.Errorf(
			"fetching source %q: ref %q is not supported "+"for filesystem sources",
			src.Raw, src.RequestedRef)
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
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("fetching source %q: resolving subpath: %w", src.Raw, err)
	}
	if err := filter.EnsureWithinRoot(root, resolved); err != nil {
		return "", fmt.Errorf("fetching source %q: %w", src.Raw, err)
	}
	return dir, nil
}

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

func latestDefaultCacheDir() (string, string, bool) {
	cache := config.ResolveCacheDir()
	if cache == "" {
		return "", "", false
	}
	base := filepath.Join(cache, "default")
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", "", false
	}
	var bestDir, bestSHA string
	var bestMod time.Time
	for _, entry := range entries {
		sha := strings.ToLower(entry.Name())
		if !fullSHA.MatchString(sha) {
			continue
		}
		dir := filepath.Join(base, entry.Name())
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if bestDir == "" || info.ModTime().After(bestMod) {
			bestDir, bestSHA, bestMod = dir, sha, info.ModTime()
		}
	}
	if bestDir == "" {
		return "", "", false
	}
	return bestDir, bestSHA, true
}

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
			return os.MkdirAll(target, 0o750) // #nosec G122 -- SafeJoin-pinned copy into this run's fresh staging dir; no attacker-controlled path components on the dst side
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304, G122 -- copies WalkDir-found file with symlinks skipped into SafeJoin-pinned fresh staging dir; no cross-user TOCTOU boundary
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) // #nosec G703, G122 -- target is SafeJoin-pinned with symlinks skipped; no cross-user TOCTOU boundary
	})
}
