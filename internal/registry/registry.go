// SPDX-License-Identifier: Apache-2.0

// Package registry defines the core registry model for turutan: template
// authors, content URIs, semver helpers, auth configuration, template
// manifests and the registry index file. It is the foundation every other
// registry subcommand builds on. Fetch and push backends live elsewhere.
// ~keep
package registry

// Reserved default registry identity. DefaultRegistryURI is the git address
// of the turutan source repository at main and DefaultTemplateRepo addresses
// the default template source tree at main.
// ~keep
const (
	DefaultRegistryURI  = "git+https://github.com/LazyGeniusMan/turutan.git?ref=main"
	DefaultTemplateName = "default"
	DefaultTemplateRepo = "git+https://github.com/LazyGeniusMan/turutan.git?ref=main&subpath=templates/default"
)

// DefaultTemplateRef returns the full git reference for the default
// template at the given version, without fetching anything.
// ~keep
func DefaultTemplateRef(version string) string {
	return "git+https://github.com/LazyGeniusMan/turutan.git?ref=templates/default/" + version + "&subpath=templates/default"
}
