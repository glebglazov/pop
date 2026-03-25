package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// DirPickerResult holds the result from the directory picker
type DirPickerResult struct {
	Path      string // absolute path of selected directory
	Cancelled bool
}

// DirPicker is a directory browser for selecting project root directories
type DirPicker struct {
	currentDir string
	homeDir    string
	entries    []string // subdirectory names
	filtered   []string // filtered entries (with ".." prepended)
	input      textinput.Model
	cursor     int
	scroll     int
	height     int
	width      int
	selected   string
	cancelled  bool
}

// NewDirPicker creates a directory picker starting at the user's home directory
func NewDirPicker() *DirPicker {
	home, _ := os.UserHomeDir()

	ti := newTextInput()

	dp := &DirPicker{
		currentDir: home,
		homeDir:    home,
		input:      ti,
		height:     10,
	}
	dp.loadEntries()
	return dp
}

func (dp *DirPicker) loadEntries() {
	dp.entries = nil
	entries, err := os.ReadDir(dp.currentDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dp.entries = append(dp.entries, e.Name())
		}
	}
	sort.Strings(dp.entries)
	dp.input.SetValue("")
	dp.applyFilter()
	dp.cursorToEnd()
}

func (dp *DirPicker) Init() tea.Cmd {
	dp.cursorToEnd()
	return nil
}

func (dp *DirPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, dirPickerKeys.Quit):
			dp.cancelled = true
			return dp, tea.Quit

		case key.Matches(msg, dirPickerKeys.Add):
			if len(dp.filtered) > 0 {
				dp.selected = filepath.Join(dp.currentDir, dp.filtered[dp.cursor])
			}
			return dp, tea.Quit

		case key.Matches(msg, dirPickerKeys.Enter):
			if len(dp.filtered) > 0 {
				name := dp.filtered[dp.cursor]
				if name == ".." {
					dp.currentDir = filepath.Dir(dp.currentDir)
				} else {
					dp.currentDir = filepath.Join(dp.currentDir, name)
				}
				dp.loadEntries()
			}
			return dp, nil

		case key.Matches(msg, dirPickerKeys.Back):
			if dp.currentDir != "/" {
				dp.currentDir = filepath.Dir(dp.currentDir)
				dp.loadEntries()
			}
			return dp, nil

		case key.Matches(msg, dirPickerKeys.Up):
			if len(dp.filtered) > 0 {
				if dp.cursor > 0 {
					dp.cursor--
				} else {
					dp.cursor = len(dp.filtered) - 1
				}
				dp.adjustScroll()
			}
			return dp, nil

		case key.Matches(msg, dirPickerKeys.Down):
			if len(dp.filtered) > 0 {
				if dp.cursor < len(dp.filtered)-1 {
					dp.cursor++
				} else {
					dp.cursor = 0
				}
				dp.adjustScroll()
			}
			return dp, nil

		case key.Matches(msg, dirPickerKeys.HalfPageUp):
			if len(dp.filtered) > 0 {
				dp.cursor -= dp.height
				if dp.cursor < 0 {
					dp.cursor = 0
				}
				dp.adjustScroll()
			}
			return dp, nil

		case key.Matches(msg, dirPickerKeys.HalfPageDown):
			if len(dp.filtered) > 0 {
				dp.cursor += dp.height
				if dp.cursor >= len(dp.filtered) {
					dp.cursor = len(dp.filtered) - 1
				}
				dp.adjustScroll()
			}
			return dp, nil

		case key.Matches(msg, dirPickerKeys.ClearInput):
			dp.input.SetValue("")
			dp.applyFilter()
			return dp, nil
		}

	case tea.WindowSizeMsg:
		dp.width = msg.Width
		dp.height = msg.Height - 5 // header + input box (3 lines) + hints
		if dp.height < 3 {
			dp.height = 3
		}
	}

	var cmd tea.Cmd
	dp.input, cmd = dp.input.Update(msg)
	dp.applyFilter()
	return dp, cmd
}

func (dp *DirPicker) applyFilter() {
	query := dp.input.Value()

	var base []string
	if dp.currentDir != "/" {
		base = append(base, "..")
	}

	if query == "" {
		dp.filtered = append(base, dp.entries...)
	} else {
		dp.filtered = append(base, fuzzyMatch(query, dp.entries)...)
	}

	if dp.cursor >= len(dp.filtered) {
		dp.cursor = len(dp.filtered) - 1
	}
	if dp.cursor < 0 {
		dp.cursor = 0
	}
	dp.adjustScroll()
}

func (dp *DirPicker) adjustScroll() {
	dp.scroll = adjustScroll(dp.cursor, dp.scroll, dp.height, len(dp.filtered), 0)
}

func (dp *DirPicker) cursorToEnd() {
	if len(dp.filtered) > 0 {
		dp.cursor = len(dp.filtered) - 1
	}
	dp.adjustScroll()
}

func (dp *DirPicker) displayPath() string {
	path := dp.currentDir
	if strings.HasPrefix(path, dp.homeDir) {
		path = "~" + path[len(dp.homeDir):]
	}
	return path
}

func (dp *DirPicker) View() tea.View {
	var b strings.Builder

	// Header: current path
	b.WriteString("  ")
	b.WriteString(headerStyle.Render(dp.displayPath()))
	b.WriteString("\n")

	// Items
	visible := dp.height
	if visible > len(dp.filtered) {
		visible = len(dp.filtered)
	}
	start := dp.scroll

	emptyLines := dp.height - visible
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}

	for i := start; i < start+visible && i < len(dp.filtered); i++ {
		name := dp.filtered[i]
		display := " " + name + "/"
		if name == ".." {
			display = " .."
		}

		if i == dp.cursor {
			if dp.width > 0 {
				padding := dp.width - len([]rune(display)) - 2
				if padding > 0 {
					display += strings.Repeat(" ", padding)
				}
			}
			b.WriteString(pipeStyle.Render("▌ "))
			b.WriteString(selectedStyle.Render(display))
		} else {
			b.WriteString("  ")
			if name == ".." {
				b.WriteString(dimStyle.Render(display))
			} else {
				b.WriteString(display)
			}
		}
		b.WriteString("\n")
	}

	writeInputBox(&b, dp.width, dp.input.View())

	hints := "  ↑/↓ navigate · Enter open · - back · C-a add · Esc cancel"
	b.WriteString(hintStyle.Render(hints))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// Result returns the directory picker result
func (dp *DirPicker) Result() DirPickerResult {
	return DirPickerResult{
		Path:      dp.selected,
		Cancelled: dp.cancelled,
	}
}

// RunDirPicker launches the directory picker and returns the result
func RunDirPicker() (DirPickerResult, error) {
	dp := NewDirPicker()
	program := tea.NewProgram(dp)
	m, err := program.Run()
	if err != nil {
		return DirPickerResult{Cancelled: true}, err
	}
	return m.(*DirPicker).Result(), nil
}

var dirPickerKeys = struct {
	Up           key.Binding
	Down         key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	Enter        key.Binding
	Back         key.Binding
	Add          key.Binding
	Quit         key.Binding
	ClearInput   key.Binding
}{
	Up:           key.NewBinding(key.WithKeys("up", "ctrl+p")),
	Down:         key.NewBinding(key.WithKeys("down", "ctrl+n")),
	HalfPageUp:   key.NewBinding(key.WithKeys("ctrl+b")),
	HalfPageDown: key.NewBinding(key.WithKeys("ctrl+f")),
	Enter:        key.NewBinding(key.WithKeys("enter")),
	Back:         key.NewBinding(key.WithKeys("-")),
	Add:          key.NewBinding(key.WithKeys("ctrl+a")),
	Quit:         key.NewBinding(key.WithKeys("esc", "ctrl+c")),
	ClearInput:   key.NewBinding(key.WithKeys("alt+backspace", "ctrl+u")),
}
