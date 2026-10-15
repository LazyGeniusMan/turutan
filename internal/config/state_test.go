// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func validState() *State {
	return &State{
		Version:         StateVersion,
		Template:        "oci://ghcr.io/acme/templates/web@1.2.0",
		SourceKind:      "registry",
		Registry:        "oci://ghcr.io/acme/templates",
		TemplateURI:     "oci://ghcr.io/acme/web:1.2.0",
		TemplateDigest:  "sha256:" + strings.Repeat("a", 64),
		RequestedRef:    "1.2.0",
		ResolvedCommit:  strings.Repeat("a", 64),
		Data:            map[string]any{"project_name": "demo"},
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
		if got.Data["project_name"] != "demo" {
			t.Errorf("Data = %v, want project_name=demo", got.Data)
		}
	})
	t.Run("sha256 digest form accepted", func(t *testing.T) {
		state := validState()
		state.ResolvedCommit = state.TemplateDigest
		if err := ValidateState(state); err != nil {
			t.Errorf("ValidateState digest error: %v", err)
		}
	})
	t.Run("legacy git kind rejected", func(t *testing.T) {
		state := validState()
		state.SourceKind = "remote-git"
		if err := ValidateState(state); err == nil {
			t.Error("ValidateState remote-git succeeded, want removal error")
		}
	})
	t.Run("legacy filesystem kind rejected", func(t *testing.T) {
		state := validState()
		state.SourceKind = "filesystem"
		if err := ValidateState(state); err == nil {
			t.Error("ValidateState filesystem succeeded, want removal error")
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
		t.Errorf("state perms = %04o, want 0600 (data may hold secrets)", perm)
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
		{name: "empty registry rejected", mutate: func(s *State) { s.Registry = "" }},
		{name: "empty templateURI rejected", mutate: func(s *State) { s.TemplateURI = "" }},
		{name: "bad templateDigest rejected", mutate: func(s *State) { s.TemplateDigest = "md5:abc" }},
		{name: "digest mismatch rejected", mutate: func(s *State) { s.ResolvedCommit = strings.Repeat("b", 64) }},
		{name: "empty requestedRef rejected", mutate: func(s *State) { s.RequestedRef = "" }},
		{name: "bad requestedRef rejected", mutate: func(s *State) { s.RequestedRef = "not-a-version" }},
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
		Source:       "oci://ghcr.io/acme/templates/web@1.2.0",
		Ref:          "1.2.0",
		ResolvedSHA:  strings.Repeat("a", 64),
		Registry:     "oci://ghcr.io/acme/templates",
		URI:          "oci://ghcr.io/acme/web:1.2.0",
		Digest:       "sha256:" + strings.Repeat("a", 64),
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
	t.Run("sha256 digest form accepted", func(t *testing.T) {
		lock := validLock()
		lock.ResolvedSHA = lock.Digest
		if err := ValidateLock(lock); err != nil {
			t.Errorf("ValidateLock error: %v", err)
		}
	})
	t.Run("empty registry uri rejected", func(t *testing.T) {
		lock := validLock()
		lock.URI = ""
		if err := ValidateLock(lock); err == nil {
			t.Error("ValidateLock with empty uri succeeded, want error")
		}
	})
	t.Run("bad registry digest rejected", func(t *testing.T) {
		lock := validLock()
		lock.Digest = "md5:abc"
		if err := ValidateLock(lock); err == nil {
			t.Error("ValidateLock with bad digest succeeded, want error")
		}
	})
	t.Run("empty registry ref rejected", func(t *testing.T) {
		lock := validLock()
		lock.Ref = ""
		if err := ValidateLock(lock); err == nil {
			t.Error("ValidateLock with empty ref succeeded, want error")
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
		if len(got.Excludes) != 0 || got.MinEngine != "" {
			t.Errorf("default manifest = %+v, want empty", got)
		}
	})
	t.Run("registry template manifest parses", func(t *testing.T) {
		data := []byte("source: \"testreg/test@0.1.0\"\nmin-engine: \">=0.1.0\"\nlicense: MIT-0\n" +
			"excludes:\n  - \"gen/**\"\nconflict: inline\n")
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if got.MinEngine == "" {
			t.Error("MinEngine empty, want a floor constraint")
		}
		if len(got.Excludes) == 0 {
			t.Error("Excludes empty, want excludes globs")
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

func TestLoadManifestConditionals(t *testing.T) {
	t.Parallel()
	t.Run("if includes excludes preserves parsed", func(t *testing.T) {
		t.Parallel()
		data := []byte("conditionals:\n  - if: '{{ contains .profiles \"go\" }}'\n    includes: [\"go.mod\", \"cmd/**\"]\n    excludes: [\"foo/bar/**\"]\n    preserves: [\"keep.local\"]\n")
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if len(got.Conditionals) != 1 {
			t.Fatalf("conditionals = %d, want 1", len(got.Conditionals))
		}
		cond := got.Conditionals[0]
		if !slices.Equal(cond.Includes, []string{"go.mod", "cmd/**"}) {
			t.Errorf("includes = %v, want go set", cond.Includes)
		}
		if !slices.Equal(cond.Excludes, []string{"foo/bar/**"}) {
			t.Errorf("excludes = %v, want [foo/bar/**]", cond.Excludes)
		}
		if !slices.Equal(cond.Preserves, []string{"keep.local"}) {
			t.Errorf("preserves = %v, want [keep.local]", cond.Preserves)
		}
	})
	t.Run("legacy profiles key rejected", func(t *testing.T) {
		t.Parallel()
		data := []byte("profiles:\n  go:\n    label: Go\n    includes: [\"go.mod\"]\n")
		if _, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}}); err == nil {
			t.Error("LoadManifest with profiles key succeeded, want error (breaking change)")
		}
	})
	t.Run("legacy ignore preserve keys rejected", func(t *testing.T) {
		t.Parallel()
		for _, key := range []string{"ignore", "preserve"} {
			data := []byte(key + ": [\"a.txt\"]\n")
			if _, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}}); err == nil {
				t.Errorf("LoadManifest with %s key succeeded, want error (breaking change)", key)
			}
		}
	})
	t.Run("excludes and preserves parsed", func(t *testing.T) {
		t.Parallel()
		data := []byte("excludes: [\"a.txt\"]\npreserves: [\"keep.txt\"]\n")
		got, err := LoadManifest(fstest.MapFS{ManifestFileName: {Data: data}})
		if err != nil {
			t.Fatalf("LoadManifest error: %v", err)
		}
		if !slices.Equal(got.Excludes, []string{"a.txt"}) {
			t.Errorf("excludes = %v, want [a.txt]", got.Excludes)
		}
		if !slices.Equal(got.Preserves, []string{"keep.txt"}) {
			t.Errorf("preserves = %v, want [keep.txt]", got.Preserves)
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

func TestValidateManifestInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inputs  map[string]ManifestInput
		wantErr bool
	}{
		{name: "empty inputs accepted", inputs: map[string]ManifestInput{}, wantErr: false},
		{name: "string type accepted", inputs: map[string]ManifestInput{"project_name": {Label: "Project Name", Type: "string"}}, wantErr: false},
		{name: "empty type defaults to string", inputs: map[string]ManifestInput{"project_name": {Label: "Project Name"}}, wantErr: false},
		{name: "array type accepted", inputs: map[string]ManifestInput{"tags": {Label: "Tags", Type: "array"}}, wantErr: false},
		{name: "bool type accepted", inputs: map[string]ManifestInput{"flag": {Label: "Flag", Type: "bool"}}, wantErr: false},
		{name: "unknown type rejected", inputs: map[string]ManifestInput{"x": {Label: "X", Type: "number"}}, wantErr: true},
		{name: "empty option rejected", inputs: map[string]ManifestInput{"x": {Label: "X", Type: "array", Options: []string{"a", ""}}}, wantErr: true},
		{name: "bool with options rejected", inputs: map[string]ManifestInput{"x": {Label: "X", Type: "bool", Options: []string{"a"}}}, wantErr: true},
		{name: "string with options accepted", inputs: map[string]ManifestInput{"x": {Label: "X", Type: "string", Options: []string{"a", "b"}}}, wantErr: false},
		{name: "missing label rejected", inputs: map[string]ManifestInput{"x": {Type: "string"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&Manifest{Inputs: tt.inputs})
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
	t.Run("empty input key rejected", func(t *testing.T) {
		t.Parallel()
		if err := ValidateManifest(&Manifest{Inputs: map[string]ManifestInput{"": {Type: "string"}}}); err == nil {
			t.Error("ValidateManifest with empty input key succeeded, want error")
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
		{name: "valid excludes accepted", conds: []ManifestConditional{{If: `{{ contains .profiles "go" }}`, Excludes: []string{"go.mod"}}}, wantErr: false},
		{name: "valid includes accepted", conds: []ManifestConditional{{If: "true", Includes: []string{"foo/**"}}}, wantErr: false},
		{name: "valid includes plus excludes accepted", conds: []ManifestConditional{{If: "true", Includes: []string{"foo/**"}, Excludes: []string{"foo/bar/**"}}}, wantErr: false},
		{name: "missing if rejected", conds: []ManifestConditional{{Excludes: []string{"a.txt"}}}, wantErr: true},
		{name: "blank if rejected", conds: []ManifestConditional{{If: "  ", Excludes: []string{"a.txt"}}}, wantErr: true},
		{name: "neither includes nor excludes nor preserves rejected", conds: []ManifestConditional{{If: "true"}}, wantErr: true},
		{name: "preserves-only accepted", conds: []ManifestConditional{{If: "true", Preserves: []string{"keep.txt"}}}, wantErr: false},
		{name: "bad preserves glob rejected", conds: []ManifestConditional{{If: "true", Preserves: []string{"/abs"}}}, wantErr: true},
		{name: "bad includes glob rejected", conds: []ManifestConditional{{If: "true", Includes: []string{"/abs"}}}, wantErr: true},
		{name: "bad excludes glob rejected", conds: []ManifestConditional{{If: "true", Excludes: []string{"/abs"}}}, wantErr: true},
		{name: "dotdot includes rejected", conds: []ManifestConditional{{If: "true", Includes: []string{"../escape"}}}, wantErr: true},
		{name: "dotdot excludes rejected", conds: []ManifestConditional{{If: "true", Excludes: []string{"../escape"}}}, wantErr: true},
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

func TestValidateManifestRootGlobs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		manifest Manifest
		wantErr  bool
	}{
		{name: "empty accepted", manifest: Manifest{}, wantErr: false},
		{name: "valid includes accepted", manifest: Manifest{Includes: []string{"a.txt", "gen/**"}}, wantErr: false},
		{name: "valid excludes accepted", manifest: Manifest{Excludes: []string{"a.txt", "gen/**"}}, wantErr: false},
		{name: "valid preserves accepted", manifest: Manifest{Preserves: []string{"keep.txt"}}, wantErr: false},
		{name: "bad includes rejected", manifest: Manifest{Includes: []string{"/abs"}}, wantErr: true},
		{name: "bad excludes rejected", manifest: Manifest{Excludes: []string{"/abs"}}, wantErr: true},
		{name: "bad preserves rejected", manifest: Manifest{Preserves: []string{"../escape"}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateManifest(&tt.manifest)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateManifest error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
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
		{name: "state collision rejected", files: []string{"state.yml"}, wantErr: true},
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
		{name: "state collision rejected", files: []string{"state.yml"}, wantErr: true},
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

func TestDiffInputs(t *testing.T) {
	t.Parallel()
	manifest := &Manifest{
		Inputs: map[string]ManifestInput{
			"project_name": {},
			"ai_agents":    {Default: []string{"pi"}, If: `{{contains .profiles "ai"}}`},
			"new_key":      {},
		},
	}
	t.Run("unanswered reported", func(t *testing.T) {
		t.Parallel()
		oldData := map[string]any{"project_name": "demo", "profiles": []string{"go"}}
		if got, want := DiffInputs(oldData, manifest), []string{"new_key"}; !slices.Equal(got, want) {
			t.Errorf("DiffInputs = %v, want %v", got, want)
		}
	})
	t.Run("conditional input gated by profiles data", func(t *testing.T) {
		t.Parallel()
		withAI := map[string]any{"project_name": "demo", "profiles": []string{"ai"}}
		if got := DiffInputs(withAI, manifest); !slices.Equal(got, []string{"ai_agents", "new_key"}) {
			t.Errorf("DiffInputs with ai = %v, want [ai_agents new_key]", got)
		}
		withoutAI := map[string]any{"project_name": "demo", "profiles": []string{"go"}}
		if got := DiffInputs(withoutAI, manifest); slices.Contains(got, "ai_agents") {
			t.Errorf("DiffInputs without ai = %v, want no ai_agents", got)
		}
	})
	t.Run("nil manifest", func(t *testing.T) {
		t.Parallel()
		if got := DiffInputs(map[string]any{}, nil); len(got) != 0 {
			t.Errorf("DiffInputs = %v, want empty", got)
		}
	})
}

func TestStateLockLocations(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 64)
	digest := "sha256:" + commit
	stateFile := func(engine string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(
			"version: 1\ntemplate: oci://ghcr.io/acme/templates/web@1.2.0\nsourceKind: registry\n" +
				"registry: oci://ghcr.io/acme/templates\ntemplateURI: oci://ghcr.io/acme/web:1.2.0\n" +
				"templateDigest: " + digest + "\nrequestedRef: 1.2.0\n" +
				"resolvedCommit: " + commit + "\nengine: " + engine + "\n" +
				"templateLicense: MIT-0\n")}
	}
	lockFile := &fstest.MapFile{Data: []byte(
		`{"source":"oci://ghcr.io/acme/templates/web@1.2.0","ref":"1.2.0","resolved_sha":"` + commit + `",` +
			`"registry":"oci://ghcr.io/acme/templates","uri":"oci://ghcr.io/acme/web:1.2.0",` +
			`"digest":"` + digest + `","manifest_hash":"` + digest + `","files":[]}`)}
	t.Run("yaml state loads", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			StateFileName: stateFile("turutan/yaml"),
			LockFileName:  lockFile,
		}
		state, err := LoadState(fsys)
		if err != nil {
			t.Fatalf("LoadState yaml error: %v", err)
		}
		if state.Engine != "turutan/yaml" {
			t.Errorf("Engine = %q, want yaml state", state.Engine)
		}
		if _, err := LoadLock(fsys); err != nil {
			t.Fatalf("LoadLock error: %v", err)
		}
	})
	t.Run("missing state errors", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			LockFileName: lockFile,
		}
		if _, err := LoadState(fsys); err == nil {
			t.Error("LoadState without file succeeded, want error")
		}
	})
	t.Run("corrupt state errors", func(t *testing.T) {
		t.Parallel()
		fsys := fstest.MapFS{
			StateFileName: {Data: []byte(":\n\tbad yaml [unclosed")},
		}
		if _, err := LoadState(fsys); err == nil {
			t.Error("LoadState with corrupt file succeeded, want error")
		}
	})
	t.Run("saves land in yaml location", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		state := &State{
			Template: "oci://ghcr.io/acme/templates/web@1.2.0", SourceKind: "registry",
			Registry: "oci://ghcr.io/acme/templates", TemplateURI: "oci://ghcr.io/acme/web:1.2.0",
			TemplateDigest: digest, RequestedRef: "1.2.0",
			ResolvedCommit: commit, Engine: "turutan/0.1.0",
			TemplateLicense: TemplateLicenseMIT0,
		}
		if err := SaveState(dir, state); err != nil {
			t.Fatalf("SaveState error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(StateFileName))); err != nil {
			t.Errorf("new state file missing: %v", err)
		}
		oldJSON := strings.Replace(StateFileName, ".yml", ".json", 1)
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(oldJSON))); !os.IsNotExist(err) {
			t.Error("json state file written, want yaml location only")
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(StateFileName))) // #nosec G304 -- test-only fixture path under t.TempDir(), not external input
		if err != nil {
			t.Fatalf("reading saved state: %v", err)
		}
		if !strings.Contains(string(data), "template: oci://ghcr.io/acme/templates/web@1.2.0") {
			t.Errorf("saved state = %q, want yaml with template key", data)
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
