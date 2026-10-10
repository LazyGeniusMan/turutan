// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"

	"github.com/LazyGeniusMan/turutan/internal/config"
)

func diffOptions() DiffOptions {
	return DiffOptions{}
}

func TestComputeDiff(t *testing.T) {
	t.Run("no drift on clean bootstrap", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 0 {
			t.Errorf("diffs = %+v, want none on clean bootstrap", diffs)
		}
	})
	t.Run("local edit shows drift with counts", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "aaa\nbbb\nccc\nddd\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("aaa\nBBB\nccc\nddd\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 1 {
			t.Fatalf("diffs = %d, want 1", len(diffs))
		}
		diff := diffs[0]
		if diff.Path != "hello.txt" {
			t.Errorf("Path = %q, want hello.txt", diff.Path)
		}
		if diff.Added != 1 || diff.Removed != 1 {
			t.Errorf("Added/Removed = %d/%d, want 1/1", diff.Added, diff.Removed)
		}
		for _, want := range []string{"@@", "a/hello.txt", "b/hello.txt", "-BBB", "+bbb"} {
			if !strings.Contains(diff.Unified, want) {
				t.Errorf("Unified missing %q:\n%s", want, diff.Unified)
			}
		}
	})
	t.Run("new template commit shows drift", func(t *testing.T) {
		repoDir, _ := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, repoDir)
		updateTemplateFile(t, repoDir, "hello.txt", "v2\n")
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 1 {
			t.Fatalf("diffs = %d, want 1", len(diffs))
		}
		if !strings.Contains(diffs[0].Unified, "+v2") {
			t.Errorf("Unified missing new template line:\n%s", diffs[0].Unified)
		}
	})
	t.Run("ref selects tagged template", func(t *testing.T) {
		repoDir, old := initTemplateRepo(t, map[string]string{"hello.txt": "v1\n"})
		repo, err := gitpkg.PlainOpen(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreateTag("v1", plumbing.NewHash(old), nil); err != nil {
			t.Fatal(err)
		}
		projectDir := bootstrapProject(t, repoDir)
		updateTemplateFile(t, repoDir, "hello.txt", "v2\n")
		opts := diffOptions()
		opts.Ref = "v1"
		diffs, err := ComputeDiff(context.Background(), projectDir, opts)
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 0 {
			t.Errorf("diffs = %+v, want none when --ref selects the bootstrapped tag", diffs)
		}
	})
	t.Run("deleted project file shows as new", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.Remove(filepath.Join(projectDir, "hello.txt")); err != nil {
			t.Fatal(err)
		}
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 1 {
			t.Fatalf("diffs = %d, want 1", len(diffs))
		}
		if !diffs[0].IsNew {
			t.Error("IsNew = false, want true for a file missing from the project")
		}
		if diffs[0].Added == 0 {
			t.Error("Added = 0, want the restored lines counted")
		}
	})
	t.Run("dropped template file shows as deleted", func(t *testing.T) {
		repoDir, _ := initTemplateRepo(t, map[string]string{"gone.txt": "bye\n", "stay.txt": "hi\n"})
		projectDir := bootstrapProject(t, repoDir)
		if err := os.Remove(filepath.Join(repoDir, "gone.txt")); err != nil {
			t.Fatal(err)
		}
		commitAll(t, repoDir, "drop gone.txt")
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 1 {
			t.Fatalf("diffs = %+v, want exactly the dropped file", diffs)
		}
		diff := diffs[0]
		if diff.Path != "gone.txt" || !diff.IsDeleted {
			t.Errorf("diff = %+v, want deleted gone.txt", diff)
		}
		if diff.Removed == 0 {
			t.Error("Removed = 0, want the stale lines counted")
		}
	})
	t.Run("ignored paths are skipped", func(t *testing.T) {
		src := makeTemplate(t, "ignore:\n  - \"gen/**\"\n", map[string]string{"gen/out.txt": "v1\n", "hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 1 || diffs[0].Path != "hello.txt" {
			t.Errorf("diffs = %+v, want only hello.txt", diffs)
		}
	})
	t.Run("missing state errors", func(t *testing.T) {
		if _, err := ComputeDiff(context.Background(), t.TempDir(), diffOptions()); err == nil {
			t.Error("ComputeDiff without state succeeded, want error")
		}
	})
	t.Run("missing lock errors", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.Remove(filepath.Join(projectDir, config.LockFileName)); err != nil {
			t.Fatal(err)
		}
		if _, err := ComputeDiff(context.Background(), projectDir, diffOptions()); err == nil {
			t.Error("ComputeDiff without lock succeeded, want error")
		}
	})
	t.Run("no state mutation", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(projectDir, "hello.txt"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		beforeState, err := os.ReadFile(filepath.Join(projectDir, config.StateFileName))
		if err != nil {
			t.Fatal(err)
		}
		beforeLock, err := os.ReadFile(filepath.Join(projectDir, config.LockFileName))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ComputeDiff(context.Background(), projectDir, diffOptions()); err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		afterState, err := os.ReadFile(filepath.Join(projectDir, config.StateFileName))
		if err != nil {
			t.Fatal(err)
		}
		afterLock, err := os.ReadFile(filepath.Join(projectDir, config.LockFileName))
		if err != nil {
			t.Fatal(err)
		}
		if string(afterState) != string(beforeState) {
			t.Error("ComputeDiff mutated .turutan.json")
		}
		if string(afterLock) != string(beforeLock) {
			t.Error("ComputeDiff mutated .turutan.lock")
		}
	})
	t.Run("drift sorted by path", func(t *testing.T) {
		src := makeTemplate(t, "", map[string]string{"b.txt": "1\n", "a.txt": "1\n"})
		projectDir := bootstrapProject(t, src)
		for _, name := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(projectDir, name), []byte("2\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		diffs, err := ComputeDiff(context.Background(), projectDir, diffOptions())
		if err != nil {
			t.Fatalf("ComputeDiff error: %v", err)
		}
		if len(diffs) != 2 || diffs[0].Path != "a.txt" || diffs[1].Path != "b.txt" {
			t.Errorf("diffs = %+v, want path-sorted a.txt, b.txt", diffs)
		}
	})
}

func TestComputeDiffMinEngine(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		src := makeTemplate(t, "min-engine: \">=0.1.0\"\n", map[string]string{"hello.txt": "v1\n"})
		projectDir := bootstrapProject(t, src)
		if err := os.WriteFile(filepath.Join(src, ".turutan.yml"), []byte("min-engine: \">=99.0.0\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return src, projectDir
	}
	tests := []struct {
		name    string
		engine  string
		wantErr bool
		errPart string
	}{
		{name: "unsatisfiable floor fails fast", engine: "0.1.0", wantErr: true, errPart: "requires engine"},
		{name: "dev version skips gate", engine: "dev", wantErr: false},
		{name: "empty engine skips gate", engine: "", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := assert.New(t)
			_, projectDir := setup(t)
			opts := diffOptions()
			opts.Engine = tt.engine
			_, err := ComputeDiff(context.Background(), projectDir, opts)
			if tt.wantErr {
				if !is.Error(err) {
					return
				}
				is.Contains(err.Error(), tt.errPart)
				return
			}
			is.NoError(err)
		})
	}
}

func TestWriteDiffGolden(t *testing.T) {
	t.Run("plain rendering is byte-stable", func(t *testing.T) {
		changed, ok, err := newFileDiff(
			"hello.txt",
			[]byte("aaa\nbbb\nccc\nddd\neee\nfff\nggg\n"),
			[]byte("aaa\nBBB\nccc\nddd\neee\nfff\nggg\n"),
			false, false,
		)
		if err != nil {
			t.Fatalf("newFileDiff error: %v", err)
		}
		if !ok {
			t.Fatal("newFileDiff reported no change, want drift")
		}
		added, ok, err := newFileDiff("new.txt", nil, []byte("hello\n"), true, false)
		if err != nil {
			t.Fatalf("newFileDiff error: %v", err)
		}
		if !ok {
			t.Fatal("newFileDiff reported no change for a new file")
		}
		var out strings.Builder
		if err := WriteDiff(&out, []FileDiff{added, changed}, WriteOptions{NoColor: true}); err != nil {
			t.Fatalf("WriteDiff error: %v", err)
		}
		want, err := os.ReadFile("testdata/diff_basic.golden")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b") {
			t.Errorf("plain diff with NoColor contains ANSI escapes:\n%q", out.String())
		}
		if out.String() != string(want) {
			t.Errorf("plain diff mismatch:\ngot:\n%s\nwant:\n%s", out.String(), want)
		}
	})
	t.Run("empty set writes nothing", func(t *testing.T) {
		var out strings.Builder
		if err := WriteDiff(&out, nil, WriteOptions{}); err != nil {
			t.Fatalf("WriteDiff error: %v", err)
		}
		if out.String() != "" {
			t.Errorf("WriteDiff(nil) = %q, want empty", out.String())
		}
	})
}

func TestNewFileDiff(t *testing.T) {
	tests := []struct {
		name             string
		local, fresh     string
		isNew, isDeleted bool
		wantChanged      bool
		wantAdded        int
		wantRemoved      int
	}{
		{name: "identical content", local: "a\nb\n", fresh: "a\nb\n"},
		{name: "one line changed", local: "a\nb\n", fresh: "a\nB\n", wantChanged: true, wantAdded: 1, wantRemoved: 1},
		{name: "new file", fresh: "hello\n", isNew: true, wantChanged: true, wantAdded: 1},
		{name: "deleted file", local: "bye\n", isDeleted: true, wantChanged: true, wantRemoved: 1},
		{name: "appended line", local: "a\n", fresh: "a\nb\n", wantChanged: true, wantAdded: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var local, fresh []byte
			if tt.local != "" {
				local = []byte(tt.local)
			}
			if tt.fresh != "" {
				fresh = []byte(tt.fresh)
			}
			diff, changed, err := newFileDiff("f.txt", local, fresh, tt.isNew, tt.isDeleted)
			if err != nil {
				t.Fatalf("newFileDiff error: %v", err)
			}
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if !changed {
				return
			}
			if diff.Added != tt.wantAdded || diff.Removed != tt.wantRemoved {
				t.Errorf("added/removed = %d/%d, want %d/%d", diff.Added, diff.Removed, tt.wantAdded, tt.wantRemoved)
			}
			if !strings.Contains(diff.Unified, "@@") {
				t.Errorf("Unified missing hunk header:\n%s", diff.Unified)
			}
		})
	}
}
