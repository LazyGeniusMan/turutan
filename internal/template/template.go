// SPDX-License-Identifier: Apache-2.0

// Package template parses template source URIs, resolves refs, fetches
// content and renders it. The production default template is always
// fetched as a remote-git URL; it is never bundled into the binary.
package template

import (
	"fmt"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/git"
)

// Production default template coordinates (spec §8).
const (
	// DefaultURL is the canonical remote-git URL of the default template.
	DefaultURL = "git::https://github.com/LazyGeniusMan/turutan.git//templates/default"
	// DefaultRef is the floating stable ref of the default template.
	DefaultRef = "templates-default/v1"
	// DefaultAlias is the CLI shorthand resolving to DefaultURL.
	DefaultAlias = "default"
	// defaultRepo is the clone URL half of DefaultURL.
	defaultRepo = "https://github.com/LazyGeniusMan/turutan.git"
	// defaultSubpath is the monorepo subdir half of DefaultURL.
	defaultSubpath = "templates/default"
)

// SourceKind identifies how a template source is fetched.
type SourceKind string

// Source kinds (spec §3).
const (
	KindRemoteGit  SourceKind = "remote-git"
	KindLocalGit   SourceKind = "local-git"
	KindFilesystem SourceKind = "filesystem"
)

// Source describes a parsed template source (spec §3.1): Repo is the clone
// URL (remote-git) or local path (local-git, filesystem) without subpath
// or query; Subpath is the // monorepo subdir ("" means the whole tree);
// RequestedRef is the ?ref= expression ("" means the default branch for
// git kinds and is unused for filesystem); Depth is the ?depth= value and
// defaults to DefaultDepth.
type Source struct {
	Raw          string
	Kind         SourceKind
	Repo         string
	Subpath      string
	RequestedRef string
	Depth        int
}

// DefaultSource returns the production default remote-git source: the
// built-in remote-default URL at its floating stable ref. It is fetched
// like any other remote-git source, never read from disk nor embedded.
func DefaultSource() *Source {
	return &Source{
		Raw:          DefaultURL + "?ref=" + DefaultRef,
		Kind:         KindRemoteGit,
		Repo:         defaultRepo,
		Subpath:      defaultSubpath,
		RequestedRef: DefaultRef,
		Depth:        DefaultDepth,
	}
}

// IsDefaultSource reports whether src addresses the built-in default
// template repository and subpath, regardless of the requested ref.
func IsDefaultSource(src *Source) bool {
	return src != nil && src.Kind == KindRemoteGit && src.Repo == defaultRepo && src.Subpath == defaultSubpath
}

// ResolveAlias maps an omitted or "default" source argument onto the
// built-in remote-default URL; any other input parses normally.
func ResolveAlias(raw string) (*Source, error) {
	if raw == "" || raw == DefaultAlias {
		return DefaultSource(), nil
	}
	return ParseSource(raw)
}

// String reconstructs the canonical URI form of s.
func (s *Source) String() string {
	var b strings.Builder
	if s.Kind == KindRemoteGit && !strings.HasPrefix(s.Repo, "git::") && isRemoteURL(s.Repo) {
		b.WriteString("git::")
	}
	b.WriteString(s.Repo)
	if s.Subpath != "" {
		b.WriteString("//")
		b.WriteString(s.Subpath)
	}
	query := ""
	if s.RequestedRef != "" {
		query = "?ref=" + s.RequestedRef
	}
	if s.Depth != 0 && s.Depth != DefaultDepth {
		if query == "" {
			query = fmt.Sprintf("?depth=%d", s.Depth)
		} else {
			query += fmt.Sprintf("&depth=%d", s.Depth)
		}
	}
	b.WriteString(query)
	return b.String()
}

// isRemoteURL reports whether repo parses as a remote (non-local) locator.
// Scp-like detection shares git.IsScpLike with source parsing so the two
// never disagree.
func isRemoteURL(repo string) bool {
	if strings.Contains(repo, "://") {
		return true
	}
	return git.IsScpLike(repo)
}
