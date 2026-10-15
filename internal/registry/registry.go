// SPDX-License-Identifier: Apache-2.0

// Package registry defines the core registry model for turutan: template
// authors, content URIs, semver helpers, auth configuration, template
// manifests and the registry index file. It is the foundation every other
// registry subcommand builds on. Fetch and push backends live elsewhere.
// ~keep
package registry

// Reserved default registry identity. DefaultRegistryName is reserved for
// the built-in registry; DefaultRegistryURI is its OCI address and
// DefaultTemplateRepo addresses the default template without a tag.
// ~keep
const (
	DefaultRegistryName = "turutan"
	DefaultRegistryURI  = "oci://ghcr.io/lazygeniusman/turutan/templates"
	DefaultTemplateName = "default"
	DefaultTemplateRepo = "oci://ghcr.io/lazygeniusman/turutan/templates/default"
)

// DefaultTemplateRef returns the full OCI reference for the default
// template at the given version, without fetching anything.
// ~keep
func DefaultTemplateRef(version string) string {
	return DefaultTemplateRepo + ":" + version
}
