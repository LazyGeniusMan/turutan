// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"errors"
	"fmt"
)

// ErrRelativeURI is returned when a relative URI is used without a base.
// ~keep
var ErrRelativeURI = errors.New("relative uri needs a base: resolve it with URI.ResolveRelativeTo first")

// Backend bundles a template tree into a registry and fetches one back.
// ~keep
type Backend interface {
	Scheme() Scheme
	Bundle(ctx context.Context, srcDir string, uri URI, auth AuthConfig) (string, error)
	Fetch(ctx context.Context, uri URI, auth AuthConfig, destDir string) error
}

// BackendFor returns the Backend handling scheme, or an error for unknown schemes.
// ~keep
func BackendFor(scheme Scheme) (Backend, error) {
	switch scheme {
	case SchemeFS:
		return NewFSBackend(), nil
	case SchemeHTTP, SchemeHTTPS:
		return NewHTTPBackend(nil, scheme), nil
	case SchemeS3:
		return NewS3Backend(nil), nil
	case SchemeGit:
		return NewGitBackend(nil), nil
	case SchemeOCI:
		return NewOCIBackend(nil), nil
	default:
		return nil, fmt.Errorf("unknown registry scheme %q", scheme)
	}
}
