// SPDX-License-Identifier: Apache-2.0

// Package git wraps pure-Go git transport (ls-remote, clone, open, auth).
// It is the only package that may import go-git; the dependency is added
// in M1, so M0 holds the option surface without external imports.
package git

// ShoreDepth is the default shallow fetch depth (spec §3: depth defaults to 1).
const ShallowDepth = 1

// CloneOptions describes a fetch; transport arrives in M1.
type CloneOptions struct {
	URL   string
	Ref   string
	Depth int
	Dir   string
}
