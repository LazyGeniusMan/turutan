// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/filter"
	"github.com/LazyGeniusMan/turutan/internal/registry"
)

const shortSHALen = 12

// CheckOptions tunes the Check update-availability probe. Ref pins a
// version to compare against instead of the index latest.
// ~keep
type CheckOptions struct {
	Ref     string
	Engine  string
	Verbose bool
	Stdout  io.Writer
	Stderr  io.Writer
}

// CheckResult reports whether a newer template release is available. Old
// and New are template versions.
// ~keep
type CheckResult struct {
	Available bool
	Old       string
	New       string
}

// Check reports whether the project's pinned template release has an update
// available in its registry.
// ~keep
func Check(
	ctx context.Context,
	projectDir string,
	opts CheckOptions,
) (*CheckResult, error) {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("check: %w", err)
	}
	if state.SourceKind != "registry" {
		return nil, fmt.Errorf("check: unknown sourceKind %q", state.SourceKind)
	}
	vlogf(opts.Stderr, opts.Verbose, "check: stored template %q version %q digest %s",
		state.Template, state.RequestedRef, state.ResolvedCommit)
	fresh, changed, newInputs, err := reresolveRegistry(
		ctx, state, opts.Ref, opts.Engine, opts.Stderr, opts.Verbose)
	if err != nil {
		return nil, err
	}
	vlogf(opts.Stderr, opts.Verbose, "check: fresh version %s available %v", fresh, changed)
	result := &CheckResult{Old: state.RequestedRef, New: fresh, Available: changed}
	if changed {
		if _, err := fmt.Fprintf(stdout, "turutan: update available: %s -> %s\n", state.RequestedRef, fresh); err != nil {
			return nil, err
		}
	} else {
		if _, err := fmt.Fprintf(stdout, "turutan: template up to date @ %s\n", state.RequestedRef); err != nil {
			return nil, err
		}
	}
	reportNewInputs(stdout, newInputs)
	return result, nil
}

func reresolveRegistry(
	ctx context.Context,
	state *config.State,
	ref string,
	engine string,
	stderr io.Writer,
	verbose bool,
) (string, bool, []string, error) {
	registryURI := strings.TrimSpace(state.Registry)
	if registryURI == "" {
		return "", false, nil, fmt.Errorf("check: stored registry is empty")
	}
	storedRegistry, templateName, _, err := SplitStoredRef(state.Template)
	if err != nil {
		return "", false, nil, fmt.Errorf("check: %w", err)
	}
	if storedRegistry != registryURI {
		return "", false, nil, fmt.Errorf("check: stored template %q does not match registry %q", state.Template, registryURI)
	}
	if templateName == "" {
		return "", false, nil, fmt.Errorf("check: stored template %q has no template name", state.Template)
	}
	rr, err := ResolveRegistry(ctx, registryURI)
	if err != nil {
		return "", false, nil, fmt.Errorf("check: %w", err)
	}
	entry, err := rr.Idx.FindTemplate(templateName)
	if err != nil {
		return "", false, nil, fmt.Errorf("check: %w", err)
	}
	want := strings.TrimSpace(ref)
	if want == "" {
		want = entry.ResolvedVersion()
	} else if _, err := ResolveTemplateManifest(rr, templateName, want); err != nil {
		return "", false, nil, fmt.Errorf("check: %w", err)
	}
	if _, err := registry.ParseStrict(want); err != nil {
		return "", false, nil, fmt.Errorf("check: %w", err)
	}
	if want != state.RequestedRef && !isNewerVersion(want, state.RequestedRef) && want != entry.ResolvedVersion() {
		vlogf(stderr, verbose, "check: requested version %s is not newer than pinned %s", want, state.RequestedRef)
	}
	changed := want != state.RequestedRef
	newInputs := fetchRegistryInputs(ctx, rr, templateName, want, state, engine, stderr, verbose)
	return want, changed, newInputs, nil
}

func isNewerVersion(first, second string) bool {
	cmp, err := registry.Compare(first, second)
	if err != nil {
		return first != second
	}
	return cmp > 0
}

func fetchRegistryInputs(
	ctx context.Context,
	rr *ResolvedRegistry,
	templateName, want string,
	state *config.State,
	engine string,
	stderr io.Writer,
	verbose bool,
) []string {
	manifest, err := ResolveTemplateManifest(rr, templateName, want)
	if err != nil {
		vlogf(stderr, verbose, "check: skipping inputs report: %v", err)
		return nil
	}
	bundleDir, _, cleanup, err := FetchTemplateBundle(ctx, rr, manifest)
	if err != nil {
		vlogf(stderr, verbose, "check: skipping inputs report: %v", err)
		return nil
	}
	defer cleanup()
	templateManifest, err := config.LoadManifest(os.DirFS(bundleDir))
	if err != nil {
		vlogf(stderr, verbose, "check: skipping inputs report: %v", err)
		return nil
	}
	if _, err := checkEngineVersion(templateManifest, engine, stderr, verbose); err != nil {
		vlogf(stderr, verbose, "check: skipping inputs report: %v", err)
		return nil
	}
	return config.DiffInputs(state.Data, templateManifest)
}

func userOwned(slash string, manifest *config.Manifest, state *config.State, activePreserves []string) (bool, error) {
	kept, err := isPreservedPath(slash, manifest, activePreserves)
	if err != nil {
		return false, err
	}
	if kept {
		return true, nil
	}
	skipped, err := filter.MatchAny(state.Skip, slash)
	if err != nil {
		return false, fmt.Errorf("matching skip entries: %w", err)
	}
	return skipped, nil
}
