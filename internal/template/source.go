// SPDX-License-Identifier: Apache-2.0

package template

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/git"
)

const DefaultDepth = git.ShallowDepth

var errAtRef = errors.New("use ?ref= instead of @ref")

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
	if err := rejectAtRef(raw, locator); err != nil {
		return nil, err
	}
	if !strings.Contains(locator, "://") && git.IsScpLike(locator) {
		return &Source{
			Raw: raw, Kind: KindRemoteGit, Repo: locator, Subpath: subpath,
			RequestedRef: requestedRef, Depth: depth,
		}, nil
	}
	parsed, err := url.Parse(locator)
	if err != nil {
		return nil, fmt.Errorf(
			"parsing source %q: invalid locator %q: %w", raw, locator, err)
	}
	return dispatchScheme(raw, locator, subpath, requestedRef, depth, forcedGit, parsed)
}

func dispatchScheme(
	raw, locator, subpath, requestedRef string,
	depth int,
	forcedGit bool,
	parsed *url.URL,
) (*Source, error) {
	if forcedGit && parsed.Scheme == "" {
		return parseForcedGitPath(raw, locator, subpath, requestedRef, depth)
	}
	switch parsed.Scheme {
	case "https", "http", "ssh", "git":
		return &Source{
			Raw: raw, Kind: KindRemoteGit, Repo: locator, Subpath: subpath,
			RequestedRef: requestedRef, Depth: depth,
		}, nil
	case "file":
		return parseLocalPath(
			raw, parsed.Path, subpath, requestedRef, depth, forcedGit)
	case "":
		return parseLocalPath(
			raw, locator, subpath, requestedRef, depth, forcedGit)
	default:
		return nil, fmt.Errorf(
			"parsing source %q: unsupported scheme %q", raw, parsed.Scheme)
	}
}

func rejectAtRef(raw, locator string) error {
	if strings.Contains(locator, "://") {
		if tail := locator[strings.LastIndex(locator, "/")+1:]; strings.Contains(tail, "@") {
			return fmt.Errorf(
				"parsing source %q: invalid locator %q: @ref form "+"is unsupported, use ?ref=: %w",
				raw, locator, errAtRef)
		}
		return nil
	}
	if git.IsScpLike(locator) {
		pathPart := locator[strings.Index(locator, ":")+1:]
		tail := pathPart
		if idx := strings.LastIndex(pathPart, "/"); idx >= 0 {
			tail = pathPart[idx+1:]
		}
		if strings.Contains(tail, "@") {
			return fmt.Errorf(
				"parsing source %q: invalid locator %q: @ref form "+"is unsupported, use ?ref=: %w",
				raw, locator, errAtRef)
		}
	}
	return nil
}

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

func parseForcedGitPath(raw, locator, subpath, requestedRef string, depth int) (*Source, error) {
	info, err := os.Stat(locator)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("parsing source %q: git:: path %q does not exist or is not a directory", raw, locator)
	}
	if !isGitDir(locator) {
		return nil, fmt.Errorf("parsing source %q: git:: path %q is not a git repository", raw, locator)
	}
	return &Source{
		Raw: raw, Kind: KindLocalGit, Repo: locator, Subpath: subpath,
		RequestedRef: requestedRef, Depth: depth,
	}, nil
}

func parseLocalPath(raw, locator, subpath, requestedRef string, depth int, forcedGit bool) (*Source, error) {
	info, err := os.Stat(locator) // #nosec G703 -- locator is the explicit CLI source path; read-only existence probe with no write
	if err != nil || !info.IsDir() {
		if forcedGit {
			return nil, fmt.Errorf("parsing source %q: git:: path %q does not exist or is not a directory", raw, locator)
		}
		return nil, fmt.Errorf("parsing source %q: local path %q does not exist", raw, locator)
	}
	if isGitDir(locator) {
		return &Source{
			Raw: raw, Kind: KindLocalGit, Repo: locator, Subpath: subpath,
			RequestedRef: requestedRef, Depth: depth,
		}, nil
	}
	if forcedGit {
		return nil, fmt.Errorf("parsing source %q: git:: path %q is not a git repository", raw, locator)
	}
	return &Source{
		Raw: raw, Kind: KindFilesystem, Repo: locator, Subpath: subpath,
		RequestedRef: requestedRef, Depth: depth,
	}, nil
}

func isGitDir(path string) bool {
	if info, err := os.Stat(filepath.Join(path, ".git")); err == nil && (info.IsDir() || !info.IsDir()) { // #nosec G703 -- joins fixed ".git" segment onto the CLI-supplied repo path; read-only existence probe
		return true
	}
	return isBareLayout(path)
}

func isBareLayout(path string) bool {
	for _, entry := range []string{"HEAD", "objects", "refs"} {
		if _, err := os.Stat(filepath.Join(path, entry)); err != nil { // #nosec G703 -- entry is one of HEAD/objects/refs constants; read-only bare-repo layout probe
			return false
		}
	}
	return true
}
