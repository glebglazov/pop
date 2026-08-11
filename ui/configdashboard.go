package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/internal/tty"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// The Config dashboard: a searchable list of override-exposed config keys on the
// left, a config-format preview of the highlighted key on the right (ADR-0202
// decisions 10 and 12). It is a self-contained component rather than a page of
// any one program, because the three hosts it must open from — the Work
// dashboard, the project picker and the worktree picker — are unrelated tea
// programs. `pop config dashboard` runs this same model as a top-level program.
//
// Two contracts bind every host (ADR-0202 decision 11): while this model is open
// the host suspends its own keys, and this model never writes to stdout on any
// path, because in the picker hosts stdout is a data channel. Errors surface as
// a row in this view instead.
//
// This pass is read-only: no editing keys are bound, and nothing here writes.

// configOverrideMarker marks a row whose key currently carries an override, so
// the list answers "what have I changed" without arrowing through every preview.
const configOverrideMarker = "●"

// ConfigDashboardReachLine is one actor's line of a key's declared reach
// (ADR-0198), already resolved by the caller.
type ConfigDashboardReachLine struct {
	Actor  string
	Detail string
}

// ConfigDashboardPreview is everything the right-hand pane shows for one key.
// Every field is rendered text: this component decides layout, never provenance.
type ConfigDashboardPreview struct {
	// ValueTOML is the effective value as a `key = value` TOML statement, and may
	// span several lines.
	ValueTOML string
	// Provenance names the layer that produced ValueTOML.
	Provenance string
	// Note tells apart two states that both look empty, or is blank when the
	// value speaks for itself.
	Note string
	// SourceTOML is what the layer below an override still says, shown dimmed so
	// the copy and remove actions have a visible target. Empty when there is no
	// override, or when no layer below it defines the key.
	SourceTOML string
	// SourceProvenance names where SourceTOML came from, or — with SourceTOML
	// empty — what removing the override would restore.
	SourceProvenance string
	// Reach is the key's declared reach; nil for a key that declares none, which
	// renders no reach block at all.
	Reach []ConfigDashboardReachLine
}

// ConfigDashboardRow is one override-exposed key as the list shows it.
type ConfigDashboardRow struct {
	// Key is the dotted config path — the row's own text, because it is what
	// `pop config keys` takes and what the preview's TOML shows.
	Key string
	// Desc is the schema description, rendered dim beneath the path where the
	// pane is tall enough.
	Desc string
	// Overridden marks the row.
	Overridden bool
	Preview    ConfigDashboardPreview
}

// ConfigDashboard is the component model.
type ConfigDashboard struct {
	rows     []ConfigDashboardRow
	filtered []ConfigDashboardRow
	list     *List[ConfigDashboardRow]
	input    TextField

	width    int
	height   int
	showHelp bool
	done     bool
	// failure is the error row decision 11 requires instead of a write to
	// stdout. Hosts and the standalone runner set it through Fail.
	failure string
}

// NewConfigDashboard builds the component over the override-exposed keys, in the
// order the caller listed them.
func NewConfigDashboard(rows []ConfigDashboardRow) *ConfigDashboard {
	m := &ConfigDashboard{rows: rows, filtered: rows, input: NewTextField()}
	m.input.Focus()
	m.list = NewList(rows, Opts[ConfigDashboardRow]{
		Key:  func(r ConfigDashboardRow) string { return r.Key },
		Cell: m.renderRow,
	})
	return m
}

// Done reports whether the human has closed the component.
func (m *ConfigDashboard) Done() bool { return m.done }

// Selected returns the highlighted row, or false when the filter matches none.
func (m *ConfigDashboard) Selected() (ConfigDashboardRow, bool) { return m.list.Selected() }

// Fail records a message to show as a row in this view. It exists so a host with
// nowhere safe to print — the worktree picker's stdout is a `cd` argument — can
// still report a failure to the human.
func (m *ConfigDashboard) Fail(msg string) { m.failure = msg }

// SetSize gives the component its pane, for a host that renders ViewContent
// inside its own frame rather than running it as a program.
func (m *ConfigDashboard) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Init implements tea.Model.
func (m *ConfigDashboard) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *ConfigDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		if ToggleHelp(&m.showHelp, msg) {
			return m, nil
		}
		if m.showHelp {
			return m, nil
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *ConfigDashboard) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, configDashboardKeys.Quit):
		m.done = true
		return tea.Quit
	case key.Matches(msg, configDashboardKeys.Up):
		m.list.MoveUp()
		return nil
	case key.Matches(msg, configDashboardKeys.Down):
		m.list.MoveDown()
		return nil
	}
	// Everything else edits the query, so the search field needs no focus key
	// and a human can start typing the moment the component opens.
	m.input.Update(msg)
	m.filter()
	return nil
}

// filter narrows the list over the dotted path and the description text
// together, so "verifier" finds work.verify.agents through its desc and
// "work.att" finds it through its path. Matching rows keep registry order: with
// a handful of keys, reordering by score moves rows under a human's fingers for
// nothing.
func (m *ConfigDashboard) filter() {
	query := strings.TrimSpace(m.input.Value())
	if query == "" {
		m.filtered = m.rows
	} else {
		pattern := []rune(strings.ToLower(query))
		slab := util.MakeSlab(100*1024, 2048)
		matched := make([]ConfigDashboardRow, 0, len(m.rows))
		for _, row := range m.rows {
			chars := util.ToChars([]byte(strings.ToLower(row.Key + " " + row.Desc)))
			result, _ := algo.FuzzyMatchV2(false, true, true, &chars, pattern, false, slab)
			if result.Score > 0 {
				matched = append(matched, row)
			}
		}
		m.filtered = matched
	}
	m.list.SetItems(m.filtered)
}

// View implements tea.Model. AltScreen is on: run as a top-level program this is
// the whole screen, and a host renders ViewContent into its own frame instead.
func (m *ConfigDashboard) View() tea.View {
	content := m.ViewContent()
	if m.showHelp {
		content = RenderHelpOverlay("Help · Config", m.helpEntries(), m.width, m.viewHeight())
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m *ConfigDashboard) helpEntries() []HelpEntry {
	return []HelpEntry{
		{"type", "Filter over key path and description"},
		{"↑/↓", "Move highlight"},
		{"Esc", "Close"},
		{"C-h", "Toggle this help"},
	}
}

// ViewContent renders the component body. The standalone program and any host
// overlay share this exact render, and so do the golden tests.
func (m *ConfigDashboard) ViewContent() string {
	frame := m.frameSpec()
	return frame.Render(m.viewBody(frame.BodyHeight(m.viewHeight())))
}

func (m *ConfigDashboard) frameSpec() Frame {
	var warnings []string
	if m.failure != "" {
		warnings = []string{m.failure}
	}
	return Frame{
		Width:    m.viewWidth(),
		TermH:    m.viewHeight(),
		Header:   "  Config · keys you can override",
		InputBox: m.input.View(),
		Warnings: warnings,
		Hints:    "  type to filter · ↑/↓ move · esc close · C-h help",
	}
}

// viewBody composes the two panes: the list on the left, the preview of the
// highlighted key on the right, one separator column between them.
func (m *ConfigDashboard) viewBody(height int) string {
	if height <= 0 {
		return ""
	}
	listWidth := m.listWidth()
	m.list.SetLinesPerItem(m.linesPerItem(height))
	m.list.Resize(height)

	left := m.list.VisibleRows()
	right := m.previewLines(height)

	sep := lipgloss.NewStyle().Foreground(colorSeparator).Render("│")
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		leftLine := ""
		if i < len(left) {
			leftLine = left[i]
		}
		rightLine := ""
		if i < len(right) {
			rightLine = right[i]
		}
		pad := listWidth - lipgloss.Width(leftLine)
		if pad < 0 {
			pad = 0
		}
		lines[i] = leftLine + strings.Repeat(" ", pad) + " " + sep + " " + rightLine
	}
	return strings.Join(lines, "\n")
}

// linesPerItem gives every row its dim description line when the pane can hold
// one for each row, and falls back to bare paths when it cannot — the path is
// what identifies the key, so it is what survives a short pane.
func (m *ConfigDashboard) linesPerItem(height int) int {
	if len(m.filtered) > 0 && height >= 2*len(m.filtered) {
		return 2
	}
	return 1
}

// renderRow draws one list row: the marker column, the dotted path, and — on the
// second line, when there is one — the description dimmed.
func (m *ConfigDashboard) renderRow(row ConfigDashboardRow, state RowState) string {
	width := m.listWidth() - 2 // the list owns a two-column cursor prefix
	if state.LineIndex > 0 {
		if strings.TrimSpace(row.Desc) == "" {
			return ""
		}
		return dimStyle.Render(TruncateString("    "+row.Desc, width))
	}
	marker := "  "
	if row.Overridden {
		marker = IndicatorStyle.Render(configOverrideMarker) + " "
	}
	text := TruncateString(row.Key, width-2)
	if state.Selected {
		text = selectedGateItemStyle.Render(text)
	}
	return marker + text
}

// previewLines renders the highlighted key's preview, clipped to the pane. It is
// config format throughout — the value is the TOML statement that would set the
// key — with the provenance line, the note, the source value and any declared
// reach under it.
func (m *ConfigDashboard) previewLines(height int) []string {
	row, ok := m.list.Selected()
	if !ok {
		hint := "(no override-exposed keys)"
		if len(m.rows) > 0 {
			hint = "(no key matches the filter)"
		}
		return []string{hintStyle.Render(hint)}
	}

	width := m.previewWidth()
	var lines []string
	add := func(style lipgloss.Style, text string) {
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, style.Render(TruncateString(line, width)))
		}
	}
	plain := lipgloss.NewStyle().Foreground(colorPreview)

	add(plain, row.Preview.ValueTOML)
	if row.Preview.Provenance != "" {
		lines = append(lines, "")
		add(hintStyle, "from: "+row.Preview.Provenance)
	}
	if row.Preview.Note != "" {
		add(hintStyle, row.Preview.Note)
	}
	if row.Preview.SourceProvenance != "" {
		lines = append(lines, "")
		add(hintStyle, "without the override: "+row.Preview.SourceProvenance)
		if row.Preview.SourceTOML != "" {
			add(dimStyle, row.Preview.SourceTOML)
		}
	}
	if len(row.Preview.Reach) > 0 {
		lines = append(lines, "")
		add(hintStyle, "reach:")
		for _, line := range row.Preview.Reach {
			add(dimStyle, "  "+line.Actor+"  "+line.Detail)
		}
	}

	if len(lines) > height {
		lines = append(lines[:height-1:height-1], hintStyle.Render("  … clipped to fit the pane"))
	}
	return lines
}

// listWidth splits the pane: enough for a dotted path on the left, with the rest
// for the preview's TOML.
func (m *ConfigDashboard) listWidth() int {
	width := m.viewWidth() * 2 / 5
	if width < 24 {
		width = 24
	}
	if max := m.viewWidth() - 24; width > max && max > 0 {
		width = max
	}
	return width
}

func (m *ConfigDashboard) previewWidth() int {
	width := m.viewWidth() - m.listWidth() - 3 // pad + separator + pad
	if width < 8 {
		width = 8
	}
	return width
}

func (m *ConfigDashboard) viewWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

func (m *ConfigDashboard) viewHeight() int {
	if m.height <= 0 {
		return 24
	}
	return m.height
}

// RunConfigDashboard runs the component as a top-level tea program. The caller
// has already established that out is a terminal: this never prints to it
// outside the program's own frame (ADR-0202 decision 11).
func RunConfigDashboard(rows []ConfigDashboardRow, in io.Reader, out io.Writer) error {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	m := NewConfigDashboard(rows)
	if fd, ok := tty.TerminalFd(in); ok {
		claimTerminal(fd, nil)
	}
	final, err := tea.NewProgram(m,
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
	).Run()
	if err != nil {
		return err
	}
	if _, ok := final.(*ConfigDashboard); !ok {
		return fmt.Errorf("config dashboard: unexpected model type %T", final)
	}
	return nil
}

var configDashboardKeys = struct {
	Up   key.Binding
	Down key.Binding
	Quit key.Binding
}{
	// The query owns every printable key, so movement lives on the arrows and
	// the ctrl chords the pickers already use.
	Up:   key.NewBinding(key.WithKeys("up", "ctrl+p")),
	Down: key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Quit: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
}
