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
	t.Parallel()
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

func TestSaveStatePerms0600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SaveState(dir, validState()); err != nil {
		t.Fatalf("SaveState error: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, StateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state perms = %04o, want 0600 (answers may hold secrets)", perm)
	}
}

func TestSaveLockPerms0600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lock := &Lock{
		Source:      "s",
		ResolvedSHA: strings.Repeat("a", 40),
		Files:       []LockFile{FileEntry("a.txt", []byte("a"))},
	}
	if err := SaveLock(dir, lock); err != nil {
		t.Fatalf("SaveLock error: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, LockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("lock perms = %04o, want 0600", perm)
	}
}

func TestValidateStateFailures(t *testing.T) {
	t.Parallel()
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
		{name: "unknown conflict rejected", mutate: func(s *State) { s.Conflict = "merge" }},
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
	t.Run("non-MIT-0 license accepted", func(t *testing.T) {
		state := validState()
		state.TemplateLicense = "Apache-2.0"
		if err := ValidateState(state); err != nil {
			t.Errorf("ValidateState with Apache-2.0 error: %v", err)
		}
	})
	t.Run("missing file errors", func(t *testing.T) {
		if _, err := LoadState(fstest.MapFS{}); err == nil {
			t.Error("LoadState without file succeeded, want error")
		}
	})
}

func TestLockRoundTrip(t *testing.T) {
	t.Parallel()
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
			Source:       "s",
			ResolvedSHA:  strings.Repeat("a", 40),
			ManifestHash: "sha256:" + strings.Repeat("b", 64),
			Files: []LockFile{
				{Path: "b", SHA256: strings.Repeat("c", 64)},
				{Path: "a", SHA256: strings.Repeat("d", 64)},
			},
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

func validLock() *Lock {
	return &Lock{
		Source:       "git::https://github.com/org/web.git//react",
		ResolvedSHA:  strings.Repeat("a", 40),
		ManifestHash: "sha256:" + strings.Repeat("b", 64),
		Files: []LockFile{
			{Path: "a.txt", SHA256: strings.Repeat("c", 64), Bytes: 1},
			{Path: "b.txt", SHA256: strings.Repeat("d", 64), Bytes: 0},
		},
	}
}

func TestValidateLockShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Lock)
	}{
		{name: "empty resolved_sha rejected", mutate: func(l *Lock) { l.ResolvedSHA = "" }},
		{name: "short resolved_sha rejected", mutate: func(l *Lock) { l.ResolvedSHA = "abc123" }},
		{name: "non-hex resolved_sha rejected", mutate: func(l *Lock) { l.ResolvedSHA = strings.Repeat("z", 40) }},
		{name: "41-hex resolved_sha rejected", mutate: func(l *Lock) { l.ResolvedSHA = strings.Repeat("a", 41) }},
		{name: "empty manifest_hash rejected", mutate: func(l *Lock) { l.ManifestHash = "" }},
		{name: "manifest_hash without prefix rejected", mutate: func(l *Lock) { l.ManifestHash = strings.Repeat("b", 64) }},
		{name: "short manifest_hash digest rejected", mutate: func(l *Lock) { l.ManifestHash = "sha256:abc" }},
		{name: "empty file path rejected", mutate: func(l *Lock) { l.Files[0].Path = "" }},
		{name: "empty file sha256 rejected", mutate: func(l *Lock) { l.Files[0].SHA256 = "" }},
		{name: "non-hex file sha256 rejected", mutate: func(l *Lock) { l.Files[0].SHA256 = strings.Repeat("z", 64) }},
		{name: "40-hex file sha256 rejected", mutate: func(l *Lock) { l.Files[0].SHA256 = strings.Repeat("a", 40) }},
		{name: "negative bytes rejected", mutate: func(l *Lock) { l.Files[0].Bytes = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := validLock()
			tt.mutate(lock)
			if err := ValidateLock(lock); err == nil {
				t.Errorf("ValidateLock(%+v) succeeded, want error", lock)
			}
		})
	}
	t.Run("64-hex filesystem identity accepted", func(t *testing.T) {
		lock := validLock()
		lock.ResolvedSHA = strings.Repeat("e", 64)
		if err := ValidateLock(lock); err != nil {
			t.Errorf("ValidateLock error: %v", err)
		}
	})
	t.Run("saved lock passes shape validation", func(t *testing.T) {
		dir := t.TempDir()
		lock := &Lock{
			Source:      "git::https://github.com/org/web.git//react",
			ResolvedSHA: strings.Repeat("a", 40),
			Files:       []LockFile{FileEntry("a.txt", []byte("a"))},
		}
		if err := SaveLock(dir, lock); err != nil {
			t.Fatalf("SaveLock error: %v", err)
		}
		if err := ValidateLock(lock); err != nil {
			t.Errorf("ValidateLock error: %v", err)
		}
	})
}

func TestLoadManifest(t *testing.T) {
	t.Parallel()
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

func TestValidateManifestFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		from    string
		wantErr bool
	}{
		{name: "empty from always applies", from: "", wantErr: false},
		{name: "semver range passes", from: "<2.0.0", wantErr: false},
		{name: "semver gte range passes", from: ">=1.0.0", wantErr: false},
		{name: "hex prefix passes", from: "abc1234", wantErr: false},
		{name: "full commit SHA passes", from: strings.Repeat("a", 40), wantErr: false},
		{name: "typo fails fast", from: "not-a-range!!!", wantErr: true},
		{name: "short hex fails", from: "abc123", wantErr: true},
		{name: "non-hex word fails", from: "release-candidate", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := &Manifest{Migrations: []ManifestMigration{{From: tt.from, Run: []string{"echo hi"}}}}
			err := ValidateManifest(manifest)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest(From=%q) error = %v, wantErr %v", tt.from, err, tt.wantErr)
			}
		})
	}
	t.Run("bad from fails at load", func(t *testing.T) {
		fsys := fstest.MapFS{ManifestFileName: {Data: []byte("migrations:\n  - from: \"not-a-range!!!\"\n    run: [\"echo hi\"]\n")}}
		if _, err := LoadManifest(fsys); err == nil {
			t.Error("LoadManifest with bad migration from succeeded, want error")
		}
	})
	t.Run("empty and valid from load", func(t *testing.T) {
		data := []byte("migrations:\n  - run: [\"echo hi\"]\n  - from: \"<2.0.0\"\n    run: [\"echo yo\"]\n")
		if _, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}}); err != nil {
			t.Errorf("LoadManifest error: %v", err)
		}
	})
	t.Run("empty run rejected", func(t *testing.T) {
		manifest := &Manifest{Migrations: []ManifestMigration{{Run: []string{}}}}
		if err := ValidateManifest(manifest); err == nil {
			t.Error("ValidateManifest with empty run succeeded, want error")
		}
	})
	t.Run("nil run rejected", func(t *testing.T) {
		manifest := &Manifest{Migrations: []ManifestMigration{{}}}
		if err := ValidateManifest(manifest); err == nil {
			t.Error("ValidateManifest with missing run succeeded, want error")
		}
	})
	t.Run("empty run fails at load", func(t *testing.T) {
		fsys := fstest.MapFS{ManifestFileName: {Data: []byte("migrations:\n  - run: []\n")}}
		if _, err := LoadManifest(fsys); err == nil {
			t.Error("LoadManifest with empty run succeeded, want error")
		}
	})
}
