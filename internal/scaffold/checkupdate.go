// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/git"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

const shortSHALen = 12

type CheckUpdateOptions struct {
	Ref     string
	Engine  string
	Verbose bool
	Stdout  io.Writer
	Stderr  io.Writer
}

type CheckUpdateResult struct {
	Available bool
	Old       string
	New       string
}

func CheckUpdate(
	ctx context.Context,
	projectDir string,
	opts CheckUpdateOptions,
) (*CheckUpdateResult, error) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("check-update: %w", err)
	}
	ref := state.RequestedRef
	if opts.Ref != "" {
		ref = opts.Ref
	}
	vlogf(opts.Stderr, opts.Verbose, "check-update: stored ref %q commit %s", state.RequestedRef, state.ResolvedCommit)
	fresh, changed, err := reresolve(
		ctx, state, ref, projectDir, opts.Engine, opts.Stderr, opts.Verbose)
	if err != nil {
		return nil, err
	}
	vlogf(opts.Stderr, opts.Verbose, "check-update: fresh commit %s available %v", fresh, changed)
	result := &CheckUpdateResult{Old: state.ResolvedCommit, New: fresh, Available: changed}
	if changed {
		fmt.Fprintf(stdout, "turutan: update available: %s -> %s\n", shortSHA(state.ResolvedCommit), shortSHA(fresh))
	} else {
		fmt.Fprintf(stdout, "turutan: template up to date @ %s\n", shortSHA(state.ResolvedCommit))
	}
	return result, nil
}

func reresolve(
	ctx context.Context,
	state *config.State,
	ref, projectDir, engine string,
	stderr io.Writer,
	verbose bool,
) (string, bool, error) {
	switch template.SourceKind(state.SourceKind) {
	case template.KindRemoteGit:
		src, err := template.ParseSource(state.Template)
		if err != nil {
			return "", false, fmt.Errorf("check-update: %w", err)
		}
		sha, err := git.ResolveRemoteRef(ctx, src.Repo, ref)
		if err != nil {
			return "", false, fmt.Errorf("check-update: %w", err)
		}
		return sha, sha != state.ResolvedCommit, nil
	case template.KindLocalGit:
		src, err := template.ParseSource(state.Template)
		if err != nil {
			return "", false, fmt.Errorf("check-update: %w", err)
		}
		sha, err := git.ResolveLocal(src.Repo, ref)
		if err != nil {
			return "", false, fmt.Errorf("check-update: %w", err)
		}
		return sha, sha != state.ResolvedCommit, nil
	case template.KindFilesystem:
		return reresolveFilesystem(ctx, state, ref, projectDir, engine, stderr, verbose)
	default:
		return "", false, fmt.Errorf("check-update: unknown sourceKind %q", state.SourceKind)
	}
}

func reresolveFilesystem(
	ctx context.Context,
	state *config.State,
	ref, projectDir, engine string,
	stderr io.Writer,
	verbose bool,
) (string, bool, error) {
	if ref != "" {
		return "", false, fmt.Errorf(
			"check-update: ref %q is not supported "+
				"for filesystem sources",
			ref)
	}
	lock, _, manifest, stage, err := prepareFilesystemReresolve(
		ctx, state, projectDir, engine, stderr, verbose)
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(stage)
	return compareFilesystemHashes(stage, manifest, state, lock)
}

func prepareFilesystemReresolve(
	ctx context.Context,
	state *config.State,
	projectDir, engine string,
	stderr io.Writer,
	verbose bool,
) (*config.Lock, *template.Source, *config.Manifest, string, error) {
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("check-update: %w", err)
	}
	src, err := template.ParseSource(state.Template)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("check-update: %w", err)
	}
	fetched, err := template.Fetch(ctx, src)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("check-update: %w", err)
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("check-update: %w", err)
	}
	if _, err := checkMinEngine(manifest, engine, stderr, verbose); err != nil {
		return nil, nil, nil, "", err
	}
	stage, err := os.MkdirTemp("", "turutan-check-update-*")
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf(
			"check-update: creating staging dir: %w", err)
	}
	if err := template.RenderDir(fetched.Dir, stage, state.Answers); err != nil {
		_ = os.RemoveAll(stage)
		return nil, nil, nil, "", fmt.Errorf("check-update: %w", err)
	}
	return lock, src, manifest, stage, nil
}

func compareFilesystemHashes(
	stage string,
	manifest *config.Manifest,
	state *config.State,
	lock *config.Lock,
) (string, bool, error) {
	rendered, err := renderedEntries(stage, manifest, state, false)
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	display, err := renderedEntries(stage, manifest, state, true)
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	locked, err := filterLockEntries(lock.Files, manifest, state)
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	changed := config.ComputeManifestHash(rendered) !=
		config.ComputeManifestHash(locked)
	if !changed {
		return state.ResolvedCommit, false, nil
	}
	fresh := strings.TrimPrefix(
		config.ComputeManifestHash(display), "sha256:")
	return fresh, true, nil
}

func renderedEntries(
	stage string,
	manifest *config.Manifest,
	state *config.State,
	includeUserOwned bool,
) ([]config.LockFile, error) {
	var files []config.LockFile
	collector := &renderCollector{
		stage: stage, manifest: manifest, state: state,
		includeUserOwned: includeUserOwned,
	}
	err := filepath.WalkDir(stage, func(path string, d fs.DirEntry, err error) error {
		return collector.collect(path, d, err, &files)
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

type renderCollector struct {
	stage            string
	manifest         *config.Manifest
	state            *config.State
	includeUserOwned bool
}

func (c *renderCollector) collect(
	path string,
	d fs.DirEntry,
	walkErr error,
	files *[]config.LockFile,
) error {
	if walkErr != nil {
		return walkErr
	}
	rel, err := filepath.Rel(c.stage, path)
	if err != nil {
		return err
	}
	if rel == "." || d.IsDir() {
		return nil
	}
	if skip, err := skipStagedEntry(d); err != nil || skip {
		return err
	}
	slash := filter.ToSlash(rel)
	if skip, err := c.skipListed(slash); err != nil || skip {
		return err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- reads file found by WalkDir of this run's staging dir, not caller-controlled inclusion
	if err != nil {
		return err
	}
	*files = append(*files, config.FileEntry(slash, data))
	return nil
}

func skipStagedEntry(d fs.DirEntry) (bool, error) {
	if d.Type()&fs.ModeSymlink != 0 {
		return true, nil
	}
	info, err := d.Info()
	if err != nil {
		return false, err
	}
	if filter.IsSpecialFile(info) || !info.Mode().IsRegular() {
		return true, nil
	}
	return false, nil
}

func (c *renderCollector) skipListed(slash string) (bool, error) {
	ignored, err := filter.MatchAny(c.manifest.Ignore, slash)
	if err != nil {
		return false, fmt.Errorf("matching ignore globs: %w", err)
	}
	if ignored {
		return true, nil
	}
	if c.includeUserOwned {
		return false, nil
	}
	owned, err := userOwned(slash, c.manifest, c.state)
	if err != nil {
		return false, err
	}
	return owned, nil
}

func filterLockEntries(
	files []config.LockFile,
	manifest *config.Manifest,
	state *config.State,
) ([]config.LockFile, error) {
	var kept []config.LockFile
	for _, file := range files {
		owned, err := userOwned(file.Path, manifest, state)
		if err != nil {
			return nil, err
		}
		if !owned {
			kept = append(kept, file)
		}
	}
	return kept, nil
}

func userOwned(slash string, manifest *config.Manifest, state *config.State) (bool, error) {
	preserved, err := filter.MatchAny(manifest.Preserve, slash)
	if err != nil {
		return false, fmt.Errorf("matching preserve globs: %w", err)
	}
	if preserved {
		return true, nil
	}
	skipped, err := filter.MatchAny(state.Skip, slash)
	if err != nil {
		return false, fmt.Errorf("matching skip entries: %w", err)
	}
	return skipped, nil
}
