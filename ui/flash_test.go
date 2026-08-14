package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestFlashTakesTheHintLineThenGivesItBack drives one message through its whole
// life: shown on the bottom line with the hints stood down, then — on the
// command the flash itself handed out — gone again, with the hints back on the
// same line and the body budget unmoved. It waits out the real FlashDuration
// because those three seconds are the behaviour under test (ADR-0204).
func TestFlashTakesTheHintLineThenGivesItBack(t *testing.T) {
	var flash Flash
	flash.Set("copied my-set")

	timer := flash.Timer()
	if timer == nil {
		t.Fatal("Set armed no expiry timer")
	}
	if flash.Timer() != nil {
		t.Fatal("one message handed out two timers")
	}
	expired := make(chan tea.Msg, 1)
	go func() { expired <- timer() }()

	const hints = "j/k move · y copy name · h/esc quit"
	frame := Frame{Width: 40, TermH: 10, Header: "Work · 1 here", Hints: hints, Flash: flash}
	budget := frame.BodyHeight(10)
	shown := strings.Split(frame.Render("  row"), "\n")

	if !strings.Contains(shown[len(shown)-1], "copied my-set") {
		t.Fatalf("bottom line = %q, want the flash message", shown[len(shown)-1])
	}
	if strings.Contains(strings.Join(shown, "\n"), "y copy name") {
		t.Fatalf("hints still painted under a live flash:\n%s", strings.Join(shown, "\n"))
	}

	var msg tea.Msg
	select {
	case msg = <-expired:
	case <-time.After(FlashDuration + 2*time.Second):
		t.Fatal("the flash never expired")
	}
	expiry, ok := msg.(FlashExpiredMsg)
	if !ok {
		t.Fatalf("timer produced %T, want FlashExpiredMsg", msg)
	}
	flash.Expired(expiry)
	if flash.Text() != "" {
		t.Fatalf("flash still reads %q after its expiry", flash.Text())
	}

	frame.Flash = flash
	after := strings.Split(frame.Render("  row"), "\n")
	if !strings.Contains(after[len(after)-1], "y copy name") {
		t.Fatalf("bottom line = %q, want the hints back", after[len(after)-1])
	}
	if len(after) != len(shown) || frame.BodyHeight(10) != budget {
		t.Fatalf("layout shifted on expiry: %d lines (was %d), body %d (was %d)",
			len(after), len(shown), frame.BodyHeight(10), budget)
	}
}

// TestFlashKeepsTheNewerMessageOverAStaleTimer: two messages in quick
// succession leave two timers in flight, and the first one coming back must not
// take the second one's message off the screen early.
func TestFlashKeepsTheNewerMessageOverAStaleTimer(t *testing.T) {
	var flash Flash
	flash.Set("copied my-set")
	stale := FlashExpiredMsg{token: flash.token}
	flash.Set("copied other-set")

	flash.Expired(stale)

	if flash.Text() != "copied other-set" {
		t.Fatalf("flash = %q, want the newer message to survive the stale timer", flash.Text())
	}
}
