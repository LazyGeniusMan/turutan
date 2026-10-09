// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// testFiles returns a fixed two-file drift for model tests.
func testFiles() []FileDiff {
	return []FileDiff{
		{Path: "a.txt", Unified: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n", Added: 1, Removed: 1},
		{Path: "b.txt", Unified: "--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-x\n+y\n", Added: 1, Removed: 1},
	}
}

// press builds a printable-key press message.
func press(key string) tea.KeyPressMsg {
	var code rune
	if len(key) == 1 {
		code = rune(key[0])
	}
	return tea.KeyPressMsg(tea.Key{Text: key, Code: code})
}

// viewString renders the model view to a plain string.
func viewString(t *testing.T, m diffModel) string {
	t.Helper()
	return m.View().Content
}

func TestDiffModelView(t *testing.T) {
	t.Run("lists files and selected hunk", func(t *testing.T) {
		got := viewString(t, newDiffModel(testFiles()))
		for _, want := range []string{"turutan diff", "a.txt", "b.txt", "+1/-1", "-old", "+new"} {
			if !strings.Contains(got, want) {
				t.Errorf("view missing %q:\n%s", want, got)
			}
		}
		if !strings.Contains(got, "> a.txt") {
			t.Errorf("view missing selection marker:\n%s", got)
		}
	})
	t.Run("window size accepted", func(t *testing.T) {
		m := newDiffModel(testFiles())
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		m, ok := updated.(diffModel)
		if !ok {
			t.Fatalf("Update returned %T, want diffModel", updated)
		}
		if m.width != 100 || m.height != 40 {
			t.Errorf("size = %dx%d, want 100x40", m.width, m.height)
		}
	})
}

func TestDiffModelNavigation(t *testing.T) {
	t.Run("j moves down k moves up", func(t *testing.T) {
		m := newDiffModel(testFiles())
		updated, _ := m.Update(press("j"))
		m = updated.(diffModel)
		if m.cursor != 1 {
			t.Fatalf("cursor = %d, want 1 after j", m.cursor)
		}
		if got := viewString(t, m); !strings.Contains(got, "> b.txt") || !strings.Contains(got, "-x") {
			t.Errorf("view did not follow selection:\n%s", got)
		}
		updated, _ = m.Update(press("k"))
		m = updated.(diffModel)
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want 0 after k", m.cursor)
		}
	})
	t.Run("cursor clamps at ends", func(t *testing.T) {
		m := newDiffModel(testFiles())
		updated, _ := m.Update(press("k"))
		m = updated.(diffModel)
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want clamp at 0", m.cursor)
		}
		updated, _ = m.Update(press("j"))
		m = updated.(diffModel)
		updated, _ = m.Update(press("j"))
		m = updated.(diffModel)
		if m.cursor != 1 {
			t.Errorf("cursor = %d, want clamp at 1", m.cursor)
		}
	})
	t.Run("scroll clamps to hunk", func(t *testing.T) {
		m := newDiffModel(testFiles())
		updated, _ := m.Update(press("d"))
		m = updated.(diffModel)
		if m.hunkOffset != m.maxHunkOffset() {
			t.Errorf("hunkOffset = %d, want clamp at %d", m.hunkOffset, m.maxHunkOffset())
		}
		updated, _ = m.Update(press("u"))
		m = updated.(diffModel)
		if m.hunkOffset != 0 {
			t.Errorf("hunkOffset = %d, want 0 after scrolling up", m.hunkOffset)
		}
		// Rendering a clamped offset must not panic or drop the file list.
		if got := viewString(t, m); !strings.Contains(got, "a.txt") {
			t.Errorf("view lost file list after scroll:\n%s", got)
		}
	})
	t.Run("quit keys request quit", func(t *testing.T) {
		msgs := map[string]tea.Msg{
			"q":      press("q"),
			"esc":    tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}),
			"ctrl+c": tea.KeyPressMsg(tea.Key{Text: "c", Code: 'c', Mod: tea.ModCtrl}),
			"ETX":    tea.KeyPressMsg(tea.Key{Code: 3}),
		}
		for name, msg := range msgs {
			_, cmd := newDiffModel(testFiles()).Update(msg)
			if cmd == nil {
				t.Errorf("key %s returned nil cmd, want quit", name)
				continue
			}
			if got := cmd(); got == nil {
				t.Errorf("key %s cmd returned nil msg, want quit", name)
			}
		}
	})
}
