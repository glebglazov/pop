package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
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

	ti := textinput.New()
	ti.Prompt = "> "
	styles := ti.Styles()
	styles.Cursor.Blink = false
	ti.SetStyles(styles)
	ti.Focus()

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
		pattern := []rune(strings.ToLower(query))
		slab := util.MakeSlab(100*1024, 2048)

		type match struct {
			name  string
			score int
		}
		var matches []match
		for _, name := range dp.entries {
			chars := util.ToChars([]byte(name))
			result, _ := algo.FuzzyMatchV2(false, true, true, &chars, pattern, false, slab)
			if result.Score > 0 {
				matches = append(matches, match{name: name, score: result.Score})
			}
		}
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].score < matches[j].score
		})

		dp.filtered = base
		for _, m := range matches {
			dp.filtered = append(dp.filtered, m.name)
		}
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
	visible := dp.height
	if visible > len(dp.filtered) {
		visible = len(dp.filtered)
	}
	if visible == 0 {
		dp.scroll = 0
		return
	}
	if dp.cursor < dp.scroll {
		dp.scroll = dp.cursor
	}
	if dp.cursor >= dp.scroll+visible {
		dp.scroll = dp.cursor - visible + 1
	}
	if dp.scroll < 0 {
		dp.scroll = 0
	}
	maxScroll := len(dp.filtered) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if dp.scroll > maxScroll {
		dp.scroll = maxScroll
	}
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

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("237")).
		Foreground(lipgloss.Color("255"))
	pipeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39"))
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true)

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

	// Input box
	boxWidth := dp.width
	if boxWidth < 20 {
		boxWidth = 40
	}
	innerWidth := boxWidth - 2

	b.WriteString("┌")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┐\n")

	inputView := dp.input.View()
	padding := innerWidth - lipgloss.Width(inputView)
	if padding < 0 {
		padding = 0
	}
	b.WriteString("│")
	b.WriteString(inputView)
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("│\n")

	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┘\n")

	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
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
