package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ConfigurePickerResult holds the result from the configure picker
type ConfigurePickerResult struct {
	Path         string
	DisplayDepth int
	Cancelled    bool
}

type configurePhase int

const (
	phasePath configurePhase = iota
	phaseDepth
)

// ConfigurePicker is a two-phase TUI for entering a project path pattern and display depth
type ConfigurePicker struct {
	phase         configurePhase
	input         textinput.Model
	path          string   // confirmed path text (preserved across transitions)
	depth         int      // current depth (preserved across transitions)
	expandedPaths []string // raw absolute paths from expandFn
	preview       []string // display names computed from expandedPaths + depth
	height        int
	width         int
	cancelled     bool
	confirmed     bool
	expandFn      func(string) []string

	// Cursor position memory per phase
	pathCursor  int // remembered cursor position in path phase
	depthCursor int // remembered cursor position in depth phase

	// Tab completion state
	tabMatches []string // current completion candidates
	tabIndex   int      // current position in cycle (-1 = none)
	tabPrefix  string   // the text that was present when Tab was first pressed
}

// NewConfigurePicker creates a new configure picker with the given expand function
func NewConfigurePicker(expandFn func(string) []string) *ConfigurePicker {
	ti := textinput.New()
	ti.Prompt = "> "
	styles := ti.Styles()
	styles.Cursor.Blink = false
	ti.SetStyles(styles)
	ti.Focus()

	return &ConfigurePicker{
		phase:    phasePath,
		input:    ti,
		depth:    1,
		expandFn: expandFn,
		tabIndex: -1,
		height:   10,
	}
}

func (cp *ConfigurePicker) Init() tea.Cmd {
	return nil
}

func (cp *ConfigurePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cp.width = msg.Width
		cp.height = msg.Height - 6 // Reserve space for hint + preview header + input box (3 lines) + key hints
		if cp.height < 3 {
			cp.height = 3
		}
		return cp, nil

	case tea.KeyPressMsg:
		switch cp.phase {
		case phasePath:
			return cp.updatePathPhase(msg)
		case phaseDepth:
			return cp.updateDepthPhase(msg)
		}
	}

	return cp, nil
}

func (cp *ConfigurePicker) updatePathPhase(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, configureKeys.Quit):
		cp.cancelled = true
		return cp, tea.Quit

	case key.Matches(msg, configureKeys.Escape):
		cp.cancelled = true
		return cp, tea.Quit

	case key.Matches(msg, configureKeys.Enter):
		cp.path = cp.input.Value()
		if cp.path == "" {
			cp.cancelled = true
			return cp, tea.Quit
		}
		// Save path cursor, transition to depth phase
		cp.pathCursor = cp.input.Position()
		cp.phase = phaseDepth
		depthStr := strconv.Itoa(cp.depth)
		cp.input.SetValue(depthStr)
		cp.input.SetCursor(cp.depthCursor)
		cp.clearTabState()
		return cp, nil

	case key.Matches(msg, configureKeys.Tab):
		cp.completeTab()
		return cp, nil

	default:
		// Clear tab state on any non-tab keystroke
		cp.clearTabState()

		// Update text input
		var cmd tea.Cmd
		cp.input, cmd = cp.input.Update(msg)

		// Update preview
		cp.updatePreview()

		return cp, cmd
	}
}

func (cp *ConfigurePicker) updateDepthPhase(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, configureKeys.Quit):
		cp.cancelled = true
		return cp, tea.Quit

	case key.Matches(msg, configureKeys.Escape):
		// Save depth cursor, go back to path phase
		cp.depthCursor = cp.input.Position()
		cp.phase = phasePath
		cp.input.SetValue(cp.path)
		cp.input.SetCursor(cp.pathCursor)
		cp.updatePreview()
		return cp, nil

	case key.Matches(msg, configureKeys.Enter):
		// Parse depth from input
		if d, err := strconv.Atoi(cp.input.Value()); err == nil && d >= 1 {
			cp.depth = d
		}
		cp.confirmed = true
		return cp, tea.Quit

	case key.Matches(msg, configureKeys.Up):
		cp.depth++
		cp.input.SetValue(strconv.Itoa(cp.depth))
		cp.updatePreviewForDepth()
		return cp, nil

	case key.Matches(msg, configureKeys.Down):
		if cp.depth > 1 {
			cp.depth--
			cp.input.SetValue(strconv.Itoa(cp.depth))
			cp.updatePreviewForDepth()
		}
		return cp, nil

	default:
		// Only allow digit input
		if msg.Text != "" {
			for _, r := range msg.Text {
				if !unicode.IsDigit(r) {
					return cp, nil
				}
			}
		}

		var cmd tea.Cmd
		cp.input, cmd = cp.input.Update(msg)

		// Parse and update depth from input
		if d, err := strconv.Atoi(cp.input.Value()); err == nil && d >= 1 {
			cp.depth = d
			cp.updatePreviewForDepth()
		}

		return cp, cmd
	}
}

func (cp *ConfigurePicker) updatePreview() {
	val := cp.input.Value()
	if val == "" {
		cp.expandedPaths = nil
		cp.preview = nil
		return
	}
	cp.expandedPaths = cp.expandFn(val)
	cp.computePreviewNames()
}

func (cp *ConfigurePicker) updatePreviewForDepth() {
	cp.computePreviewNames()
}

func (cp *ConfigurePicker) computePreviewNames() {
	cp.preview = make([]string, len(cp.expandedPaths))
	for i, p := range cp.expandedPaths {
		cp.preview[i] = LastNSegments(p, cp.depth)
	}
}

// Tab completion

func (cp *ConfigurePicker) clearTabState() {
	cp.tabMatches = nil
	cp.tabIndex = -1
	cp.tabPrefix = ""
}

func (cp *ConfigurePicker) completeTab() {
	if cp.tabIndex >= 0 && len(cp.tabMatches) > 0 {
		// Cycle to next match
		cp.tabIndex = (cp.tabIndex + 1) % len(cp.tabMatches)
		cp.applyTabCompletion()
		return
	}

	// First tab press: compute matches
	val := cp.input.Value()

	// After a glob star, just insert "/" so the user can quickly build patterns like ~/Dev/*/*
	if strings.HasSuffix(val, "*") {
		cp.input.SetValue(val + "/")
		cp.input.SetCursor(len(val) + 1)
		cp.updatePreview()
		return
	}

	cp.tabPrefix = val

	expanded := expandTilde(val)

	dirPart := filepath.Dir(expanded)
	prefix := filepath.Base(expanded)

	// If the input ends with "/" treat the whole thing as a directory
	if strings.HasSuffix(expanded, "/") {
		dirPart = expanded
		prefix = ""
	}

	entries, err := os.ReadDir(dirPart)
	if err != nil {
		return
	}

	var matches []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !isDirOrSymlinkToDir(dirPart, e) {
			continue
		}
		if prefix == "" || strings.HasPrefix(e.Name(), prefix) {
			matches = append(matches, e.Name())
		}
	}

	if len(matches) == 0 {
		return
	}

	cp.tabMatches = matches
	cp.tabIndex = 0
	cp.applyTabCompletion()
}

func (cp *ConfigurePicker) applyTabCompletion() {
	expanded := expandTilde(cp.tabPrefix)
	dirPart := filepath.Dir(expanded)
	if strings.HasSuffix(expanded, "/") {
		dirPart = expanded
	}

	completedPath := filepath.Join(dirPart, cp.tabMatches[cp.tabIndex]) + "/"
	display := contractTilde(completedPath)
	cp.input.SetValue(display)
	cp.input.SetCursor(len(display))
	cp.updatePreview()
}

// View renders the configure picker

func (cp *ConfigurePicker) View() tea.View {
	var b strings.Builder

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Phase-specific top hint
	switch cp.phase {
	case phasePath:
		b.WriteString("  ")
		b.WriteString(headerStyle.Render("Enter a project directory pattern"))
		b.WriteString("\n")
	case phaseDepth:
		b.WriteString("  ")
		b.WriteString(headerStyle.Render("Set display depth"))
		b.WriteString("\n")
	}

	// Preview section
	previewHeader := "Preview"
	if cp.depth > 1 {
		previewHeader += fmt.Sprintf(" (depth: %d)", cp.depth)
	}
	previewHeader += ":"

	// Calculate how many preview lines we can show
	previewHeight := cp.height
	if previewHeight < 0 {
		previewHeight = 0
	}

	// Count preview lines needed
	previewCount := len(cp.preview)
	showMore := false
	if previewCount > previewHeight {
		showMore = true
		previewCount = previewHeight - 1 // leave room for "... and N more"
		if previewCount < 0 {
			previewCount = 0
		}
	}

	// Count total lines (header + preview items + optional "more" line)
	totalPreviewLines := 1 + previewCount // 1 for header
	if showMore {
		totalPreviewLines++
	}

	// Empty lines to push content to bottom
	emptyLines := cp.height - totalPreviewLines
	if emptyLines < 0 {
		emptyLines = 0
	}
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}

	// Preview header
	if len(cp.preview) > 0 {
		b.WriteString("  ")
		b.WriteString(previewStyle.Render(previewHeader))
		b.WriteString("\n")

		// Preview items
		for i := 0; i < previewCount; i++ {
			b.WriteString("    ")
			b.WriteString(previewStyle.Render(cp.preview[i]))
			b.WriteString("\n")
		}

		if showMore {
			remaining := len(cp.preview) - previewCount
			b.WriteString("    ")
			b.WriteString(dimStyle.Render(fmt.Sprintf("... and %d more", remaining)))
			b.WriteString("\n")
		}
	} else {
		b.WriteString("  ")
		b.WriteString(previewStyle.Render(previewHeader))
		b.WriteString("\n")

		b.WriteString("    ")
		b.WriteString(previewStyle.Render("(no matches)"))
		b.WriteString("\n")
	}

	// Input box
	boxWidth := cp.width
	if boxWidth < 20 {
		boxWidth = 40
	}
	innerWidth := boxWidth - 2

	b.WriteString("┌")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┐\n")

	inputView := cp.input.View()
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

	// Key hints
	var hints string
	switch cp.phase {
	case phasePath:
		hints = "  Tab complete · Enter confirm · Esc cancel · use * for glob patterns"
	case phaseDepth:
		hints = "  ↑/↓ adjust depth · Enter confirm · Esc back"
	}
	b.WriteString(hintStyle.Render(hints))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// Result returns the configure picker result after running
func (cp *ConfigurePicker) Result() ConfigurePickerResult {
	if cp.cancelled || !cp.confirmed {
		return ConfigurePickerResult{Cancelled: true}
	}
	return ConfigurePickerResult{
		Path:         cp.path,
		DisplayDepth: cp.depth,
	}
}

// RunConfigurePicker launches the configure picker and returns the result
func RunConfigurePicker(expandFn func(string) []string) (ConfigurePickerResult, error) {
	cp := NewConfigurePicker(expandFn)
	program := tea.NewProgram(cp)
	m, err := program.Run()
	if err != nil {
		return ConfigurePickerResult{Cancelled: true}, err
	}
	return m.(*ConfigurePicker).Result(), nil
}

// Helpers

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}

// isDirOrSymlinkToDir returns true if the entry is a directory,
// or a symlink whose target is a directory.
func isDirOrSymlinkToDir(dir string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink != 0 {
		target := filepath.Join(dir, e.Name())
		info, err := os.Stat(target) // Stat follows symlinks
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

func contractTilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + path[len(home):]
	}
	if path == home {
		return "~"
	}
	return path
}

// Key bindings for configure picker
var configureKeys = struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	Quit   key.Binding
	Tab    key.Binding
}{
	Up:     key.NewBinding(key.WithKeys("up")),
	Down:   key.NewBinding(key.WithKeys("down")),
	Enter:  key.NewBinding(key.WithKeys("enter")),
	Escape: key.NewBinding(key.WithKeys("esc")),
	Quit:   key.NewBinding(key.WithKeys("ctrl+c")),
	Tab:    key.NewBinding(key.WithKeys("tab")),
}
