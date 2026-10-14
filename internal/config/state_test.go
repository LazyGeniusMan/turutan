// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LazyGeniusMan/turutan/internal/filter"
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

func TestLoadManifestLayeredProfiles(t *testing.T) {
	t.Parallel()
	t.Run("profiles-only carries includes without synthesis", func(t *testing.T) {
		t.Parallel()
		data := []byte("profiles:\n  go:\n    label: Go\n    includes: [\"go.mod\", \"cmd/**\"]\n  github:\n    label: GitHub\n    includes: [\".github/**\"]\n")
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if len(got.Conditionals) != 0 {
			t.Fatalf("conditionals = %d, want 0 (no synthesis)", len(got.Conditionals))
		}
		if got, want := got.Profiles["go"].Includes, []string{"go.mod", "cmd/**"}; !slices.Equal(got, want) {
			t.Errorf("go includes = %v, want %v", got, want)
		}
		if got, want := got.Profiles["github"].Includes, []string{".github/**"}; !slices.Equal(got, want) {
			t.Errorf("github includes = %v, want %v", got, want)
		}
	})
	t.Run("explicit conditionals preserved", func(t *testing.T) {
		t.Parallel()
		data := []byte("conditionals:\n  - if: '{{ not (contains .enabled_profiles \"go\") }}'\n    skip: [\"custom/**\"]\nprofiles:\n  go:\n    label: Go\n    includes: [\"go.mod\"]\n  github:\n    label: GitHub\n    includes: [\".github/**\"]\n")
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if len(got.Conditionals) != 1 {
			t.Fatalf("conditionals = %d, want 1 explicit only", len(got.Conditionals))
		}
		if got.Conditionals[0].If != `{{ not (contains .enabled_profiles "go") }}` {
			t.Errorf("conditionals[0].If = %q, want explicit go conditional kept", got.Conditionals[0].If)
		}
		if !slices.Equal(got.Conditionals[0].Skip, []string{"custom/**"}) {
			t.Errorf("conditionals[0].Skip = %v, want [custom/**]", got.Conditionals[0].Skip)
		}
	})
	t.Run("excludes parsed", func(t *testing.T) {
		t.Parallel()
		data := []byte("profiles:\n  go:\n    label: Go\n    includes: [\"foo/**\"]\n    excludes: [\"foo/bar/**\"]\n")
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if !slices.Equal(got.Profiles["go"].Excludes, []string{"foo/bar/**"}) {
			t.Errorf("go excludes = %v, want [foo/bar/**]", got.Profiles["go"].Excludes)
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

func TestValidateManifestPrompts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prompts map[string]ManifestPrompt
		wantErr bool
	}{
		{name: "empty prompts accepted", prompts: map[string]ManifestPrompt{}, wantErr: false},
		{name: "string type accepted", prompts: map[string]ManifestPrompt{"project_name": {Type: "string"}}, wantErr: false},
		{name: "empty type defaults to string", prompts: map[string]ManifestPrompt{"project_name": {}}, wantErr: false},
		{name: "array type accepted", prompts: map[string]ManifestPrompt{"enabled_profiles": {Type: "array"}}, wantErr: false},
		{name: "bool type accepted", prompts: map[string]ManifestPrompt{"flag": {Type: "bool"}}, wantErr: false},
		{name: "unknown type rejected", prompts: map[string]ManifestPrompt{"x": {Type: "number"}}, wantErr: true},
		{name: "empty option rejected", prompts: map[string]ManifestPrompt{"x": {Type: "array", Options: []string{"a", ""}}}, wantErr: true},
		{name: "bool with options rejected", prompts: map[string]ManifestPrompt{"x": {Type: "bool", Options: []string{"a"}}}, wantErr: true},
		{name: "string with options accepted", prompts: map[string]ManifestPrompt{"x": {Type: "string", Options: []string{"a", "b"}}}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&Manifest{Prompts: tt.prompts})
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	t.Run("empty prompt key rejected", func(t *testing.T) {
		t.Parallel()
		if err := ValidateManifest(&Manifest{Prompts: map[string]ManifestPrompt{"": {Type: "string"}}}); err == nil {
			t.Error("ValidateManifest with empty prompt key succeeded, want error")
		}
	})
}

func TestValidateManifestConditionals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		conds   []ManifestConditional
		wantErr bool
	}{
		{name: "empty accepted", conds: nil, wantErr: false},
		{name: "valid if skip accepted", conds: []ManifestConditional{{If: `{{ contains .enabled_profiles "go" }}`, Skip: []string{"go.mod"}}}, wantErr: false},
		{name: "skip without if rejected", conds: []ManifestConditional{{Skip: []string{"a.txt"}}}, wantErr: true},
		{name: "bad glob rejected", conds: []ManifestConditional{{If: "true", Skip: []string{"/abs"}}}, wantErr: true},
		{name: "dotdot glob rejected", conds: []ManifestConditional{{If: "true", Skip: []string{"../escape"}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&Manifest{Conditionals: tt.conds})
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStateVersionSingle(t *testing.T) {
	t.Parallel()
	t.Run("current version accepted", func(t *testing.T) {
		t.Parallel()
		if err := ValidateState(validState()); err != nil {
			t.Errorf("ValidateState current error: %v", err)
		}
	})
	t.Run("save stamps current version", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := validState()
		state.Version = 0
		if err := SaveState(dir, state); err != nil {
			t.Fatalf("SaveState error: %v", err)
		}
		if state.Version != StateVersion {
			t.Errorf("Version = %d, want %d after save", state.Version, StateVersion)
		}
	})
	t.Run("v0 v2 and future rejected", func(t *testing.T) {
		t.Parallel()
		for _, version := range []int{0, 2, StateVersion + 1} {
			if version == StateVersion {
				continue
			}
			state := validState()
			state.Version = version
			if err := ValidateState(state); err == nil {
				t.Errorf("ValidateState version %d succeeded, want error", version)
			}
		}
	})
}

func TestValidateManifestProfiles(t *testing.T) {
	t.Parallel()
	t.Run("valid profiles accepted", func(t *testing.T) {
		t.Parallel()
		manifest := &Manifest{Profiles: map[string]ManifestProfile{
			"go": {Label: "Go", Includes: []string{"go.mod", "cmd/**"}},
		}}
		if err := ValidateManifest(manifest); err != nil {
			t.Errorf("ValidateManifest error: %v", err)
		}
	})
	t.Run("bad glob rejected", func(t *testing.T) {
		t.Parallel()
		manifest := &Manifest{Profiles: map[string]ManifestProfile{
			"go": {Includes: []string{"/abs"}},
		}}
		if err := ValidateManifest(manifest); err == nil {
			t.Error("ValidateManifest with bad profile glob succeeded, want error")
		}
	})
	t.Run("empty id rejected", func(t *testing.T) {
		t.Parallel()
		manifest := &Manifest{Profiles: map[string]ManifestProfile{
			"": {Includes: []string{"a.txt"}},
		}}
		if err := ValidateManifest(manifest); err == nil {
			t.Error("ValidateManifest with empty profile id succeeded, want error")
		}
	})
	t.Run("includes and excludes accepted", func(t *testing.T) {
		t.Parallel()
		manifest := &Manifest{Profiles: map[string]ManifestProfile{
			"go": {Includes: []string{"foo/**"}, Excludes: []string{"foo/bar/**"}},
		}}
		if err := ValidateManifest(manifest); err != nil {
			t.Errorf("ValidateManifest error: %v", err)
		}
	})
	t.Run("bad excludes rejected", func(t *testing.T) {
		t.Parallel()
		manifest := &Manifest{Profiles: map[string]ManifestProfile{
			"go": {Includes: []string{"foo/**"}, Excludes: []string{"/abs"}},
		}}
		if err := ValidateManifest(manifest); err == nil {
			t.Error("ValidateManifest with bad excludes succeeded, want error")
		}
	})
}

func TestValidateManifestRenames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		renames map[string]string
		wantErr bool
	}{
		{name: "empty accepted", renames: nil, wantErr: false},
		{name: "valid accepted", renames: map[string]string{"old.txt": "new.txt"}, wantErr: false},
		{name: "empty old rejected", renames: map[string]string{"": "new.txt"}, wantErr: true},
		{name: "empty new rejected", renames: map[string]string{"old.txt": ""}, wantErr: true},
		{name: "self-map rejected", renames: map[string]string{"a.txt": "a.txt"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&Manifest{Renames: tt.renames})
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateManifestLicense(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		license string
		files   []string
		wantErr bool
	}{
		{name: "empty accepted", license: "", wantErr: false},
		{name: "MIT-0 accepted", license: "MIT-0", files: []string{"LICENSE"}, wantErr: false},
		{name: "Apache-2.0 accepted", license: "Apache-2.0", wantErr: false},
		{name: "LicenseRef accepted", license: "LicenseRef-Custom-1.0", wantErr: false},
		{name: "blank rejected", license: "   ", wantErr: true},
		{name: "spaces rejected", license: "MIT OR Apache-2.0", wantErr: true},
		{name: "empty entry rejected", files: []string{""}, wantErr: true},
		{name: "absolute rejected", files: []string{"/LICENSE"}, wantErr: true},
		{name: "escape rejected", files: []string{"../LICENSE"}, wantErr: true},
		{name: "template suffix rejected", files: []string{"LICENSE.tmpl"}, wantErr: true},
		{name: "state collision rejected", files: []string{"state.json"}, wantErr: true},
		{name: "lock collision rejected", files: []string{"lock.json"}, wantErr: true},
		{name: "templates collision rejected", files: []string{"templates/foo"}, wantErr: true},
		{name: "duplicate rejected", files: []string{"LICENSE", "LICENSE"}, wantErr: true},
		{name: "nested accepted", files: []string{"licenses/MIT-0.txt"}, wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&Manifest{License: tt.license, LicenseFiles: tt.files})
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateManifestDocFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   []string
		license []string
		wantErr bool
	}{
		{name: "empty accepted", files: nil, wantErr: false},
		{name: "rendered path accepted", files: []string{"README.md"}, wantErr: false},
		{name: "nested accepted", files: []string{"docs/guide.md"}, wantErr: false},
		{name: "empty entry rejected", files: []string{""}, wantErr: true},
		{name: "absolute rejected", files: []string{"/README.md"}, wantErr: true},
		{name: "escape rejected", files: []string{"../README.md"}, wantErr: true},
		{name: "template suffix rejected", files: []string{"README.md.tmpl"}, wantErr: true},
		{name: "state collision rejected", files: []string{"state.json"}, wantErr: true},
		{name: "lock collision rejected", files: []string{"lock.json"}, wantErr: true},
		{name: "templates collision rejected", files: []string{"templates/foo"}, wantErr: true},
		{name: "duplicate rejected", files: []string{"README.md", "README.md"}, wantErr: true},
		{
			name:    "license overlap rejected",
			files:   []string{"LICENSE"},
			license: []string{"LICENSE"},
			wantErr: true,
		},
		{
			name:    "disjoint license accepted",
			files:   []string{"README.md"},
			license: []string{"LICENSE"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&Manifest{LicenseFiles: tt.license, DocFiles: tt.files})
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLockProfileOwner(t *testing.T) {
	t.Parallel()
	t.Run("profile survives round trip", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lock := &Lock{
			Source:      "s",
			ResolvedSHA: strings.Repeat("a", 40),
			Files:       []LockFile{{Path: "go.mod", SHA256: strings.Repeat("c", 64), Profile: "go"}},
		}
		if err := SaveLock(dir, lock); err != nil {
			t.Fatalf("SaveLock error: %v", err)
		}
		got, err := LoadLock(os.DirFS(dir))
		if err != nil {
			t.Fatalf("LoadLock error: %v", err)
		}
		if got.Files[0].Profile != "go" {
			t.Errorf("Profile = %q, want %q", got.Files[0].Profile, "go")
		}
	})
	t.Run("hash ignores profile", func(t *testing.T) {
		t.Parallel()
		plain := []LockFile{FileEntry("a.txt", []byte("a"))}
		owned := []LockFile{{Path: "a.txt", SHA256: plain[0].SHA256, Bytes: plain[0].Bytes, Profile: "go"}}
		if ComputeManifestHash(plain) != ComputeManifestHash(owned) {
			t.Error("ComputeManifestHash changed with profile, want owner excluded")
		}
	})
}

func TestNormalizeProfiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil injects pair", input: nil, want: []string{"core", "custom"}},
		{name: "empty injects pair", input: []string{}, want: []string{"core", "custom"}},
		{name: "core only gains custom last", input: []string{"core"}, want: []string{"core", "custom"}},
		{name: "custom only gains core first", input: []string{"custom"}, want: []string{"core", "custom"}},
		{name: "middle kept in order", input: []string{"go", "ai"}, want: []string{"core", "go", "ai", "custom"}},
		{name: "shuffled core custom reordered", input: []string{"go", "custom", "core", "ai"}, want: []string{"core", "go", "ai", "custom"}},
		{name: "dupes keep first", input: []string{"go", "go", "core", "ai", "custom", "go"}, want: []string{"core", "go", "ai", "custom"}},
		{name: "blanks dropped", input: []string{"", "  ", "go"}, want: []string{"core", "go", "custom"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeProfiles(tt.input); !slices.Equal(got, tt.want) {
				t.Errorf("NormalizeProfiles(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetProfiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		answers map[string]any
		want    []string
	}{
		{name: "nil answers", answers: nil, want: nil},
		{name: "missing key", answers: map[string]any{}, want: nil},
		{name: "string list", answers: map[string]any{"enabled_profiles": "core, go,go"}, want: []string{"core", "go"}},
		{name: "string slice", answers: map[string]any{"enabled_profiles": []string{"core", "go"}}, want: []string{"core", "go"}},
		{name: "any slice", answers: map[string]any{"enabled_profiles": []any{"core", "go"}}, want: []string{"core", "go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := GetProfiles(tt.answers); !slices.Equal(got, tt.want) {
				t.Errorf("GetProfiles = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("stored profiles win over answers", func(t *testing.T) {
		t.Parallel()
		state := validState()
		state.Profiles = []string{"core"}
		state.Answers = map[string]any{"enabled_profiles": []string{"core", "go"}}
		if got := state.GetProfiles(); !slices.Equal(got, []string{"core"}) {
			t.Errorf("GetProfiles = %v, want [core]", got)
		}
	})
	t.Run("nil state", func(t *testing.T) {
		t.Parallel()
		var state *State
		if got := state.GetProfiles(); got != nil {
			t.Errorf("GetProfiles = %v, want nil", got)
		}
	})
}

func TestProfilesForPath(t *testing.T) {
	t.Parallel()
	manifest := &Manifest{Profiles: map[string]ManifestProfile{
		"go":     {Includes: []string{"go.mod", "cmd/**"}},
		"github": {Includes: []string{".github/**"}},
	}}
	t.Run("matches owner", func(t *testing.T) {
		t.Parallel()
		got := ProfilesForPath(manifest, []string{"go", "github"}, "cmd/main.go")
		if !slices.Equal(got, []string{"go"}) {
			t.Errorf("ProfilesForPath = %v, want [go]", got)
		}
	})
	t.Run("nil profiles checks all", func(t *testing.T) {
		t.Parallel()
		got := ProfilesForPath(manifest, nil, ".github/workflows/ci.yml")
		if !slices.Equal(got, []string{"github"}) {
			t.Errorf("ProfilesForPath = %v, want [github]", got)
		}
	})
	t.Run("unowned path", func(t *testing.T) {
		t.Parallel()
		if got := ProfilesForPath(manifest, nil, "README.md"); len(got) != 0 {
			t.Errorf("ProfilesForPath = %v, want empty", got)
		}
	})
	t.Run("nil manifest", func(t *testing.T) {
		t.Parallel()
		if got := ProfilesForPath(nil, nil, "a.txt"); len(got) != 0 {
			t.Errorf("ProfilesForPath = %v, want empty", got)
		}
	})
}

func TestManifestIsIncludedLayered(t *testing.T) {
	t.Parallel()
	manifest := &Manifest{Profiles: map[string]ManifestProfile{
		"a": {Includes: []string{"foo/**"}, Excludes: []string{"foo/bar/**"}},
		"b": {Includes: []string{"foo/bar/baz/**"}, Excludes: []string{"foo/bar/baz/2.md"}},
	}}
	t.Run("later include re-includes after earlier exclude", func(t *testing.T) {
		t.Parallel()
		got, err := manifest.IsIncluded("foo/bar/baz/1.md", []string{"core", "a", "b", "custom"})
		if err != nil {
			t.Fatalf("IsIncluded error: %v", err)
		}
		if !got {
			t.Error("foo/bar/baz/1.md excluded, want included via b after a exclude")
		}
	})
	t.Run("later exclude wins", func(t *testing.T) {
		t.Parallel()
		got, err := manifest.IsIncluded("foo/bar/baz/2.md", []string{"core", "a", "b", "custom"})
		if err != nil {
			t.Fatalf("IsIncluded error: %v", err)
		}
		if got {
			t.Error("foo/bar/baz/2.md included, want excluded via b")
		}
	})
	t.Run("disabled profiles ignored", func(t *testing.T) {
		t.Parallel()
		got, err := manifest.IsIncluded("foo/bar/baz/1.md", []string{"core", "a", "custom"})
		if err != nil {
			t.Fatalf("IsIncluded error: %v", err)
		}
		if got {
			t.Error("foo/bar/baz/1.md included without b, want excluded via a")
		}
	})
	t.Run("unmatched paths are core-always", func(t *testing.T) {
		t.Parallel()
		got, err := manifest.IsIncluded("keep.txt", []string{"core", "custom"})
		if err != nil {
			t.Fatalf("IsIncluded error: %v", err)
		}
		if !got {
			t.Error("keep.txt excluded, want core-always included")
		}
	})
	t.Run("tmpl sources share decision", func(t *testing.T) {
		t.Parallel()
		got, err := manifest.IsIncluded("foo/bar/baz/1.md.tmpl", []string{"core", "a", "b", "custom"})
		if err != nil {
			t.Fatalf("IsIncluded error: %v", err)
		}
		if !got {
			t.Error("foo/bar/baz/1.md.tmpl excluded, want included like its destination")
		}
	})
}

func TestResolveRenames(t *testing.T) {
	t.Parallel()
	t.Run("unmapped returns path", func(t *testing.T) {
		t.Parallel()
		if got := ResolveRenames("a.txt", map[string]string{"b.txt": "c.txt"}); got != "a.txt" {
			t.Errorf("ResolveRenames = %q, want a.txt", got)
		}
	})
	t.Run("follows chains", func(t *testing.T) {
		t.Parallel()
		renames := map[string]string{"a.txt": "b.txt", "b.txt": "c.txt"}
		if got := ResolveRenames("a.txt", renames); got != "c.txt" {
			t.Errorf("ResolveRenames = %q, want c.txt", got)
		}
	})
	t.Run("cycle terminates", func(t *testing.T) {
		t.Parallel()
		renames := map[string]string{"a.txt": "b.txt", "b.txt": "a.txt"}
		got := ResolveRenames("a.txt", renames)
		if got != "a.txt" && got != "b.txt" {
			t.Errorf("ResolveRenames = %q, want cycle guard", got)
		}
	})
}

func TestDiffProfiles(t *testing.T) {
	t.Parallel()
	manifest := &Manifest{
		Prompts: map[string]ManifestPrompt{
			"project_name":     {},
			"enabled_profiles": {Type: "array"},
			"new_key":          {},
		},
		Profiles: map[string]ManifestProfile{
			"go":     {Includes: []string{"go.mod"}},
			"github": {Includes: []string{".github/**"}},
		},
	}
	t.Run("added removed and new prompts", func(t *testing.T) {
		t.Parallel()
		old := map[string]any{
			"project_name":     "demo",
			"enabled_profiles": []string{"core", "go", "dropped"},
		}
		diff := DiffProfiles(old, manifest)
		if !slices.Equal(diff.Added, []string{"github"}) {
			t.Errorf("Added = %v, want [github]", diff.Added)
		}
		if !slices.Equal(diff.Removed, []string{"dropped"}) {
			t.Errorf("Removed = %v, want [dropped]", diff.Removed)
		}
		if !slices.Equal(diff.NewPrompts, []string{"new_key"}) {
			t.Errorf("NewPrompts = %v, want [new_key]", diff.NewPrompts)
		}
	})
	t.Run("custom immune from added and removed", func(t *testing.T) {
		t.Parallel()
		for _, old := range []map[string]any{
			{"enabled_profiles": []string{"core", "go"}},
			{"enabled_profiles": []string{"core", "go", "custom"}},
		} {
			diff := DiffProfiles(old, manifest)
			if slices.Contains(diff.Added, "custom") {
				t.Errorf("Added = %v, want no custom", diff.Added)
			}
			if slices.Contains(diff.Removed, "custom") {
				t.Errorf("Removed = %v, want no custom", diff.Removed)
			}
		}
	})
	t.Run("core never removed", func(t *testing.T) {
		t.Parallel()
		old := map[string]any{"enabled_profiles": []string{"core"}}
		diff := DiffProfiles(old, manifest)
		if slices.Contains(diff.Removed, "core") {
			t.Errorf("Removed = %v, want no core", diff.Removed)
		}
	})
	t.Run("nil manifest", func(t *testing.T) {
		t.Parallel()
		diff := DiffProfiles(map[string]any{"enabled_profiles": []string{"core"}}, nil)
		if len(diff.Added) != 0 || len(diff.Removed) != 0 || len(diff.NewPrompts) != 0 {
			t.Errorf("DiffProfiles = %+v, want empty", diff)
		}
	})
}

func TestDefaultTemplateProfiles(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "default", ManifestFileName))
	if err != nil {
		t.Skipf("default template manifest not found: %v", err)
	}
	got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if len(got.Profiles) != 21 {
		t.Errorf("profiles = %d, want 21 (core implicit)", len(got.Profiles))
	}
	if _, ok := got.Profiles["core"]; ok {
		t.Error("profiles has core, want core implicit only")
	}
	customProfile, ok := got.Profiles["custom"]
	if !ok {
		t.Error("profiles lacks custom, want always-enabled custom")
	} else if !slices.Contains(customProfile.Includes, ".config/fnox.toml") {
		t.Errorf("custom files = %v, want fnox seed", customProfile.Includes)
	}
	goProfile := got.Profiles["go"]
	if !slices.Contains(goProfile.Includes, ".config/mise/config.go.toml") {
		t.Errorf("go files = %v, want go toolchain", goProfile.Includes)
	}
	if slices.Contains(goProfile.Includes, "go.mod") || slices.Contains(goProfile.Includes, "cmd/**") || slices.Contains(goProfile.Includes, "internal/**") {
		t.Errorf("go files = %v, want no source layout opinion", goProfile.Includes)
	}
	if got := ProfilesForPath(got, nil, ".config/mise/config.go.toml"); !slices.Contains(got, "go") {
		t.Errorf("ProfilesForPath config.go.toml = %v, want go", got)
	}
	if got := ProfilesForPath(got, nil, "cmd/main.go"); len(got) != 0 {
		t.Errorf("ProfilesForPath cmd/main.go = %v, want unowned", got)
	}
}

func TestDefaultTemplateOwnership(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "default", ManifestFileName))
	if err != nil {
		t.Skipf("default template manifest not found: %v", err)
	}
	manifest, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	t.Run("profiles carry layered includes without synthesis", func(t *testing.T) {
		t.Parallel()
		if strings.Contains(string(data), "\nconditionals:") {
			t.Error("default manifest declares conditionals, want profiles-only source")
		}
		if len(manifest.Conditionals) != 0 {
			t.Errorf("conditionals = %d, want 0 (no synthesis)", len(manifest.Conditionals))
		}
		for id, profile := range manifest.Profiles {
			if len(profile.Includes) == 0 {
				t.Errorf("profile %q has no includes, want at least one", id)
			}
		}
		owners := ProfilesForPath(manifest, nil, ".config/ai-rulez/domains/yml/.gitkeep")
		slices.Sort(owners)
		if !slices.Equal(owners, []string{"docker", "github", "kubernetes"}) {
			t.Errorf("yml owners = %v, want [docker github kubernetes] (layered OR)", owners)
		}
	})
	t.Run("every file layered-owned or core", func(t *testing.T) {
		t.Parallel()
		coreGlobs := []string{
			".config/hk/core.pkl",
			".config/hk/index.pkl.tmpl",
			".config/mise/config.linux.toml",
			".config/mise/config.toml",
			".config/tombi.toml",
			".config/updatecli/mise-core.yaml",
			".vscode/**",
			"hooks/**",
			".config/miserc.toml.tmpl",
			".config/ai-rulez/config.toml.tmpl",
			".config/ai-rulez/domains/index.md",
			".config/ai-rulez/domains/pkl/.gitkeep",
			".config/hk.pkl",
			".config/hk.pkl.tmpl",
		}
		isCore := func(rel string) bool {
			for _, pattern := range coreGlobs {
				ok, err := filter.Match(pattern, rel)
				if err != nil {
					continue
				}
				if ok {
					return true
				}
			}
			return false
		}
		if owners := ProfilesForPath(manifest, nil, ".config/mise/config.linux.toml"); len(owners) != 0 {
			t.Errorf("config.linux.toml owners = %v, want core (unowned)", owners)
		}
		if !isCore(".config/mise/config.linux.toml") {
			t.Error("config.linux.toml not in core set, want core-always")
		}
		for _, rel := range []string{
			".config/mise/config.custom.toml",
			".config/fnox.toml",
			".config/hk/custom.pkl",
			".gitignore",
		} {
			if owners := ProfilesForPath(manifest, nil, rel); !slices.Equal(owners, []string{"custom"}) {
				t.Errorf("%s owners = %v, want [custom]", rel, owners)
			}
			if isCore(rel) {
				t.Errorf("%s in core set, want custom-owned only", rel)
			}
		}
		root := filepath.Join("..", "..", "templates", "default")
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == ManifestFileName || rel == ManifestFileNameAlt {
				return nil
			}
			owners := ProfilesForPath(manifest, nil, rel)
			licensed := slices.Contains(manifest.LicenseFiles, rel)
			docced := slices.Contains(manifest.DocFiles, strings.TrimSuffix(rel, ".tmpl"))
			core := isCore(rel)
			if core && (len(owners) > 0 || licensed || docced) {
				t.Errorf("core file %q also owned by %v, want exclusive", rel, owners)
				return nil
			}
			if len(owners) == 0 && !core && !licensed && !docced {
				t.Errorf("orphan file %q unowned and not in core set", rel)
				return nil
			}
			if rel == ".config/ai-rulez/domains/yml/.gitkeep" {
				slices.Sort(owners)
				if !slices.Equal(owners, []string{"docker", "github", "kubernetes"}) {
					t.Errorf("yml owners = %v, want layered OR across docker github kubernetes", owners)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking default template: %v", err)
		}
	})
}

func TestStateLockLocations(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 64)
	digest := "sha256:" + strings.Repeat("b", 64)
	stateFile := func(engine string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(
			`{"version":1,"template":"tpl","sourceKind":"filesystem",` +
				`"resolvedCommit":"` + commit + `","engine":"` + engine + `",` +
				`"templateLicense":"MIT-0"}`)}
	}
	lockFile := &fstest.MapFile{Data: []byte(
		`{"source":"tpl","resolved_sha":"` + commit + `",` +
			`"manifest_hash":"` + digest + `","files":[]}`)}
	t.Run("legacy fallback loads", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			legacyStateFileName: stateFile("turutan/legacy"),
			legacyLockFileName:  lockFile,
		}
		state, err := LoadState(fsys)
		if err != nil {
			t.Fatalf("LoadState legacy error: %v", err)
		}
		if state.Engine != "turutan/legacy" {
			t.Errorf("Engine = %q, want legacy fallback", state.Engine)
		}
		if _, err := LoadLock(fsys); err != nil {
			t.Fatalf("LoadLock legacy error: %v", err)
		}
	})
	t.Run("new location wins", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			StateFileName:       stateFile("turutan/new"),
			LockFileName:        lockFile,
			legacyStateFileName: stateFile("turutan/legacy"),
			legacyLockFileName:  lockFile,
		}
		state, err := LoadState(fsys)
		if err != nil {
			t.Fatalf("LoadState error: %v", err)
		}
		if state.Engine != "turutan/new" {
			t.Errorf("Engine = %q, want new location to win", state.Engine)
		}
	})
	t.Run("corrupt new file does not fall back", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			StateFileName:       {Data: []byte("{bad json")},
			legacyStateFileName: stateFile("turutan/legacy"),
		}
		if _, err := LoadState(fsys); err == nil {
			t.Error("LoadState with corrupt new file succeeded, want error")
		}
	})
	t.Run("saves land in new location", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := &State{
			Template: "tpl", SourceKind: "filesystem",
			ResolvedCommit: commit, Engine: "turutan/0.1.0",
			TemplateLicense: TemplateLicenseMIT0,
		}
		if err := SaveState(dir, state); err != nil {
			t.Fatalf("SaveState error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(StateFileName))); err != nil {
			t.Errorf("new state file missing: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, legacyStateFileName)); !os.IsNotExist(err) {
			t.Error("legacy state file written, want new location only")
		}
		if err := SaveLock(dir, &Lock{
			Source: "tpl", ResolvedSHA: commit, ManifestHash: digest,
		}); err != nil {
			t.Fatalf("SaveLock error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(LockFileName))); err != nil {
			t.Errorf("new lock file missing: %v", err)
		}
	})
}
