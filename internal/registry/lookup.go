// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"fmt"
	"strings"
)

// ResolveRegistryURI returns the parsed URI for a named registry. An
// explicit entry in registries wins; the reserved DefaultRegistryName falls
// back to DefaultRegistryURI so `turutan` resolves without an explicit
// `registry add`. Any other unknown name is an error.
// ~keep
func ResolveRegistryURI(registries map[string]string, name string) (URI, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return URI{}, fmt.Errorf("unknown registry %q: name is empty", name)
	}
	if raw, ok := registries[trimmed]; ok {
		if strings.TrimSpace(raw) == "" {
			return URI{}, fmt.Errorf("registry %q has an empty uri", trimmed)
		}
		parsed, err := ParseURI(raw)
		if err != nil {
			return URI{}, fmt.Errorf("registry %q: %w", trimmed, err)
		}
		return parsed, nil
	}
	if trimmed == DefaultRegistryName {
		return ParseURI(DefaultRegistryURI)
	}
	return URI{}, fmt.Errorf(
		"unknown registry %q: add it with `turutan registry add %s <uri>`", trimmed, trimmed)
}
