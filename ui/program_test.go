package ui

import (
	"bytes"
	"context"
	"image/color"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/glebglazov/pop/internal/tty"
)

// runProgramStub is a caller's model: RunProgram must hand this very value back,
// because half the call sites type-assert the final model to their own type.
type runProgramStub struct {
	saw []tea.Msg
}

func (m *runProgramStub) Init() tea.Cmd { return tea.Quit }

func (m *runProgramStub) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.saw = append(m.saw, msg)
	return m, nil
}

func (m *runProgramStub) View() tea.View { return tea.NewView("") }

// The chokepoint is transparent: what a caller passed in is what it gets back,
// so every existing assertion on a returned model still holds.
func TestRunProgramReturnsTheCallersOwnModel(t *testing.T) {
	model := &runProgramStub{}
	final, err := RunProgram(model, strings.NewReader(""), io.Discard, tea.WithoutSignalHandler())
	if err != nil {
		t.Fatalf("RunProgram: %v", err)
	}
	same, ok := final.(*runProgramStub)
	if !ok {
		t.Fatalf("RunProgram returned %T, want the caller's own *runProgramStub", final)
	}
	if same != model {
		t.Fatal("RunProgram returned a different *runProgramStub than the caller passed in")
	}
}

// The notification is the doorbell, never the answer. It reports the operating
// system's setting, which a pinned terminal theme overrides, so it may only
// provoke a fresh background-colour query — and the query's answer is what every
// surface then reads (ADR-0230).
func TestColorSchemeNotificationAsksRatherThanAnswers(t *testing.T) {
	pinAppearance(t, AppearanceDark)

	inner := &runProgramStub{}
	next, cmd := appearanceModel{inner: inner}.Update(uv.LightColorSchemeEvent{})
	if got := CurrentAppearance(); got != AppearanceDark {
		t.Fatalf("the notification was taken as the answer: appearance is %v, want dark", got)
	}
	if cmd == nil {
		t.Fatal("a colour-scheme notification provoked no background-colour query")
	}
	if msg := cmd(); msg != tea.RequestBackgroundColor() {
		t.Fatalf("notification produced %#v, want a background-colour request", msg)
	}
	if len(inner.saw) != 1 {
		t.Fatalf("the wrapper swallowed the event: inner model saw %v", inner.saw)
	}

	// The reply is the answer, and it lands where every surface reads it.
	white := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if _, cmd = next.Update(tea.BackgroundColorMsg{Color: white}); cmd != nil {
		t.Fatalf("the background-colour reply produced an unexpected command %#v", cmd())
	}
	if got := CurrentAppearance(); got != AppearanceLight {
		t.Fatalf("appearance after a white background = %v, want light", got)
	}
}

// A terminal left with the notification enabled keeps reporting colour-scheme
// changes to whatever runs next, which a shell prints as noise. The mode is
// therefore turned off around the program rather than inside it, so a program
// that ends in an error turns it off too.
func TestRunProgramDisablesTheNotificationAfterAFailedProgram(t *testing.T) {
	pinAppearance(t, AppearancePlain)

	master, slavePath, err := tty.OpenPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	defer master.Close()
	slave, err := os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", slavePath, err)
	}

	var mu sync.Mutex
	var written bytes.Buffer
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buf := make([]byte, 1024)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			written.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	// A context that is already cancelled is the shortest ordinary failure: Run
	// returns an error without the program ever reaching a frame.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunProgram(&runProgramStub{}, strings.NewReader(""), slave, tea.WithContext(ctx), tea.WithoutSignalHandler()); err == nil {
		t.Fatal("a cancelled context ran the program to a clean exit, want an error")
	}
	slave.Close()
	<-drained

	mu.Lock()
	out := written.String()
	mu.Unlock()
	on := strings.Index(out, ansi.SetModeLightDark)
	off := strings.Index(out, ansi.ResetModeLightDark)
	if on < 0 {
		t.Fatalf("colour-scheme notification was never enabled:\n%q", out)
	}
	if off < 0 {
		t.Fatalf("colour-scheme notification was left enabled after a failed program:\n%q", out)
	}
	if off < on {
		t.Fatalf("the notification was disabled before it was enabled:\n%q", out)
	}
}

// pinAppearance holds the process-wide appearance for one test and puts back
// what it found, so a test that drives the chokepoint cannot leak a palette into
// the next one.
func pinAppearance(t *testing.T, a Appearance) {
	t.Helper()
	was := CurrentAppearance()
	t.Cleanup(func() { setCurrentAppearance(was) })
	setCurrentAppearance(a)
}
