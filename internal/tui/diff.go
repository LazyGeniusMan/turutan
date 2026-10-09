// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// defaultHeight is the assumed terminal height before the first
// WindowSizeMsg arrives.
const defaultHeight = 24

// FileDiff is one drifted file for interactive display. Path is the
// slash-relative project path, Unified the Context-3 unified diff, and
// Added/Removed the changed-line counts.
type FileDiff struct {
	Path      string
	Unified   string
	Added     int
	Removed   int
	IsNew     bool
	IsDeleted bool
}

// RunDiff starts the file-list + hunk review UI over files. It returns nil
// once the user quits (q, Esc or Ctrl+C); quitting is not an error because
// the drift itself is reported through the command exit code.
func RunDiff(files []FileDiff) error {
	if len(files) == 0 {
		return nil
	}
	_, err := tea.NewProgram(newDiffModel(files)).Run()
	return err
}

// diffModel is the Bubbletea model for drift review: a file list with a
// hunk view of the selected file.
type diffModel struct {
	files      []FileDiff
	cursor     int
	width      int
	height     int
	hunkOffset int
}

// keyName normalizes a key press to its matchable name. Keystroke covers
// named combos such as ctrl+c, while raw control characters arrive with
// String "\x03"; either form quits the review UI.
func keyName(msg tea.KeyPressMsg) string {
	if name := msg.Keystroke(); name != "" {
		return name
	}
	return msg.String()
}

// newDiffModel returns the review model over files.
func newDiffModel(files []FileDiff) diffModel {
	return diffModel{files: files, height: defaultHeight}
}

// Init implements tea.Model.
func (m diffModel) Init() tea.Cmd { return nil }

// Update implements tea.Model: j/k or arrows move the selection, PgUp/PgDn
// (or u/d) scroll the hunk view, q/Esc/Ctrl+C quits.
func (m diffModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch keyName(msg) {
		case "q", "esc", "ctrl+c", "\x03":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.hunkOffset = 0
			}
		case "down", "j":
			if m.cursor < len(m.files)-1 {
				m.cursor++
				m.hunkOffset = 0
			}
		case "pgup", "u":
			m.hunkOffset -= m.hunkPage()
			if m.hunkOffset < 0 {
				m.hunkOffset = 0
			}
		case "pgdown", "d", "space":
			m.hunkOffset += m.hunkPage()
			m.hunkOffset = min(m.hunkOffset, m.maxHunkOffset())
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m diffModel) View() tea.View {
	var out strings.Builder
	fmt.Fprintf(&out, "turutan diff — %d file(s) (q to quit)\n\n", len(m.files))
	listSize, hunkSize := m.layout()
	start := m.listWindow(listSize)
	for i := start; i < start+listSize && i < len(m.files); i++ {
		file := m.files[i]
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		fmt.Fprintf(&out, "%s%s +%d/-%d\n", marker, file.Path, file.Added, file.Removed)
	}
	fmt.Fprintf(&out, "\n--- %s ---\n", m.files[m.cursor].Path)
	lines := strings.Split(m.files[m.cursor].Unified, "\n")
	end := min(m.hunkOffset+hunkSize, len(lines))
	for _, line := range lines[m.hunkOffset:end] {
		out.WriteString(line + "\n")
	}
	if hidden := len(lines) - end; hidden > 0 {
		fmt.Fprintf(&out, "… %d more line(s) (PgDn to scroll)\n", hidden)
	}
	return tea.NewView(out.String())
}

// layout splits the available height between the file list and the hunk
// view, reserving lines for the header, separator and footer.
func (m diffModel) layout() (listSize, hunkSize int) {
	const chromeLines = 4
	avail := max(m.height-chromeLines, 2)
	listSize = min(len(m.files), max(1, avail/3))
	hunkSize = max(1, avail-listSize)
	return listSize, hunkSize
}

// listWindow returns the first visible file index, keeping the cursor in
// view.
func (m diffModel) listWindow(listSize int) int {
	start := m.cursor - listSize + 1
	return max(0, start)
}

// hunkPage is the scroll step for the hunk view.
func (m diffModel) hunkPage() int {
	_, hunkSize := m.layout()
	return max(1, hunkSize)
}

// maxHunkOffset clamps scrolling to the selected file's hunk length.
func (m diffModel) maxHunkOffset() int {
	_, hunkSize := m.layout()
	lines := len(strings.Split(m.files[m.cursor].Unified, "\n"))
	return max(0, lines-hunkSize)
}
