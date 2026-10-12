// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/LazyGeniusMan/turutan/internal/config"
	"github.com/LazyGeniusMan/turutan/internal/template"
)

// ProfileOptions carries the fetch and logging knobs for ListProfiles.
// Profile listing resolves the same template ref as update, so it needs the
// ref override plus engine and verbosity plumbing.
// ~keep
type ProfileOptions struct {
	Ref     string
	Engine  string
	Verbose bool
	Stderr  io.Writer
}

// ProfileInfo reports the profiles a template offers versus what the
// project enabled, so CLI output can show both without a second fetch.
// ~keep
type ProfileInfo struct {
	Offered []string
	Enabled []string
}

// ListProfiles fetches the template manifest and reports offered versus
// enabled profiles. Commands use it to validate --profiles values and to
// show what a project opted into without re-rendering anything.
// ~keep
func ListProfiles(
	ctx context.Context,
	projectDir string,
	opts ProfileOptions,
) (*ProfileInfo, error) {
	state, err := config.LoadState(os.DirFS(projectDir))
	if err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	src, err := parseStoredSource(projectDir, state.Template)
	if err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	if opts.Ref != "" {
		src.RequestedRef = opts.Ref
	}
	vlogf(opts.Stderr, opts.Verbose,
		"profiles: template %q ref %q", state.Template, src.RequestedRef)
	fetched, err := template.Fetch(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	if fetched.Cleanup != nil {
		defer fetched.Cleanup()
	}
	manifest, err := config.LoadManifest(os.DirFS(fetched.Dir))
	if err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}
	if _, err := checkMinEngine(
		manifest, opts.Engine, opts.Stderr, opts.Verbose); err != nil {
		return nil, err
	}
	enabled := append([]string{}, state.GetProfiles()...)
	sort.Strings(enabled)
	return &ProfileInfo{
		Offered: offeredProfiles(manifest),
		Enabled: enabled,
	}, nil
}
