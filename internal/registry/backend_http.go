// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	httpPushTimeout  = 60 * time.Second
	httpFetchTimeout = 120 * time.Second
	maxErrorSnippet  = 512
)

type HTTPBackend struct {
	client *http.Client
	scheme Scheme
}

func NewHTTPBackend(client *http.Client, scheme Scheme) *HTTPBackend {
	if client == nil {
		client = &http.Client{Timeout: httpFetchTimeout}
	}
	if scheme != SchemeHTTP && scheme != SchemeHTTPS {
		scheme = SchemeHTTPS
	}
	return &HTTPBackend{client: client, scheme: scheme}
}

func (b *HTTPBackend) Scheme() Scheme { return b.scheme }

func (b *HTTPBackend) Bundle(ctx context.Context, srcDir string, uri URI, auth AuthConfig) (string, error) {
	if err := requireHTTPScheme(uri); err != nil {
		return "", err
	}
	blob, digest, err := BundleBytes(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling %s://%s%s: %w", uri.Scheme, uri.Host, ensureSlash(uri.Path), err)
	}
	address := uri.String()
	ctx, cancel := context.WithTimeout(ctx, httpPushTimeout)
	defer cancel()
	putErr := b.upload(ctx, http.MethodPut, address, auth, blob)
	if putErr == nil {
		return digest, nil
	}
	var statusErr *httpStatusError
	if !errors.As(putErr, &statusErr) || (statusErr.code != http.StatusMethodNotAllowed && statusErr.code != http.StatusNotImplemented) {
		return "", putErr
	}
	if err := b.upload(ctx, http.MethodPost, address, auth, blob); err != nil {
		return "", fmt.Errorf("pushing bundle to %s (PUT and POST rejected): %w", address, err)
	}
	return digest, nil
}

func (b *HTTPBackend) Fetch(ctx context.Context, uri URI, auth AuthConfig, destDir string) error {
	if err := requireHTTPScheme(uri); err != nil {
		return err
	}
	address := uri.String()
	ctx, cancel := context.WithTimeout(ctx, httpFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", address, err)
	}
	setAuthHeader(req, auth)
	req.Header.Set("Accept", BundleMediaType+", application/gzip, application/octet-stream")
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("fetching %s: %w", address, newHTTPStatusError(resp))
	}
	if err := ExtractBundle(resp.Body, destDir); err != nil {
		return fmt.Errorf("fetching %s: %w", address, err)
	}
	return nil
}

func (b *HTTPBackend) upload(ctx context.Context, method, address string, auth AuthConfig, blob []byte) error {
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(blob))
	if err != nil {
		return fmt.Errorf("pushing bundle to %s: %w", address, err)
	}
	setAuthHeader(req, auth)
	req.Header.Set("Content-Type", BundleMediaType)
	req.ContentLength = int64(len(blob))
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("pushing bundle to %s: %w", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("pushing bundle to %s: %w", address, newHTTPStatusError(resp))
	}
	return nil
}

func setAuthHeader(req *http.Request, auth AuthConfig) {
	switch auth.Kind {
	case AuthKindToken:
		if token, ok := auth.BearerToken(); ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case AuthKindBasic:
		if username, password, ok := auth.BasicAuth(); ok {
			req.SetBasicAuth(username, password)
		}
	}
}

type httpStatusError struct {
	code    int
	status  string
	snippet string
}

func (e *httpStatusError) Error() string {
	if e.snippet == "" {
		return fmt.Sprintf("server answered %s", e.status)
	}
	return fmt.Sprintf("server answered %s: %s", e.status, e.snippet)
}

func newHTTPStatusError(resp *http.Response) *httpStatusError {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet))
	return &httpStatusError{
		code:    resp.StatusCode,
		status:  resp.Status,
		snippet: strings.TrimSpace(string(snippet)),
	}
}

func requireHTTPScheme(uri URI) error {
	if err := uri.Validate(); err != nil {
		return err
	}
	if uri.Scheme != SchemeHTTP && uri.Scheme != SchemeHTTPS {
		return fmt.Errorf("http backend cannot handle %q uris", uri.Scheme)
	}
	return nil
}
