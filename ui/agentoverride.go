package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/internal/tty"
)

// AgentOverrideGroup is one Work agent group in the override picker (ADR-0196).
type AgentOverrideGroup struct {
	// ID is the group key (implement, verify, routine, attended).
	ID string
	// Label is what the group row shows.
	Label string
	// Entries are the group's agent rows in declared order.
	Entries []AgentOverrideEntry
}

// AgentOverrideEntry is one selectable agent row.
type AgentOverrideEntry struct {
	// ID is the entry's command — what a Promote call stores.
	ID string
	// Label is the shared one-line render of the entry.
	Label string
}

// AgentOverrideChoice is a confirmed pick: which group and which entry cmd.
type AgentOverrideChoice struct {
	Group string
	Cmd   string
}

// AgentOverridePicker is the two-level numeric agent-override picker
// (ADR-0196 decisions 5–6). Level one lists groups; a digit opens that group's
// entries; a digit there applies and closes. Enter always moves forward on the
// highlighted row — open at the group level, pick at the entry level — so the
// same key carries a human through both steps. Esc and 0 step back one level,
// and leave the picker unchanged when there is no level to step back to.
//
// Alt+a is inert while this picker is open — callers must not re-open it on
// that chord from the picker's own Update.
type AgentOverridePicker struct {
	groups []AgentOverrideGroup
	// level 0 = groups, 1 = entries of groupIdx.
	level    int
	groupIdx int
	cursor   int
	width    int
	height   int
	showHelp bool

	choice    *AgentOverrideChoice
	cancelled bool
	done      bool
}

// NewAgentOverridePicker builds a picker over groups. Empty groups are still
// listed so the four Work groups keep their declared shape.
func NewAgentOverridePicker(groups []AgentOverrideGroup) *AgentOverridePicker {
	return &AgentOverridePicker{groups: groups}
}

// Choice returns the confirmed pick, or nil when the picker exited unchanged.
func (p *AgentOverridePicker) Choice() *AgentOverrideChoice { return p.choice }

// Done reports whether the picker has closed (choice or cancel).
func (p *AgentOverridePicker) Done() bool { return p.done }

// Cancelled reports whether Enter/esc closed the picker without a choice.
func (p *AgentOverridePicker) Cancelled() bool { return p.cancelled }

// Init implements tea.Model.
func (p *AgentOverridePicker) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (p *AgentOverridePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, nil
	case tea.KeyPressMsg:
		if ToggleHelp(&p.showHelp, msg) {
			return p, nil
		}
		if p.showHelp {
			return p, nil
		}
		return p, p.handleKey(msg)
	}
	return p, nil
}

func (p *AgentOverridePicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	// alt+a is inert inside the picker itself (ADR-0196 decision 5).
	if IsAgentOverrideKey(msg) {
		return nil
	}
	switch {
	case key.Matches(msg, agentOverrideKeys.Up):
		p.moveCursor(-1)
		return nil
	case key.Matches(msg, agentOverrideKeys.Down):
		p.moveCursor(1)
		return nil
	case key.Matches(msg, agentOverrideKeys.Submit):
		return p.activateCursor()
	case key.Matches(msg, agentOverrideKeys.Quit):
		return p.exitUnchanged()
	case key.Matches(msg, agentOverrideKeys.Back):
		return p.stepBack()
	}

	s := strings.TrimSpace(msg.String())
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return p.activateDigit(int(s[0] - '0'))
	}
	if s == "0" {
		return p.stepBack()
	}
	return nil
}

// stepBack returns to the group list from an open group, and leaves the picker
// unchanged when the group list is already what's showing.
func (p *AgentOverridePicker) stepBack() tea.Cmd {
	if p.level == 1 {
		p.level = 0
		p.cursor = p.groupIdx
		return nil
	}
	return p.exitUnchanged()
}

func (p *AgentOverridePicker) items() []string {
	if p.level == 0 {
		labels := make([]string, len(p.groups))
		for i, g := range p.groups {
			labels[i] = g.Label
		}
		return labels
	}
	if p.groupIdx < 0 || p.groupIdx >= len(p.groups) {
		return nil
	}
	entries := p.groups[p.groupIdx].Entries
	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = e.Label
	}
	return labels
}

func (p *AgentOverridePicker) moveCursor(delta int) {
	n := len(p.items())
	if n == 0 {
		return
	}
	p.cursor = (p.cursor + delta + n) % n
}

func (p *AgentOverridePicker) activateDigit(digit int) tea.Cmd {
	items := p.items()
	if digit < 1 || digit > 9 || digit > len(items) {
		return nil
	}
	return p.activateIndex(digit - 1)
}

func (p *AgentOverridePicker) activateCursor() tea.Cmd {
	// Enter commits the highlighted row at either level, so a human who never
	// reaches for the digits still gets group → entry → done. Past nine entries
	// it is the only way to reach row 10+ (ADR-0196 decision 6).
	return p.activateIndex(p.cursor)
}

func (p *AgentOverridePicker) activateIndex(idx int) tea.Cmd {
	items := p.items()
	if idx < 0 || idx >= len(items) {
		return nil
	}
	if p.level == 0 {
		p.groupIdx = idx
		p.level = 1
		p.cursor = 0
		return nil
	}
	group := p.groups[p.groupIdx]
	if idx >= len(group.Entries) {
		return nil
	}
	entry := group.Entries[idx]
	p.choice = &AgentOverrideChoice{Group: group.ID, Cmd: entry.ID}
	p.done = true
	return tea.Quit
}

func (p *AgentOverridePicker) exitUnchanged() tea.Cmd {
	p.cancelled = true
	p.done = true
	p.choice = nil
	return tea.Quit
}

// View implements tea.Model. AltScreen stays false so a gate's drain log above
// the picker remains visible; the dashboard embeds ViewContent into its own
// altscreen frame instead.
func (p *AgentOverridePicker) View() tea.View {
	content := p.viewHelp()
	if !p.showHelp {
		content = clampToPane(p.ViewContent(), p.height)
	}
	v := tea.NewView(content)
	v.AltScreen = false
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (p *AgentOverridePicker) viewHelp() string {
	height := p.height
	if height <= 0 {
		height = 12
	}
	return RenderHelpOverlay("Help · Agent override", p.helpEntries(), p.width, height)
}

func (p *AgentOverridePicker) helpEntries() []HelpEntry {
	return []HelpEntry{
		{"1-9", "Open / pick that row"},
		{"Enter", "Open / pick the highlighted row"},
		{"↑/↓ j/k", "Move highlight"},
		{"Esc / 0", "Back a level, or exit unchanged from the group list"},
		{"C-h", "Toggle this help"},
	}
}

// ViewContent renders the picker body. Dashboard overlays and golden tests share
// this exact render with the interactive View.
func (p *AgentOverridePicker) ViewContent() string {
	var b strings.Builder
	title := "Agent override"
	if p.level == 1 && p.groupIdx >= 0 && p.groupIdx < len(p.groups) {
		title = "Agent override · " + p.groups[p.groupIdx].Label
	}
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	items := p.items()
	if len(items) == 0 {
		b.WriteString(HintStyle.Render("  (no entries)"))
		b.WriteString("\n")
	}
	for i, label := range items {
		prefix := "  "
		key := " "
		if i < 9 {
			key = fmt.Sprintf("%d", i+1)
		}
		line := fmt.Sprintf("%s. %s", key, label)
		if i == p.cursor {
			prefix = IndicatorStyle.Render("▸ ")
			line = selectedGateItemStyle.Render(line)
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(HintStyle.Render(p.hint()))
	b.WriteString("\n")
	return b.String()
}

// hint names what esc does here, which is the half of the key map that changes
// between the two levels.
func (p *AgentOverridePicker) hint() string {
	if p.level == 1 {
		return "  1-9 jump · enter pick · esc back"
	}
	return "  1-9 jump · enter open · esc leave unchanged"
}

// IsAgentOverrideKey reports whether msg is the ADR-0196 override chord (alt+a).
func IsAgentOverrideKey(msg tea.KeyPressMsg) bool {
	if msg.Code != 'a' && msg.Code != 'A' {
		return false
	}
	return msg.Mod.Contains(tea.ModAlt) && !msg.Mod.Contains(tea.ModCtrl)
}

// RunAgentOverridePicker runs the two-level override picker as an inline tea
// program (no altscreen). Returns the confirmed choice, or nil when the human
// left unchanged. On a non-TTY input it prints ViewContent once and returns
// nil — digit-line picking is not offered; gates never reach here without a
// promptable terminal.
func RunAgentOverridePicker(groups []AgentOverrideGroup, in io.Reader, out io.Writer, warn func(string, ...any)) (*AgentOverrideChoice, error) {
	if out == nil {
		out = os.Stdout
	}
	if in == nil {
		in = os.Stdin
	}
	p := NewAgentOverridePicker(groups)
	if fd, ok := tty.TerminalFd(in); ok {
		claimTerminal(fd, warn)
		return runAgentOverridePickerInteractive(p, in, out)
	}
	fmt.Fprintln(out)
	fmt.Fprint(out, p.ViewContent())
	return nil, nil
}

func runAgentOverridePickerInteractive(p *AgentOverridePicker, in io.Reader, out io.Writer) (*AgentOverrideChoice, error) {
	opts := []tea.ProgramOption{
		tea.WithInput(in),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
	}
	final, err := tea.NewProgram(p, opts...).Run()
	if err != nil {
		return nil, err
	}
	fp, ok := final.(*AgentOverridePicker)
	if !ok || fp == nil {
		return nil, fmt.Errorf("agent override picker: unexpected model type %T", final)
	}
	return fp.Choice(), nil
}

var agentOverrideKeys = struct {
	Up     key.Binding
	Down   key.Binding
	Submit key.Binding
	Quit   key.Binding
	Back   key.Binding
}{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Submit: key.NewBinding(key.WithKeys("enter")),
	// ctrl+c abandons the whole picker from either level; esc is a level step.
	Quit: key.NewBinding(key.WithKeys("ctrl+c")),
	Back: key.NewBinding(key.WithKeys("esc", "left", "h")),
}
