package ui

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// FlashDuration is how long a flash message holds a view's bottom line before
// the hints come back. It is the only place the interval is written: every
// Frame-based view expires its messages after the same three seconds, so a
// message's lifetime does not depend on which surface showed it (ADR-0204).
const FlashDuration = 3 * time.Second

// FlashExpiredMsg is the expiry of one flash message, delivered by the command
// Flash.Timer handed out when the message was set. A model forwards it to every
// Flash it owns; the token says which message it belongs to, so a stale timer
// arriving after a newer message replaced its own is ignored rather than
// blanking the line early.
type FlashExpiredMsg struct{ token uint64 }

// flashTokens numbers messages across the whole process. Program-wide
// uniqueness is what lets a model fan one expiry out to all of its flashes
// without any of them mistaking another's timer for its own.
var flashTokens atomic.Uint64

func flashStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorAccent()) }

// Flash is transient action feedback with a lifetime. It owns the message, the
// three-second expiry and the command that fires it, so a view says what just
// happened and nothing has to remember to take the words away again.
//
// The zero value is a flash showing nothing. Set records a message and arms a
// timer; Timer hands that timer to bubbletea once; Expired retires the message
// when the timer comes back.
type Flash struct {
	text  string
	token uint64
	// armed is a message whose timer has not been handed out yet. Set cannot
	// return the command itself — most callers are deep in a verb with no way to
	// reach bubbletea — so the command waits here for the model's one drain point.
	armed bool
}

// Set makes text the live message and arms its expiry. An empty text is the
// absence of a message: it clears the line now and arms nothing, which is what a
// caller with nothing to say passes.
func (f *Flash) Set(text string) {
	f.text = text
	if text == "" {
		f.token = 0
		f.armed = false
		return
	}
	f.token = flashTokens.Add(1)
	f.armed = true
}

// Text returns the live message, or "" when the flash is showing nothing.
func (f Flash) Text() string { return f.text }

// Timer hands the caller the command that expires the armed message, and nil
// when there is nothing newly armed. The model calls it once per update and
// batches the result into what it returns to bubbletea.
func (f *Flash) Timer() tea.Cmd {
	if !f.armed {
		return nil
	}
	f.armed = false
	token := f.token
	return tea.Tick(FlashDuration, func(time.Time) tea.Msg {
		return FlashExpiredMsg{token: token}
	})
}

// Expired retires the message the timer was set for. An expiry for any other
// message — a timer outlived by the message that replaced it — is dropped.
func (f *Flash) Expired(msg FlashExpiredMsg) {
	if msg.token == 0 || msg.token != f.token {
		return
	}
	f.text = ""
	f.token = 0
	f.armed = false
}

// Line renders the flash as a view's bottom line: accent-coloured and indented
// like the surface's other messages, or "" when nothing is showing. Frame paints
// it in the hints slot; views that compose their own footer call it directly, so
// a flash looks the same wherever it lands.
func (f Flash) Line() string {
	if f.text == "" {
		return ""
	}
	return flashStyle().Render("  " + f.text)
}
