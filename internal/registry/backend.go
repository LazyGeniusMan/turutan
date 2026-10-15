// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"errors"
	"fmt"
)

var ErrRelativeURI = errors.New("relative uri needs a base: resolve it with URI.ResolveRelativeTo first")

type Backend interface {
	Scheme() Scheme
	Bundle(ctx context.Context, srcDir string, uri URI, auth AuthConfig) (string, error)
	Fetch(ctx context.Context, uri URI, auth AuthConfig, destDir string) error
}

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
