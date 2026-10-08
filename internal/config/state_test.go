// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func validState() *State {
	return &State{
		Version:         StateVersion,
		Template:        "git::https://github.com/org/web.git//react",
		SourceKind:      "remote-git",
		Subpath:         "react",
		RequestedRef:    "^1.2",
		ResolvedCommit:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Answers:         map[string]any{"project_name": "demo"},
		Engine:          "turutan/0.1.0",
		TemplateLicense: TemplateLicenseMIT0,
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Run("save then load preserves state", func(t *testing.T) {
		dir := t.TempDir()
		want := validState()
		if err := SaveState(dir, want); err != nil {
			t.Fatalf("SaveState error: %v", err)
		}
		got, err := LoadState(os.DirFS(dir))
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if got.Template != want.Template || got.ResolvedCommit != want.ResolvedCommit {
			t.Errorf("round trip = %+v, want %+v", got, want)
		}
		if got.TemplateLicense != TemplateLicenseMIT0 {
			t.Errorf("TemplateLicense = %q, want %q", got.TemplateLicense, TemplateLicenseMIT0)
		}
		if got.Answers["project_name"] != "demo" {
			t.Errorf("Answers = %v, want project_name=demo", got.Answers)
		}
	})
	t.Run("filesystem identity accepted", func(t *testing.T) {
		state := validState()
		state.SourceKind = "filesystem"
		state.ResolvedCommit = strings.Repeat("b", 64)
		if err := ValidateState(state); err != nil {
			t.Errorf("ValidateState filesystem error: %v", err)
		}
	})
}

func TestValidateStateFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
	}{
		{name: "future version rejected", mutate: func(s *State) { s.Version = StateVersion + 1 }},
		{name: "empty template rejected", mutate: func(s *State) { s.Template = "" }},
		{name: "unknown kind rejected", mutate: func(s *State) { s.SourceKind = "s3" }},
		{name: "short SHA rejected", mutate: func(s *State) { s.ResolvedCommit = "abc123" }},
		{name: "empty engine rejected", mutate: func(s *State) { s.Engine = "" }},
		{name: "empty license rejected", mutate: func(s *State) { s.TemplateLicense = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := validState()
			tt.mutate(state)
			if err := ValidateState(state); err == nil {
				t.Errorf("ValidateState(%+v) succeeded, want error", state)
			}
		})
	}
	t.Run("missing file errors", func(t *testing.T) {
		if _, err := LoadState(fstest.MapFS{}); err == nil {
			t.Error("LoadState without file succeeded, want error")
		}
	})
}

func TestLockRoundTrip(t *testing.T) {
	t.Run("save sorts files and stamps manifest", func(t *testing.T) {
		dir := t.TempDir()
		lock := &Lock{
			Source:      "git::https://github.com/org/web.git//react",
			Ref:         "^1.2",
			ResolvedSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Subpath:     "react",
			Files: []LockFile{
				FileEntry("b.txt", []byte("b")),
				FileEntry("a.txt", []byte("a")),
			},
		}
		if err := SaveLock(dir, lock); err != nil {
			t.Fatalf("SaveLock error: %v", err)
		}
		if lock.Files[0].Path != "a.txt" || lock.Files[1].Path != "b.txt" {
			t.Errorf("files not sorted after save: %v", lock.Files)
		}
		if !strings.HasPrefix(lock.ManifestHash, "sha256:") {
			t.Errorf("ManifestHash = %q, want sha256: prefix", lock.ManifestHash)
		}
		got, err := LoadLock(os.DirFS(dir))
		if err != nil {
			t.Fatalf("LoadLock error: %v", err)
		}
		if len(got.Files) != 2 || got.Files[0].Path != "a.txt" {
			t.Errorf("lock files = %v, want sorted a.txt first", got.Files)
		}
		if got.ManifestHash != lock.ManifestHash {
			t.Errorf("ManifestHash = %q, want %q", got.ManifestHash, lock.ManifestHash)
		}
	})
	t.Run("unsorted lock rejected", func(t *testing.T) {
		lock := &Lock{
			Source:      "s",
			ResolvedSHA: "x",
			Files:       []LockFile{{Path: "b", SHA256: "y"}, {Path: "a", SHA256: "z"}},
		}
		if err := ValidateLock(lock); err == nil {
			t.Error("ValidateLock with unsorted files succeeded, want error")
		}
	})
	t.Run("manifest hash is deterministic", func(t *testing.T) {
		files := []LockFile{FileEntry("a", []byte("1")), FileEntry("b", []byte("2"))}
		if ComputeManifestHash(files) != ComputeManifestHash([]LockFile{files[1], files[0]}) {
			t.Error("ComputeManifestHash depends on input order, want order-independent")
		}
	})
}

func TestLoadManifest(t *testing.T) {
	t.Run("absent manifest means defaults", func(t *testing.T) {
		got, err := LoadManifest(fstest.MapFS{})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if len(got.Ignore) != 0 || got.MinEngine != "" {
			t.Errorf("default manifest = %+v, want empty", got)
		}
	})
	t.Run("default template manifest parses", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "..", "templates", "default", ManifestFileName))
		if err != nil {
			t.Skipf("default template manifest not found: %v", err)
		}
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if got.MinEngine == "" {
			t.Error("MinEngine empty, want a floor constraint")
		}
		if len(got.Ignore) == 0 {
			t.Error("Ignore empty, want default ignore globs")
		}
	})
	t.Run("bad conflict rejected", func(t *testing.T) {
		fsys := fstest.MapFS{ManifestFileName: {Data: []byte("conflict: merge\n")}}
		if _, err := LoadManifest(fsys); err == nil {
			t.Error("LoadManifest with bad conflict succeeded, want error")
		}
	})
	t.Run("bad min-engine rejected", func(t *testing.T) {
		fsys := fstest.MapFS{ManifestFileName: {Data: []byte("min-engine: \"not a constraint!!!\"\n")}}
		if _, err := LoadManifest(fsys); err == nil {
			t.Error("LoadManifest with bad min-engine succeeded, want error")
		}
	})
}
