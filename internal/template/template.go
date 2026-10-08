// SPDX-License-Identifier: Apache-2.0

// Package template parses template source URIs, resolves refs, fetches
// content and renders it. Full implementation arrives in M1; M0 holds the
// type surface so dependents compile. The production default template is
// always fetched as a remote-git URL; it is never bundled into the binary.
package template

import "fmt"

// Production default template coordinates (spec §8).
const (
	// DefaultURL is the canonical remote-git URL of the default template.
	DefaultURL = "git::https://github.com/LazyGeniusMan/turutan.git//templates/default"
	// DefaultRef is the floating stable ref of the default template.
	DefaultRef = "templates-default/v1"
	// DefaultAlias is the CLI shorthand resolving to DefaultURL.
	DefaultAlias = "default"
)

// SourceKind identifies how a template source is fetched.
type SourceKind string

// Source kinds (spec §3).
const (
	KindRemoteGit  SourceKind = "remote-git"
	KindLocalGit   SourceKind = "local-git"
	KindFilesystem SourceKind = "filesystem"
)

// Source describes a parsed template source. URI parsing arrives in M1.
type Source struct {
	Raw     string
	Kind    SourceKind
	Subpath string
	Ref     string
}

// ParseSource parses raw into a Source; M1 implements the EBNF in spec §3.
func ParseSource(raw string) (*Source, error) {
	return nil, fmt.Errorf("parse source %q: %w", raw, errNotImplemented())
}

// DefaultSource returns the production default remote-git source.
func DefaultSource() *Source {
	return &Source{Raw: DefaultURL, Kind: KindRemoteGit, Subpath: "templates/default", Ref: DefaultRef}
}
