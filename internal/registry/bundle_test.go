// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func writeFixtureTemplate(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"turutan.yml":            "name: fixture\n",
		"templates/web/main.txt": "hello\n",
		"scripts/run.sh":         "#!/bin/sh\necho hi\n",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "emptydir"), 0o750); err != nil {
		t.Fatal(err)
	}
}

func assertSameTree(t *testing.T, wantDir, gotDir string) {
	t.Helper()
	want := treeSnapshot(t, wantDir)
	got := treeSnapshot(t, gotDir)
	if len(want) != len(got) {
		t.Fatalf("tree sizes differ: want %d files, got %d", len(want), len(got))
	}
	for rel, content := range want {
		other, ok := got[rel]
		if !ok {
			t.Errorf("missing file %q after round-trip", rel)
			continue
		}
		if other != content {
			t.Errorf("file %q content differs after round-trip", rel)
		}
	}
}

func treeSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := collectBundleEntries(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.isDir {
			continue
		}
		data, err := os.ReadFile(entry.full)
		if err != nil {
			t.Fatal(err)
		}
		out[entry.rel] = string(data)
	}
	return out
}

func TestBundleDeterministic(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	first, firstDigest, err := BundleBytes(src)
	if err != nil {
		t.Fatalf("BundleBytes error: %v", err)
	}
	second, secondDigest, err := BundleBytes(src)
	if err != nil {
		t.Fatalf("BundleBytes error: %v", err)
	}
	if firstDigest != secondDigest || !bytes.Equal(first, second) {
		t.Fatalf("bundle is not deterministic")
	}
	if !digestPattern.MatchString(firstDigest) {
		t.Fatalf("digest %q is not sha256:<hex>", firstDigest)
	}
	digest, err := DigestDir(src)
	if err != nil {
		t.Fatalf("DigestDir error: %v", err)
	}
	if digest != firstDigest {
		t.Fatalf("DigestDir = %q, BundleBytes digest = %q", digest, firstDigest)
	}
}

func TestBundleRoundTrip(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	writeFixtureTemplate(t, src)
	digest, err := DigestDir(src)
	if err != nil {
		t.Fatalf("DigestDir error: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	blob, _, err := BundleBytes(src)
	if err != nil {
		t.Fatalf("BundleBytes error: %v", err)
	}
	if err := ExtractBundle(bytes.NewReader(blob), dest); err != nil {
		t.Fatalf("ExtractBundle error: %v", err)
	}
	assertSameTree(t, src, dest)
	if got, err := DigestDir(dest); err != nil || got != digest {
		t.Fatalf("DigestDir(dest) = %q, err = %v; want %q", got, err, digest)
	}
	info, err := os.Stat(filepath.Join(dest, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("stat run.sh: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh lost its executable bit: %v", info.Mode())
	}
	if info, err := os.Stat(filepath.Join(dest, "emptydir")); err != nil || !info.IsDir() {
		t.Fatalf("emptydir missing after round-trip: %v", err)
	}
}

func craftBundle(t *testing.T, name string, typeflag byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: typeflag, Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBundleRejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		entry    string
		typeflag byte
	}{
		{"parent escape", "../evil.txt", tar.TypeReg},
		{"nested escape", "a/../../evil.txt", tar.TypeReg},
		{"absolute", "/tmp/evil.txt", tar.TypeReg},
		{"symlink", "link", tar.TypeSymlink},
		{"hardlink", "hard", tar.TypeLink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dest := t.TempDir()
			blob := craftBundle(t, tt.entry, tt.typeflag)
			if err := ExtractBundle(bytes.NewReader(blob), dest); err == nil {
				t.Fatalf("ExtractBundle(%q) succeeded, want rejection", tt.entry)
			}
		})
	}
}

func TestExtractBundleNotGzip(t *testing.T) {
	t.Parallel()
	if err := ExtractBundle(strings.NewReader("not a bundle"), t.TempDir()); err == nil {
		t.Fatalf("ExtractBundle(plain text) succeeded, want error")
	}
}
