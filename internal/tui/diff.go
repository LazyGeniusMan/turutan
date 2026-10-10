// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const defaultHeight = 24

type FileDiff struct {
	Path      string
	Unified   string
	Added     int
	Removed   int
	IsNew     bool
	IsDeleted bool
}

func RunDiff(files []FileDiff, opts Options) error {
	if len(files) == 0 {
		return nil
	}
	_, err := tea.NewProgram(newDiffModel(files, opts.NoColor)).Run()
	return err
}

type diffModel struct {
	files      []FileDiff
	cursor     int
	width      int
	height     int
	hunkOffset int
	noColor    bool
}

func keyName(msg tea.KeyPressMsg) string {
	if name := msg.Keystroke(); name != "" {
		return name
	}
	return msg.String()
}

func newDiffModel(files []FileDiff, noColor bool) diffModel {
	return diffModel{files: files, height: defaultHeight, noColor: noColor}
}

func (m diffModel) Init() tea.Cmd { return nil }

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

func (m diffModel) layout() (listSize, hunkSize int) {
	const chromeLines = 4
	avail := max(m.height-chromeLines, 2)
	listSize = min(len(m.files), max(1, avail/3))
	hunkSize = max(1, avail-listSize)
	return listSize, hunkSize
}

func (m diffModel) listWindow(listSize int) int {
	start := m.cursor - listSize + 1
	return max(0, start)
}

func (m diffModel) hunkPage() int {
	_, hunkSize := m.layout()
	return max(1, hunkSize)
}

func (m diffModel) maxHunkOffset() int {
	_, hunkSize := m.layout()
	lines := len(strings.Split(m.files[m.cursor].Unified, "\n"))
	return max(0, lines-hunkSize)
}
