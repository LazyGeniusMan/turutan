// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
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

// shortSHALen abbreviates hex identities for human summary lines.
const shortSHALen = 12

// CheckUpdateOptions carries per-run knobs for the check-update flow.
type CheckUpdateOptions struct {
	// Ref overrides the stored requestedRef for this run only; the stored
	// state is never modified.
	Ref string
	// Verbose enables per-run diagnostics on Stderr.
	Verbose bool
	// Stdout receives the one human result line; it defaults to the OS
	// stdout when nil so diagnostics stay capturable in tests.
	Stdout io.Writer
	// Stderr receives verbose diagnostics; it defaults to the OS
	// stderr when nil.
	Stderr io.Writer
}

// CheckUpdateResult reports the re-resolve outcome: Available is true when
// the template moved since bootstrap. Old is the stored identity, New the
// freshly resolved one (equal to Old when nothing changed).
type CheckUpdateResult struct {
	Available bool
	Old       string
	New       string
}

// CheckUpdate re-resolves the stored template ref and compares it against
// the stored identity without mutating any state: remote-git re-resolves
// via ls-remote, local-git resolves offline, and filesystem sources
// re-render the template with the stored answers and compare manifest
// hashes (there is no ref to track). Default-template projects are
// ordinary remote-git entries: their floating stable ref re-resolves like
// any other. It prints one human line to Stdout; callers map Available to
// exit code 2 per spec §5.2.
func CheckUpdate(projectDir string, opts CheckUpdateOptions) (*CheckUpdateResult, error) {
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
	fresh, changed, err := reresolve(state, ref, projectDir)
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

// reresolve returns the fresh identity for state plus whether it differs
// from the stored one: the resolved commit SHA for git kinds, the
// re-rendered manifest digest for filesystem sources.
func reresolve(state *config.State, ref, projectDir string) (string, bool, error) {
	switch template.SourceKind(state.SourceKind) {
	case template.KindRemoteGit:
		src, err := template.ParseSource(state.Template)
		if err != nil {
			return "", false, fmt.Errorf("check-update: %w", err)
		}
		sha, err := git.ResolveRemoteRef(src.Repo, ref)
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
		return reresolveFilesystem(state, ref, projectDir)
	default:
		return "", false, fmt.Errorf("check-update: unknown sourceKind %q", state.SourceKind)
	}
}

// reresolveFilesystem re-renders the current filesystem template with the
// stored answers and compares manifest hashes. Files owned by the user
// (manifest preserve globs, state skip entries) are excluded from both
// sides: they carry local content in the lock, so including them would
// report template movement on every local edit. A template change confined
// to preserved paths is therefore not reported here; diff shows it.
func reresolveFilesystem(state *config.State, ref, projectDir string) (string, bool, error) {
	if ref != "" {
		return "", false, fmt.Errorf("check-update: ref %q is not supported for filesystem sources", ref)
	}
	lock, err := config.LoadLock(os.DirFS(projectDir))
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	src, err := template.ParseSource(state.Template)
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	fetched, err := template.Fetch(src)
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
	stage, err := os.MkdirTemp("", "turutan-check-update-*")
	if err != nil {
		return "", false, fmt.Errorf("check-update: creating staging dir: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := template.RenderDir(fetched.Dir, stage, state.Answers); err != nil {
		return "", false, fmt.Errorf("check-update: %w", err)
	}
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
	changed := config.ComputeManifestHash(rendered) != config.ComputeManifestHash(locked)
	if !changed {
		return state.ResolvedCommit, false, nil
	}
	fresh := strings.TrimPrefix(config.ComputeManifestHash(display), "sha256:")
	return fresh, true, nil
}

// renderedEntries lists the staged render as lock entries, always skipping
// manifest-ignored paths (they never reach the lock). With includeUserOwned
// false it also skips user-owned paths (preserve globs, skip entries) so
// the hash compares template content only; with true it keeps them for the
// display identity.
func renderedEntries(stage string, manifest *config.Manifest, state *config.State, includeUserOwned bool) ([]config.LockFile, error) {
	var files []config.LockFile
	err := filepath.WalkDir(stage, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filter.ToSlash(rel)
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		ignored, err := filter.MatchAny(manifest.Ignore, slash)
		if err != nil {
			return fmt.Errorf("matching ignore globs: %w", err)
		}
		if ignored {
			return nil
		}
		if !includeUserOwned {
			owned, err := userOwned(slash, manifest, state)
			if err != nil {
				return err
			}
			if owned {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, config.FileEntry(slash, data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// filterLockEntries drops lock entries for user-owned paths so the stored
// side compares template content only, mirroring renderedEntries.
func filterLockEntries(files []config.LockFile, manifest *config.Manifest, state *config.State) ([]config.LockFile, error) {
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

// userOwned reports whether a template-relative path carries local content:
// manifest preserve globs or state skip entries always prefer local files.
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
