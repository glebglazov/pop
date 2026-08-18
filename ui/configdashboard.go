package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/internal/tty"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// The Config dashboard: a searchable list of the things in force where it was
// opened on the left, a preview of the highlighted one on the right (ADR-0202
// decisions 10 and 12). Most rows are overridable config keys and preview as
// config; a row may instead hold prose composed from several layers, which
// previews as the caller's own text and edits as Markdown (ADR-0212 decision 8).
// Either way this component holds no knowledge of what a row means. It is a
// self-contained component rather than a page of any one program, because the
// three hosts it must open from — the Work dashboard, the project picker and the
// worktree picker — are unrelated tea programs. `pop config dashboard` runs this
// same model as a top-level program.
//
// Two contracts bind every host (ADR-0202 decision 11): while this model is open
// the host suspends its own keys, and this model never writes to stdout on any
// path, because in the picker hosts stdout is a data channel. Errors surface as
// a row in this view instead.
//
// It is also the editor. Enter opens $EDITOR on the selected key in place,
// ctrl+y copies the source value down and ctrl+x removes the override; the
// override layer itself is reached through the injected ConfigOverrideWriter,
// so this package still holds no config knowledge.

// ConfigDashboardKeyLabel is how the chord that opens this component from a host
// reads in chrome — `alt+c` (ADR-0202 decision 10), in ui's A- prefix form. Any
// surface that tells a human where a setting is changed points here rather than
// spelling the chord itself.
const ConfigDashboardKeyLabel = "A-c"

// ConfigDashboardKey is that same chord as a key string, for a host that names
// its bindings in text rather than matching them.
const ConfigDashboardKey = "alt+c"

// IsConfigDashboardKey reports whether msg is the chord that opens this
// component from a host (ADR-0202 decision 10). Hosts match through this rather
// than spelling the chord, so the three of them cannot drift apart.
func IsConfigDashboardKey(msg tea.KeyPressMsg) bool {
	if msg.Code != 'c' && msg.Code != 'C' {
		return false
	}
	return msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl)
}

// configOverrideMarker marks a row whose key currently carries an override, so
// the list answers "what have I changed" without arrowing through every preview.
const configOverrideMarker = "●"

// configContestedMarker marks a row whose key more than one layer states a value
// for. It is the other half of the same question: what is quietly fighting the
// value in force (ADR-0212 decision 8). The two markers occupy their own columns,
// because a contested key is usually also an overridden one and a reader loses
// nothing by seeing both.
const configContestedMarker = "◆"

// configContestedStyle colours that marker apart from the override dot: the two
// say different things and sit in adjacent columns.
var configContestedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

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
	// Layers, when set, is the whole preview: text the caller has already
	// rendered, shown instead of every field above. It is how a row whose value is
	// a composed stack of prose — rather than one value with one provenance line —
	// says what is in force (ADR-0212 decision 8). This component decides where it
	// goes on screen and nothing about what it says.
	Layers string
	// EditSeed is what the editor opens on for such a row, and it is not what
	// Layers shows: the stack composes many layers and an edit writes exactly one
	// of them, so seeding the buffer with the whole preview would have the human
	// hand back layers they never wrote. Ignored unless Layers is set.
	EditSeed string
}

// prose reports a row whose value is written prose rather than a config value:
// the preview is the caller's own text and the editor buffer is Markdown.
func (p ConfigDashboardPreview) prose() bool { return p.Layers != "" }

// ConfigDashboardRow is one overridable key as the list shows it.
type ConfigDashboardRow struct {
	// Key is the dotted config path — the row's own text, because it is what
	// `pop config keys` takes and what the preview's TOML shows.
	Key string
	// Desc is the schema description, rendered dim beneath the path where the
	// pane is tall enough.
	Desc string
	// Overridden marks the row.
	Overridden bool
	// Contested reports that more than one layer states a value for this key. It
	// marks the row and sorts it to the top of the list.
	Contested bool
	Preview   ConfigDashboardPreview
}

// ConfigOverrideWriter is the override layer as this component needs it: the
// three actions a row offers, and a re-read of every row. The host injects it
// because provenance, validation and the file itself are config's business, not
// this package's.
type ConfigOverrideWriter interface {
	// Store makes buffer the whole value of key. The problem string is what the
	// human must fix — unparseable TOML, the wrong key, a value the schema
	// refuses — and it re-opens the editor rather than failing the component
	// (ADR-0202 decision 8); an error is the write itself going wrong.
	Store(key, buffer string) (problem string, err error)
	// CopySource writes the value below the override as the override.
	CopySource(key string) error
	// Remove deletes key's override, restoring the source. On a key with no
	// override it does nothing.
	Remove(key string) error
	// Rows re-resolves every row against the layers as they now stand, so a
	// write shows up in the marker and the provenance line at once.
	Rows() ([]ConfigDashboardRow, error)
}

// ConfigDashboardOpts wires the component's write side.
type ConfigDashboardOpts struct {
	// Writer is the override layer this dashboard edits. Left nil the component
	// is read-only: the editing keys are unbound and unadvertised.
	Writer ConfigOverrideWriter
	// Editor hands a temp file to the human's editor and calls done when it
	// exits. Nil means $EDITOR in place through tea.ExecProcess, which is what
	// every host wants (ADR-0202 decision 13); a caller with no terminal
	// substitutes it to drive the same loop.
	Editor func(path string, done tea.ExecCallback) tea.Cmd
}

// ConfigDashboard is the component model.
type ConfigDashboard struct {
	rows     []ConfigDashboardRow
	filtered []ConfigDashboardRow
	list     *List[ConfigDashboardRow]
	input    TextField

	writer ConfigOverrideWriter
	editor func(path string, done tea.ExecCallback) tea.Cmd

	width    int
	height   int
	showHelp bool
	done     bool
	// wrote records that the override layer changed while this component was
	// open. A host loads config once and hands that value to what it renders, so
	// it needs to know whether closing this component means re-reading (ADR-0202
	// decision 14).
	wrote bool
	// failure is the error row decision 11 requires instead of a write to
	// stdout. Hosts and the standalone runner set it through Fail.
	failure string
}

// NewConfigDashboard builds the component over the overridable keys: the
// contested ones first, and within each group the order the caller listed them.
func NewConfigDashboard(rows []ConfigDashboardRow, opts ConfigDashboardOpts) *ConfigDashboard {
	rows = contestedFirst(rows)
	m := &ConfigDashboard{
		rows:     rows,
		filtered: rows,
		input:    NewTextField(),
		writer:   opts.Writer,
		editor:   opts.Editor,
	}
	if m.editor == nil {
		m.editor = execEditorInPlace
	}
	m.input.Focus()
	m.list = NewList(rows, Opts[ConfigDashboardRow]{
		Key:             func(r ConfigDashboardRow) string { return r.Key },
		Cell:            m.renderRow,
		TopEdgeOnChrome: true,
	})
	return m
}

// Done reports whether the human has closed the component.
func (m *ConfigDashboard) Done() bool { return m.done }

// Wrote reports whether any action changed the override layer while this
// component was open, so a host knows whether to re-read config on close.
func (m *ConfigDashboard) Wrote() bool { return m.wrote }

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
	case configEditorDoneMsg:
		return m, m.editorReturned(msg)
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
	if m.writer != nil {
		switch {
		case key.Matches(msg, configDashboardKeys.Edit):
			return m.edit()
		case key.Matches(msg, configDashboardKeys.CopySource):
			return m.act("copy the source value down", m.writer.CopySource)
		case key.Matches(msg, configDashboardKeys.Remove):
			return m.act("remove the override", m.writer.Remove)
		}
	}
	// Everything else edits the query, so the search field needs no focus key
	// and a human can start typing the moment the component opens.
	m.input.Update(msg)
	m.filter()
	return nil
}

// contestedFirst lifts the keys more than one layer states a value for to the
// top of the list (ADR-0212 decision 8), leaving every other row where the caller
// put it. The list is what a human opens the dashboard on, so the rows that
// answer "what did I customize, and what is quietly fighting it" are the rows
// they meet first, with no second view to reach for.
//
// The sort is stable and touches order alone: no row is dropped, so the filter —
// which runs over the whole list, not over what is on screen — still reaches
// every key whatever the order.
func contestedFirst(rows []ConfigDashboardRow) []ConfigDashboardRow {
	sorted := make([]ConfigDashboardRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Contested && !sorted[j].Contested
	})
	return sorted
}

// filter narrows the list over the dotted path and the description text
// together, so "verifier" finds work.verify.agents through its desc and
// "work.att" finds it through its path. Matching rows keep registry order: with
// a handful of keys, reordering by score moves rows under a human's fingers for
// nothing.
func (m *ConfigDashboard) filter() {
	m.filtered = m.matchingRows()
	m.list.SetItems(m.filtered)
}

func (m *ConfigDashboard) matchingRows() []ConfigDashboardRow {
	query := strings.TrimSpace(m.input.Value())
	if query == "" {
		return m.rows
	}
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
	return matched
}

// act performs one of the two keystroke actions on the selected row and shows
// the result immediately. Neither asks for confirmation: a removed override is
// one copy-from-source away from being back (ADR-0202 decision 6).
func (m *ConfigDashboard) act(what string, do func(key string) error) tea.Cmd {
	row, ok := m.list.Selected()
	if !ok {
		return nil
	}
	if err := do(row.Key); err != nil {
		m.failure = fmt.Sprintf("Could not %s for %s: %v", what, row.Key, err)
		return nil
	}
	m.refresh()
	return nil
}

// edit opens the selected key in $EDITOR. The buffer is seeded with the whole
// `key = value` line that is in force — the override where there is one, the
// source value copied down where there is not — so the human edits a real value
// rather than an empty file (ADR-0202 decision 7). A prose row seeds its own
// buffer instead, holding the one layer an edit there writes.
func (m *ConfigDashboard) edit() tea.Cmd {
	row, ok := m.list.Selected()
	if !ok {
		return nil
	}
	if row.Preview.prose() {
		return m.openEditor(row.Key, row.Preview.EditSeed, ".md")
	}
	return m.openEditor(row.Key, row.Preview.ValueTOML, ".toml")
}

// configEditorDoneMsg reports that the editor process for one key has exited.
// It carries the buffer's suffix so a re-open after a refusal lands the human
// back in the same kind of file they were just editing.
type configEditorDoneMsg struct {
	key    string
	path   string
	suffix string
	err    error
}

func (m *ConfigDashboard) openEditor(key, buffer, suffix string) tea.Cmd {
	path, err := writeEditorBuffer(buffer, suffix)
	if err != nil {
		m.failure = fmt.Sprintf("Could not open an editor buffer for %s: %v", key, err)
		return nil
	}
	m.failure = ""
	return m.editor(path, func(err error) tea.Msg {
		return configEditorDoneMsg{key: key, path: path, suffix: suffix, err: err}
	})
}

// editorReturned decides what the text the human handed back means: nothing at
// all, a value to store, or a problem to go back and fix.
func (m *ConfigDashboard) editorReturned(msg configEditorDoneMsg) tea.Cmd {
	defer func() { _ = os.Remove(msg.path) }()
	if msg.err != nil {
		m.failure = fmt.Sprintf("Editor for %s: %v", msg.key, msg.err)
		return nil
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.failure = fmt.Sprintf("Could not read the edited buffer for %s: %v", msg.key, err)
		return nil
	}
	// Pop's notes come back out here rather than at each reader: for a config key
	// they are TOML comments the parser would drop anyway, but a prose row's
	// buffer *is* the value, so a note that survived would be written into the
	// human's own document.
	buffer := stripEditorNotes(string(data))
	// An empty buffer is a cancel, not a deletion: leaving nothing behind is how
	// a human backs out of an edit, and ctrl+x is how they remove an override
	// (ADR-0202 decision 7).
	if strings.TrimSpace(buffer) == "" {
		return nil
	}
	problem, err := m.writer.Store(msg.key, buffer)
	switch {
	case err != nil:
		m.failure = fmt.Sprintf("Could not store the override for %s: %v", msg.key, err)
		return nil
	case problem != "":
		// A file pop wrote itself must never be the source of a finding, so the
		// human goes back to the buffer with the problem on top of it rather
		// than having it written and complained about later.
		return m.openEditor(msg.key, editorBufferWithProblem(problem, buffer), msg.suffix)
	}
	m.refresh()
	return nil
}

// refresh re-resolves every row after a write, so the marker and the provenance
// line tell the truth the moment the editor closes. The highlight stays on the
// key it was on.
func (m *ConfigDashboard) refresh() {
	// Every caller has just written the layer, which is exactly what a host has to
	// re-read for.
	m.wrote = true
	// A successful action reaches this point, so any stale failure row from an
	// earlier act is no longer true and must not survive it.
	m.failure = ""
	rows, err := m.writer.Rows()
	if err != nil {
		m.failure = fmt.Sprintf("Wrote the override, but could not re-read the config: %v", err)
		return
	}
	m.rows = contestedFirst(rows)
	m.filtered = m.matchingRows()
	m.list.ReplaceItems(m.filtered)
}

// configEditorNote prefixes every line pop writes into the buffer. It is a TOML
// comment, so a human who leaves it alone changes nothing, and it is distinct
// enough that stripping pop's notes never touches a comment they wrote.
const configEditorNote = "# pop: "

// editorBufferWithProblem puts the problem above the text that caused it and
// says what the two ways out are. Previous notes are dropped, so a second
// mistake reads as one problem rather than a growing pile.
func editorBufferWithProblem(problem, buffer string) string {
	var b strings.Builder
	b.WriteString(configEditorNote + "this value was not stored.\n")
	for _, line := range strings.Split(strings.TrimRight(problem, "\n"), "\n") {
		b.WriteString(configEditorNote + line + "\n")
	}
	b.WriteString(configEditorNote + "Fix the value below, or empty this buffer to leave the override as it was.\n")
	b.WriteString(stripEditorNotes(buffer))
	return b.String()
}

// stripEditorNotes removes the lines pop wrote into the buffer, leaving the
// human's own text — including their own comments.
func stripEditorNotes(buffer string) string {
	lines := strings.Split(buffer, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, configEditorNote) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// ConfigEditorNote prefixes each line of text with the comment marker this
// component writes into an editor buffer and takes back out of it, so a caller
// seeding a buffer can put guidance in front of the human that storing will
// never keep. It is exported because the seed for a row belongs to whoever knows
// what the row holds, and only this package knows which lines are pop's.
func ConfigEditorNote(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString(configEditorNote + line + "\n")
	}
	return b.String()
}

// writeEditorBuffer stages the buffer as a temp file under the caller's suffix,
// so an editor that keys its syntax highlighting off the extension helps the
// human get the value right the first time — TOML for a config key, Markdown for
// a row whose value is prose.
func writeEditorBuffer(buffer, suffix string) (string, error) {
	if suffix == "" {
		suffix = ".toml"
	}
	f, err := os.CreateTemp("", "pop-override-*"+suffix)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if !strings.HasSuffix(buffer, "\n") {
		buffer += "\n"
	}
	if _, err := f.WriteString(buffer); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// execEditorInPlace runs $EDITOR on this program's own terminal: bubbletea
// releases the terminal, the editor takes it, and the program repaints when it
// exits. A popup host makes for a cramped editor window, which is the human's
// own layout choice — a component that behaved differently depending on how it
// was launched would be harder to reason about (ADR-0202 decision 13).
//
// The editor inherits pop's process group, so the terminal foreground never
// moves and the claim made when the program started still holds on return. What
// it does inherit is the program's own output writer, which is why a host whose
// stdout is a data channel has to run the program on another writer, exactly as
// the pickers already do.
func execEditorInPlace(path string, done tea.ExecCallback) tea.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	fields := strings.Fields(editor)
	return tea.ExecProcess(exec.Command(fields[0], append(fields[1:], path)...), done)
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
	entries := []HelpEntry{
		{"type", "Filter over key path and description"},
		{"↑/↓", "Move highlight"},
		{configOverrideMarker, "This key carries an override"},
		{configContestedMarker, "More than one layer sets this key — listed first"},
	}
	if m.writer != nil {
		entries = append(entries,
			HelpEntry{"Enter", "Edit this row's own layer in $EDITOR (empty buffer cancels)"},
			HelpEntry{"C-y", "Copy the source value down as the override"},
			HelpEntry{"C-x", "Remove the override, restoring the source"},
		)
	}
	return append(entries,
		HelpEntry{"Esc", "Close"},
		HelpEntry{"C-h", "Toggle this help"},
	)
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
	hints := "  type to filter · ↑/↓ move · esc close · C-h help"
	if m.writer != nil {
		hints = "  type to filter · ↑/↓ move · enter edit · C-y copy source · C-x remove · esc close · C-h help"
	}
	return Frame{
		Width:    m.viewWidth(),
		TermH:    m.viewHeight(),
		Header:   ScrollEdgeLine("  Config · what is in force here", m.viewWidth(), "↑", m.list.ScrollEdges().Above),
		InputBox: m.input.View(),
		Warnings: warnings,
		Hints:    hints,
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

// renderRow draws one list row: the two marker columns, the dotted path, and —
// on the second line, when there is one — the description dimmed.
func (m *ConfigDashboard) renderRow(row ConfigDashboardRow, state RowState) string {
	width := m.listWidth() - 2 // the list owns a two-column cursor prefix
	if state.LineIndex > 0 {
		if strings.TrimSpace(row.Desc) == "" {
			return ""
		}
		return dimStyle.Render(TruncateString("    "+row.Desc, width))
	}
	marker := " "
	if row.Overridden {
		marker = IndicatorStyle.Render(configOverrideMarker)
	}
	if row.Contested {
		marker = configContestedStyle.Render(configContestedMarker) + marker
	} else {
		marker = " " + marker
	}
	marker += " "
	text := TruncateString(row.Key, width-3)
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
		hint := "(no overridable keys)"
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

	// A prose row's preview is the caller's whole text: the layers that speak are
	// its provenance, so there is no separate line to name one.
	if row.Preview.prose() {
		add(plain, strings.TrimRight(row.Preview.Layers, "\n"))
		return clipPreview(lines, height)
	}

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

	return clipPreview(lines, height)
}

// clipPreview keeps the preview inside its pane and says that it did, so a
// reader knows the value continues past the last line they can see.
func clipPreview(lines []string, height int) []string {
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
func RunConfigDashboard(rows []ConfigDashboardRow, opts ConfigDashboardOpts, in io.Reader, out io.Writer) error {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	m := NewConfigDashboard(rows, opts)
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
	Up         key.Binding
	Down       key.Binding
	Edit       key.Binding
	CopySource key.Binding
	Remove     key.Binding
	Quit       key.Binding
}{
	// The query owns every printable key, so movement and the actions live on
	// the arrows and the ctrl chords the pickers already use.
	Up:         key.NewBinding(key.WithKeys("up", "ctrl+p")),
	Down:       key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Edit:       key.NewBinding(key.WithKeys("enter")),
	CopySource: key.NewBinding(key.WithKeys("ctrl+y")),
	Remove:     key.NewBinding(key.WithKeys("ctrl+x")),
	Quit:       key.NewBinding(key.WithKeys("esc", "ctrl+c")),
}
