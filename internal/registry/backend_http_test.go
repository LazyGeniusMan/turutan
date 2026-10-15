// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type httpBundleServer struct {
	mu       sync.Mutex
	stored   []byte
	authSeen []string
	putCode  int
	postCode int
}

func (s *httpBundleServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/bundle", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if auth := r.Header.Get("Authorization"); auth != "" {
			s.authSeen = append(s.authSeen, auth)
		}
		switch r.Method {
		case http.MethodPut:
			if s.putCode != 0 {
				w.WriteHeader(s.putCode)
				return
			}
			s.store(w, r)
		case http.MethodPost:
			if s.postCode != 0 {
				w.WriteHeader(s.postCode)
				return
			}
			s.store(w, r)
		case http.MethodGet:
			if s.stored == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", BundleMediaType)
			_, _ = w.Write(s.stored)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return mux
}

func (s *httpBundleServer) store(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.stored = body
	w.WriteHeader(http.StatusOK)
}

func httpURIFor(t *testing.T, server *httptest.Server) URI {
	t.Helper()
	uri, err := ParseURI(server.URL + "/bundle")
	if err != nil {
		t.Fatalf("ParseURI error: %v", err)
	}
	return uri
}

func TestHTTPBackendRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	server := &httpBundleServer{}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()

	src := t.TempDir()
	writeFixtureTemplate(t, src)
	uri := httpURIFor(t, httpServer)

	backend := NewHTTPBackend(nil, SchemeHTTP)
	digest, err := backend.Bundle(ctx, src, uri, AuthConfig{Kind: AuthKindToken, Token: "tok"})
	if err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	if len(server.authSeen) == 0 || server.authSeen[0] != "Bearer tok" {
		t.Fatalf("server saw auth headers %q, want Bearer token", server.authSeen)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := backend.Fetch(ctx, uri, AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	assertSameTree(t, src, dest)
	if got, err := DigestDir(dest); err != nil || got != digest {
		t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, digest)
	}
}

func TestHTTPBackendBasicAuth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	server := &httpBundleServer{}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()

	src := t.TempDir()
	writeFixtureTemplate(t, src)
	backend := NewHTTPBackend(nil, SchemeHTTP)
	uri := httpURIFor(t, httpServer)
	auth := AuthConfig{Kind: AuthKindBasic, Username: "alice", Password: "pw"}
	if _, err := backend.Bundle(ctx, src, uri, auth); err != nil {
		t.Fatalf("Bundle error: %v", err)
	}
	if len(server.authSeen) == 0 || !strings.HasPrefix(server.authSeen[0], "Basic ") {
		t.Fatalf("server saw auth headers %q, want Basic", server.authSeen)
	}
}

func TestHTTPBackendPostFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	server := &httpBundleServer{putCode: http.StatusMethodNotAllowed}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()

	src := t.TempDir()
	writeFixtureTemplate(t, src)
	backend := NewHTTPBackend(nil, SchemeHTTP)
	if _, err := backend.Bundle(ctx, src, httpURIFor(t, httpServer), AuthConfig{}); err != nil {
		t.Fatalf("Bundle with PUT fallback error: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := backend.Fetch(ctx, httpURIFor(t, httpServer), AuthConfig{}, dest); err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	assertSameTree(t, src, dest)
}

func TestHTTPBackendPushUnsupported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	server := &httpBundleServer{putCode: http.StatusMethodNotAllowed, postCode: http.StatusMethodNotAllowed}
	httpServer := httptest.NewServer(server.handler())
	defer httpServer.Close()

	src := t.TempDir()
	writeFixtureTemplate(t, src)
	backend := NewHTTPBackend(nil, SchemeHTTP)
	_, err := backend.Bundle(ctx, src, httpURIFor(t, httpServer), AuthConfig{})
	if err == nil {
		t.Fatalf("Bundle succeeded against a push-less server, want error")
	}
	if !strings.Contains(err.Error(), "PUT and POST rejected") {
		t.Fatalf("error %q does not explain push is unsupported", err)
	}
}

func TestHTTPBackendFetchNotGzip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not a bundle"))
	}))
	defer httpServer.Close()

	backend := NewHTTPBackend(nil, SchemeHTTP)
	uri := httpURIFor(t, httpServer)
	if err := backend.Fetch(ctx, uri, AuthConfig{}, filepath.Join(t.TempDir(), "dest")); err == nil {
		t.Fatalf("Fetch of plain text succeeded, want error")
	}
}
