// SPDX-License-Identifier: Apache-2.0

package template

import (
	"fmt"
	"strings"

	"github.com/LazyGeniusMan/turutan/internal/git"
)

const (
	DefaultURL     = "git::https://github.com/LazyGeniusMan/turutan.git//templates/default"
	DefaultRef     = "templates-default/v1"
	DefaultAlias   = "default"
	defaultRepo    = "https://github.com/LazyGeniusMan/turutan.git"
	defaultSubpath = "templates/default"
)

type SourceKind string

const (
	KindRemoteGit  SourceKind = "remote-git"
	KindLocalGit   SourceKind = "local-git"
	KindFilesystem SourceKind = "filesystem"
)

type Source struct {
	Raw          string
	Kind         SourceKind
	Repo         string
	Subpath      string
	RequestedRef string
	Depth        int
}

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

func IsDefaultSource(src *Source) bool {
	return src != nil && src.Kind == KindRemoteGit && src.Repo == defaultRepo && src.Subpath == defaultSubpath
}

func ResolveAlias(raw string) (*Source, error) {
	if raw == "" || raw == DefaultAlias {
		return DefaultSource(), nil
	}
	return ParseSource(raw)
}

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

func isRemoteURL(repo string) bool {
	if strings.Contains(repo, "://") {
		return true
	}
	return git.IsScpLike(repo)
}
