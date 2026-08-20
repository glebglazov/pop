package ui

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// currentAppearance is the Terminal appearance every pop surface renders in.
// It is written only by RunProgram — once before a program starts and again
// whenever the terminal answers a fresh background-colour query — and read by
// the styles and the renderings that select a palette from it. Styles read it,
// they never capture it: a value copied into a package var at init is the same
// bug this whole seam exists to fix (ADR-0230).
//
// It is an atomic because the read side includes the renderer's goroutine while
// the write side is the program's event loop.
var currentAppearance atomic.Int32

// CurrentAppearance is the Terminal appearance in force. Outside a TUI, and
// before the first one starts, it is plain — the answer that is legible on any
// background.
func CurrentAppearance() Appearance {
	return Appearance(currentAppearance.Load())
}

func setCurrentAppearance(a Appearance) {
	currentAppearance.Store(int32(a))
}

// RunProgram is the one way pop starts a Bubble Tea program (ADR-0230). It
// enables the terminal's colour-scheme notification, resolves the Terminal
// appearance before the program takes stdin, keeps that appearance current for
// as long as the program runs, disables the notification on the way out, and
// hands back the caller's own final model so no call site changes shape.
//
// A nil in or out means "whatever Bubble Tea would use", which is the tty for
// input and stdout for output; pop's standard streams are what the appearance
// query then rides on. Extra options are appended after pop's own, so a caller
// that needs a context or its own signal handling still gets it.
func RunProgram(model tea.Model, in io.Reader, out io.Writer, opts ...tea.ProgramOption) (tea.Model, error) {
	queryIn, queryOut := programTerminal(in, out)
	setCurrentAppearance(ResolveAppearance(queryIn, queryOut))
	defer notifyColorScheme(queryOut)()

	programOpts := make([]tea.ProgramOption, 0, len(opts)+2)
	if in != nil {
		programOpts = append(programOpts, tea.WithInput(in))
	}
	if out != nil {
		programOpts = append(programOpts, tea.WithOutput(out))
	}
	programOpts = append(programOpts, opts...)

	final, err := tea.NewProgram(appearanceModel{inner: model}, programOpts...).Run()
	if wrapped, ok := final.(appearanceModel); ok {
		final = wrapped.inner
	}
	return final, err
}

// programTerminal names the files the appearance query and the mode switches
// ride on. A caller that named nothing is running on pop's own standard
// streams; a caller that named something which is not a file — a test's buffer,
// a pipe — named no terminal at all, and gets no query and no mode switch.
func programTerminal(in io.Reader, out io.Writer) (queryIn, queryOut *os.File) {
	if in == nil {
		queryIn = os.Stdin
	} else if f, ok := in.(*os.File); ok {
		queryIn = f
	}
	if out == nil {
		queryOut = os.Stdout
	} else if f, ok := out.(*os.File); ok {
		queryOut = f
	}
	return queryIn, queryOut
}

// notifyColorScheme turns DEC private mode 2031 on and returns the function
// that turns it off. Bubble Tea offers no command for the mode, so pop writes
// the sequences itself, around the program rather than inside it.
func notifyColorScheme(out *os.File) func() {
	if out == nil || !term.IsTerminal(int(out.Fd())) {
		return func() {}
	}
	fmt.Fprint(out, ansi.SetModeLightDark)
	return func() { fmt.Fprint(out, ansi.ResetModeLightDark) }
}

// appearanceModel is the thin outer model RunProgram wraps every caller's model
// in. It owns one fact and forwards everything else untouched, so the model
// underneath handles no appearance message at all.
type appearanceModel struct {
	inner tea.Model
}

func (m appearanceModel) Init() tea.Cmd {
	return m.inner.Init()
}

func (m appearanceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var mine tea.Cmd
	switch msg := msg.(type) {
	case uv.DarkColorSchemeEvent, uv.LightColorSchemeEvent:
		// The doorbell, never the answer: the notification reports the
		// operating system's setting, and a light OS with a pinned dark
		// terminal would report the wrong thing. Ask the terminal instead.
		mine = tea.RequestBackgroundColor
	case tea.BackgroundColorMsg:
		setCurrentAppearance(AppearanceOf(msg.Color))
	}
	inner, cmd := m.inner.Update(msg)
	m.inner = inner
	if mine == nil {
		return m, cmd
	}
	return m, tea.Batch(cmd, mine)
}

func (m appearanceModel) View() tea.View {
	return m.inner.View()
}
