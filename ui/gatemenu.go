package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/internal/tty"
)

// GateMenuTone colors the menu headline.
type GateMenuTone int

const (
	// GateMenuToneDefault is the cyan/info headline (Assist, fold conflict).
	GateMenuToneDefault GateMenuTone = iota
	// GateMenuToneWarn is the yellow headline (HITL, interrupt).
	GateMenuToneWarn
	// GateMenuToneError is the red headline (Failed, Verify-failed).
	GateMenuToneError
)

// GateMenuItem is one numbered choice in a gate menu.
type GateMenuItem struct {
	// Key is the digit (or "0") that selects this item immediately.
	Key string
	// Label is the primary line, e.g. "Get agent assistance (default)".
	Label string
	// Details are optional indented lines under the label (invocation display).
	Details []string
	// Default marks the Enter / empty-line choice.
	Default bool
	// Aliases are optional word forms accepted as the same choice (e.g. "fire"
	// for key "2"). Matched case-insensitively on digit-jump and the non-TTY
	// line path.
	Aliases []string
	// Assists marks the item whose launch uses the attended entry. When set,
	// ViewContent appends Spec.AttendedLabel (ADR-0196 decision 9).
	Assists bool
}

// GateMenuSpec describes one inline gate menu frame.
type GateMenuSpec struct {
	Headline string
	Tone     GateMenuTone
	// Preamble is free-form context above the choices (task body, findings,
	// waiter count, remediation review). Each string is one rendered line.
	Preamble []string
	Items    []GateMenuItem
	// Footnote is an optional dim line under the choices (e.g. force-quit hint).
	Footnote string
	// AttendedLabel is the shared one-line render of the attended entry the
	// Assists item will launch (FormatAgentEntry). Empty skips the append.
	AttendedLabel string
}

// GateMenuResult is the outcome of RunGateMenu.
type GateMenuResult struct {
	// Key is the selected item's Key. Empty when ForceQuit is set.
	Key string
	// ForceQuit is set when a second interrupt arrived while the menu was up
	// (interrupt gate) or the tea program was killed by that signal.
	ForceQuit bool
}

// GateMenuRunConfig holds optional RunGateMenu knobs.
type GateMenuRunConfig struct {
	// Interrupt delivers a second SIGINT for the interrupt gate's force-quit.
	// Nil means the menu does not watch for force-quit.
	Interrupt <-chan os.Signal
	// LineReader, when set, is used for the non-TTY line path so a shared
	// bufio.Reader across gates does not lose queued input. When nil, a fresh
	// tty.Reader wraps In.
	LineReader LineReader
	// Warn reports terminal-foreground diagnostics (ClaimForeground surprises).
	Warn func(string, ...any)
}

// LineReader is the line-oriented read the non-TTY path needs. *tty.Reader
// satisfies it; tests can stub it.
type LineReader interface {
	ReadLine(warn func(string, ...any)) (string, error)
}

// GateMenu is the bubbletea model behind RunGateMenu. Exported so tests can
// drive Update/View without starting a Program.
type GateMenu struct {
	spec     GateMenuSpec
	cursor   int
	width    int
	height   int
	showHelp bool
	chosen   string
	quit     bool
}

// NewGateMenu builds a menu model with the cursor on the default item (or the
// first item when none is marked default).
func NewGateMenu(spec GateMenuSpec) *GateMenu {
	cursor := 0
	for i, it := range spec.Items {
		if it.Default {
			cursor = i
			break
		}
	}
	return &GateMenu{spec: spec, cursor: cursor}
}

// Chosen returns the selected key after the model has quit, or "".
func (m *GateMenu) Chosen() string { return m.chosen }

// Init implements tea.Model.
func (m *GateMenu) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *GateMenu) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if ToggleHelp(&m.showHelp, msg) {
			return m, nil
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *GateMenu) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, gateMenuKeys.Up):
		m.moveCursor(-1)
		return nil
	case key.Matches(msg, gateMenuKeys.Down):
		m.moveCursor(1)
		return nil
	case key.Matches(msg, gateMenuKeys.Submit):
		return m.selectIndex(m.cursor)
	case key.Matches(msg, gateMenuKeys.Cancel):
		// Esc / ctrl+c maps to the "0" exit item when present; otherwise quits
		// with no choice (callers treat empty as exit).
		if idx := m.indexForKey("0"); idx >= 0 {
			return m.selectIndex(idx)
		}
		m.quit = true
		return tea.Quit
	}

	// Digit / letter aliases: match an item Key, or the historical q/quit/exit /
	// continue aliases the text menus accepted.
	s := strings.ToLower(strings.TrimSpace(msg.String()))
	if s == "" {
		return nil
	}
	if s == "q" || s == "quit" || s == "exit" {
		if idx := m.indexForKey("0"); idx >= 0 {
			return m.selectIndex(idx)
		}
	}
	if s == "c" || s == "continue" {
		// Interrupt gate: "c"/"continue" were aliases for option 1.
		if idx := m.indexForKey("1"); idx >= 0 {
			return m.selectIndex(idx)
		}
	}
	if idx := m.indexForKey(s); idx >= 0 {
		return m.selectIndex(idx)
	}
	if idx := m.indexForAlias(s); idx >= 0 {
		return m.selectIndex(idx)
	}
	return nil
}

func (m *GateMenu) moveCursor(delta int) {
	n := len(m.spec.Items)
	if n == 0 {
		return
	}
	m.cursor = (m.cursor + delta + n) % n
}

func (m *GateMenu) indexForKey(key string) int {
	for i, it := range m.spec.Items {
		if it.Key == key {
			return i
		}
	}
	return -1
}

func (m *GateMenu) indexForAlias(alias string) int {
	for i, it := range m.spec.Items {
		for _, a := range it.Aliases {
			if strings.EqualFold(a, alias) {
				return i
			}
		}
	}
	return -1
}

func (m *GateMenu) selectIndex(i int) tea.Cmd {
	if i < 0 || i >= len(m.spec.Items) {
		return nil
	}
	m.chosen = m.spec.Items[i].Key
	m.cursor = i
	m.quit = true
	return tea.Quit
}

// View implements tea.Model. AltScreen stays false so the drain log above the
// menu remains visible (ADR-0196 decision 1). Only the choices are in the frame
// — the context was printed above it before the program started — and the frame
// is clamped to the pane so it can always be repainted in place.
func (m *GateMenu) View() tea.View {
	// The help overlay sizes itself and is left alone; the clamp guards the
	// choices, which is where a spec's content reaches the frame.
	content := m.viewHelp()
	if !m.showHelp {
		content = clampToPane(m.ViewChoices(), m.height)
	}
	v := tea.NewView(content)
	v.AltScreen = false
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (m *GateMenu) viewHelp() string {
	height := m.height
	if height <= 0 {
		height = 12
	}
	return RenderHelpOverlay("Help · Gate", m.helpEntries(), m.width, height)
}

func (m *GateMenu) helpEntries() []HelpEntry {
	return []HelpEntry{
		{"1-9 / 0", "Select that option"},
		{"Enter", "Select the highlighted (default) option"},
		{"↑/↓ j/k", "Move highlight"},
		{"Esc", "Exit (option 0)"},
		{"C-h", "Toggle this help"},
	}
}

// ViewContent renders the whole menu — context above choices — without the help
// overlay. The non-TTY line path prints exactly this once, and golden tests
// assert against it. The interactive path splits the two halves apart; see
// ViewContext.
func (m *GateMenu) ViewContent() string {
	return m.ViewContext() + m.ViewChoices()
}

// ViewContext is the static half: the headline and the preamble, which carry a
// whole task body, findings block or remediation history. It is printed once,
// above the live frame, and never repainted. A repainting frame taller than the
// pane cannot reposition above the viewport top, so the inline renderer appends
// a fresh copy of it on every keypress — thousands of scrollback lines per
// keystroke, evicting the drain log this gate exists to keep readable.
func (m *GateMenu) ViewContext() string {
	var b strings.Builder
	if m.spec.Headline != "" {
		b.WriteString(m.headlineStyle().Render(m.spec.Headline))
		b.WriteString("\n")
	}
	for _, line := range m.spec.Preamble {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(m.spec.Preamble) > 0 || m.spec.Headline != "" {
		b.WriteString("\n")
	}
	return b.String()
}

// ViewChoices is the live half: the items, their details, the footnote and the
// hint line. Its height is bounded by the number of options a gate offers.
func (m *GateMenu) ViewChoices() string {
	var b strings.Builder
	for i, it := range m.spec.Items {
		prefix := "  "
		itemLabel := it.Label
		if it.Assists && m.spec.AttendedLabel != "" {
			itemLabel = it.Label + " · " + m.spec.AttendedLabel
		}
		label := fmt.Sprintf("%s. %s", it.Key, itemLabel)
		if i == m.cursor {
			prefix = IndicatorStyle().Render("▸ ")
			label = selectedGateItemStyle().Render(label)
		} else {
			prefix = "  "
		}
		b.WriteString(prefix)
		b.WriteString(label)
		b.WriteString("\n")
		for _, d := range it.Details {
			b.WriteString("     ")
			b.WriteString(hintStyle().Render(d))
			b.WriteString("\n")
		}
	}

	if m.spec.Footnote != "" {
		b.WriteString(hintStyle().Render("  " + m.spec.Footnote))
		b.WriteString("\n")
	}
	b.WriteString(hintStyle().Render("  enter select · digit jump · ↑/↓ move · C-h help"))
	b.WriteString("\n")
	return b.String()
}

// clampToPane is the last-resort guarantee that an inline frame never exceeds
// the pane it repaints into. Nothing should reach it — the context lives above
// the frame and a gate offers a handful of options — but the failure mode it
// guards is a scrollback flood, not a clipped line, so it is worth the check.
func clampToPane(content string, height int) string {
	if height <= 0 {
		return content
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) <= height {
		return content
	}
	kept := append([]string{}, lines[:height-1]...)
	kept = append(kept, hintStyle().Render("  … clipped to fit the pane"))
	return strings.Join(kept, "\n") + "\n"
}

func (m *GateMenu) headlineStyle() lipgloss.Style {
	switch m.spec.Tone {
	case GateMenuToneWarn:
		return gateWarnStyle()
	case GateMenuToneError:
		return gateErrorStyle()
	default:
		return headerStyle()
	}
}

func gateWarnStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorWarning()).Bold(true) }
func gateErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorAttention()).Bold(true)
}
func selectedGateItemStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorAccent()).Bold(true)
}

type gateMenuKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Submit key.Binding
	Cancel key.Binding
}

var gateMenuKeys = gateMenuKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k", "ctrl+p")),
	Down:   key.NewBinding(key.WithKeys("down", "j", "ctrl+n")),
	Submit: key.NewBinding(key.WithKeys("enter")),
	Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
}

// RunGateMenu draws an inline (no altscreen) gate menu and returns the chosen
// item key. On a real terminal it prints the context once and runs a bubbletea
// Program over the choices; otherwise it prints the same ViewContent once and
// reads a line — one renderer, two input paths (ADR-0196 decision 2). A
// non-promptable caller never reaches here.
func RunGateMenu(spec GateMenuSpec, in io.Reader, out io.Writer, cfg GateMenuRunConfig) (GateMenuResult, error) {
	if out == nil {
		out = os.Stdout
	}
	if in == nil {
		in = os.Stdin
	}
	m := NewGateMenu(spec)

	if fd, ok := tty.TerminalFd(in); ok {
		claimTerminal(fd, cfg.Warn)
		// The context scrolls into history like ordinary output; only the
		// choices below it are a repainting frame.
		fmt.Fprint(out, m.ViewContext())
		return runGateMenuInteractive(m, in, out, cfg.Interrupt)
	}
	return runGateMenuLine(m, in, out, cfg)
}

func claimTerminal(fd int, warn func(string, ...any)) {
	if warn == nil {
		warn = func(string, ...any) {}
	}
	claim := tty.ClaimForeground(fd)
	switch {
	case claim.Owned && claim.Taken:
		warn("Terminal foreground was held by process group %d; took it back to prompt.", claim.Holder)
	case !claim.Owned:
		warn("Could not take the terminal foreground to prompt: %v", claim.Err)
	}
}

func runGateMenuInteractive(m *GateMenu, in io.Reader, out io.Writer, interrupt <-chan os.Signal) (GateMenuResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	forceQuit := make(chan struct{}, 1)
	if interrupt != nil {
		go func() {
			select {
			case <-interrupt:
				select {
				case forceQuit <- struct{}{}:
				default:
				}
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	final, err := RunProgram(m, in, out,
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)
	if err != nil {
		select {
		case <-forceQuit:
			return GateMenuResult{ForceQuit: true}, nil
		default:
		}
		if errors.Is(err, tea.ErrProgramKilled) && interrupt != nil {
			// Context cancelled by the interrupt watcher.
			select {
			case <-forceQuit:
				return GateMenuResult{ForceQuit: true}, nil
			default:
				return GateMenuResult{ForceQuit: true}, nil
			}
		}
		return GateMenuResult{}, err
	}
	fm, ok := final.(*GateMenu)
	if !ok || fm == nil {
		return GateMenuResult{}, fmt.Errorf("gate menu: unexpected model type %T", final)
	}
	return GateMenuResult{Key: fm.chosen}, nil
}

func runGateMenuLine(m *GateMenu, in io.Reader, out io.Writer, cfg GateMenuRunConfig) (GateMenuResult, error) {
	fmt.Fprintln(out)
	fmt.Fprint(out, m.ViewContent())

	reader := cfg.LineReader
	if reader == nil {
		reader = tty.NewReader(in)
	}

	for {
		if cfg.Interrupt != nil {
			select {
			case <-cfg.Interrupt:
				return GateMenuResult{ForceQuit: true}, nil
			default:
			}
		}

		type lineRes struct {
			answer string
			err    error
		}
		lineCh := make(chan lineRes, 1)
		go func() {
			answer, err := reader.ReadLine(cfg.Warn)
			lineCh <- lineRes{answer: answer, err: err}
		}()

		var answer string
		var readErr error
		if cfg.Interrupt != nil {
			select {
			case <-cfg.Interrupt:
				return GateMenuResult{ForceQuit: true}, nil
			case res := <-lineCh:
				answer, readErr = res.answer, res.err
			}
		} else {
			res := <-lineCh
			answer, readErr = res.answer, res.err
		}

		if readErr != nil && readErr != io.EOF {
			return GateMenuResult{}, readErr
		}
		choice := strings.ToLower(strings.TrimSpace(strings.TrimRight(answer, "\r\n")))
		if readErr == io.EOF && choice == "" {
			// Closed input → exit item when present, else empty.
			if idx := m.indexForKey("0"); idx >= 0 {
				return GateMenuResult{Key: "0"}, nil
			}
			return GateMenuResult{}, nil
		}
		if choice == "" {
			for _, it := range m.spec.Items {
				if it.Default {
					return GateMenuResult{Key: it.Key}, nil
				}
			}
			if len(m.spec.Items) > 0 {
				return GateMenuResult{Key: m.spec.Items[0].Key}, nil
			}
			return GateMenuResult{}, nil
		}
		if choice == "q" || choice == "quit" || choice == "exit" {
			choice = "0"
		}
		if choice == "c" || choice == "continue" {
			if m.indexForKey("1") >= 0 {
				choice = "1"
			}
		}
		if idx := m.indexForAlias(choice); idx >= 0 {
			choice = m.spec.Items[idx].Key
		}
		if m.indexForKey(choice) >= 0 {
			return GateMenuResult{Key: choice}, nil
		}
		// Invalid — re-prompt with the same frame (one renderer).
		fmt.Fprintln(out, invalidGateChoiceHint(m.spec.Items))
		fmt.Fprint(out, m.ViewContent())
	}
}

func invalidGateChoiceHint(items []GateMenuItem) string {
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.Key)
	}
	if len(keys) == 0 {
		return "Choose a listed option."
	}
	if len(keys) == 1 {
		return fmt.Sprintf("Choose %s.", keys[0])
	}
	return fmt.Sprintf("Choose %s, or %s.", strings.Join(keys[:len(keys)-1], ", "), keys[len(keys)-1])
}
