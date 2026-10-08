// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// DefaultDepth is the default shallow fetch depth when ?depth= is absent.
const DefaultDepth = 1

// scpLike matches scp-like SSH locators (user@host:path) without "://".
// It must be tested before url.Parse because "@" collides with query refs.
var scpLike = regexp.MustCompile(`^[^/]+@[^:]+:.+$`)

// ParseSource parses raw into a Source following the EBNF in spec §3.1:
//
//	source := [ "git::" ] locator [ "//" subpath ] [ "?" query ]
//
// Rules: the optional git:: prefix is stripped first (forcing git
// interpretation); ?query (ref=/depth=, never @ref) is split off the end;
// //subpath is split outside "://" (first "//" after the scheme end);
// scp-like locators are detected before url.Parse; otherwise url.Parse
// applies and a missing scheme with an existing local path means a
// filesystem (or local-git when the path holds a repository) source.
// The literal "default" resolves to DefaultSource.
func ParseSource(raw string) (*Source, error) {
	if raw == "" {
		return nil, fmt.Errorf("parsing source: empty source URI")
	}
	if raw == DefaultAlias {
		return DefaultSource(), nil
	}
	rest := raw
	forcedGit := false
	if strings.HasPrefix(rest, "git::") {
		forcedGit = true
		rest = strings.TrimPrefix(rest, "git::")
	}
	if rest == "" {
		return nil, fmt.Errorf("parsing source %q: nothing after git:: prefix", raw)
	}
	rest, requestedRef, depth, err := splitQuery(raw, rest)
	if err != nil {
		return nil, err
	}
	locator, subpath, err := splitSubpath(raw, rest)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(locator, "://") && scpLike.MatchString(locator) {
		return &Source{Raw: raw, Kind: KindRemoteGit, Repo: locator, Subpath: subpath, RequestedRef: requestedRef, Depth: depth}, nil
	}
	parsed, err := url.Parse(locator)
	if err != nil {
		return nil, fmt.Errorf("parsing source %q: invalid locator %q: %w", raw, locator, err)
	}
	if forcedGit && parsed.Scheme == "" {
		return parseForcedGitPath(raw, locator, subpath, requestedRef, depth)
	}
	switch parsed.Scheme {
	case "https", "http", "ssh", "git":
		return &Source{Raw: raw, Kind: KindRemoteGit, Repo: locator, Subpath: subpath, RequestedRef: requestedRef, Depth: depth}, nil
	case "file":
		return parseLocalPath(raw, parsed.Path, subpath, requestedRef, depth, forcedGit)
	case "":
		return parseLocalPath(raw, locator, subpath, requestedRef, depth, forcedGit)
	default:
		return nil, fmt.Errorf("parsing source %q: unsupported scheme %q", raw, parsed.Scheme)
	}
}

// splitQuery splits "?query" off the end of rest and parses ref=/depth=.
// Only query style is accepted (?ref=, never @ref) because "@" collides
// with scp-like user@host:path locators.
func splitQuery(raw, rest string) (string, string, int, error) {
	depth := DefaultDepth
	if idx := strings.Index(rest, "?"); idx >= 0 {
		query := rest[idx+1:]
		rest = rest[:idx]
		values, err := url.ParseQuery(query)
		if err != nil {
			return "", "", 0, fmt.Errorf("parsing source %q: invalid query %q: %w", raw, query, err)
		}
		ref := values.Get("ref")
		if depthRaw := values.Get("depth"); depthRaw != "" {
			parsed, err := strconv.Atoi(depthRaw)
			if err != nil || parsed <= 0 {
				return "", "", 0, fmt.Errorf("parsing source %q: invalid depth %q: must be a positive integer", raw, depthRaw)
			}
			depth = parsed
		}
		for key := range values {
			if key != "ref" && key != "depth" {
				return "", "", 0, fmt.Errorf("parsing source %q: unknown query parameter %q (want ref= or depth=)", raw, key)
			}
		}
		return rest, ref, depth, nil
	}
	return rest, "", depth, nil
}

// splitSubpath splits "//subpath" outside "://" using go-getter
// SourceDirSubdir semantics: the first "//" after the scheme end delimits
// the monorepo subdir. "?" is never part of the subpath (splitQuery runs
// first).
func splitSubpath(raw, rest string) (string, string, error) {
	searchFrom := 0
	if idx := strings.Index(rest, "://"); idx >= 0 {
		searchFrom = idx + len("://")
	}
	rel := rest[searchFrom:]
	idx := strings.Index(rel, "//")
	if idx < 0 {
		return rest, "", nil
	}
	locator := rest[:searchFrom+idx]
	subpath := rest[searchFrom+idx+len("//"):]
	if subpath == "" {
		return "", "", fmt.Errorf("parsing source %q: empty subpath after //", raw)
	}
	cleaned := path.Clean(subpath)
	if cleaned == "." {
		return locator, "", nil
	}
	if slices.Contains(strings.Split(cleaned, "/"), "..") {
		return "", "", fmt.Errorf("parsing source %q: subpath %q escapes its repository", raw, subpath)
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", "", fmt.Errorf("parsing source %q: subpath %q must be relative", raw, subpath)
	}
	return locator, cleaned, nil
}

// parseForcedGitPath handles git:: locators without a URL scheme: the path
// must exist and hold a git repository, otherwise the forced-git request
// is unsatisfiable.
func parseForcedGitPath(raw, locator, subpath, requestedRef string, depth int) (*Source, error) {
	info, err := os.Stat(locator)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("parsing source %q: git:: path %q does not exist or is not a directory", raw, locator)
	}
	if !isGitDir(locator) {
		return nil, fmt.Errorf("parsing source %q: git:: path %q is not a git repository", raw, locator)
	}
	return &Source{Raw: raw, Kind: KindLocalGit, Repo: locator, Subpath: subpath, RequestedRef: requestedRef, Depth: depth}, nil
}

// parseLocalPath maps a scheme-less or file:// locator onto local-git
// (path holds a repository) or filesystem (anything else that exists).
func parseLocalPath(raw, locator, subpath, requestedRef string, depth int, forcedGit bool) (*Source, error) {
	info, err := os.Stat(locator)
	if err != nil || !info.IsDir() {
		if forcedGit {
			return nil, fmt.Errorf("parsing source %q: git:: path %q does not exist or is not a directory", raw, locator)
		}
		return nil, fmt.Errorf("parsing source %q: local path %q does not exist", raw, locator)
	}
	if isGitDir(locator) {
		return &Source{Raw: raw, Kind: KindLocalGit, Repo: locator, Subpath: subpath, RequestedRef: requestedRef, Depth: depth}, nil
	}
	if forcedGit {
		return nil, fmt.Errorf("parsing source %q: git:: path %q is not a git repository", raw, locator)
	}
	return &Source{Raw: raw, Kind: KindFilesystem, Repo: locator, Subpath: subpath, RequestedRef: requestedRef, Depth: depth}, nil
}

// isGitDir reports whether path holds a git repository: either a worktree
// (path/.git exists) or a bare repository (HEAD, objects and refs exist).
// It uses plain filesystem probes so template parsing never imports go-git.
func isGitDir(path string) bool {
	if info, err := os.Stat(filepath.Join(path, ".git")); err == nil && (info.IsDir() || !info.IsDir()) {
		return true
	}
	for _, entry := range []string{"HEAD", "objects", "refs"} {
		if _, err := os.Stat(filepath.Join(path, entry)); err != nil {
			return false
		}
	}
	return true
}
