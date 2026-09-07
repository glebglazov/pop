package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func attendedEntries() []AttendedAgentEntry {
	return []AttendedAgentEntry{
		{Cmd: "claude --model opus", Label: "Claude Usual · opus"},
		{Cmd: "cursor", Label: "Cursor"},
	}
}

func TestAttendedAgentPickerPicksByDigitAndLeavesUnchangedOnEsc(t *testing.T) {
	p := NewAttendedAgentPicker(attendedEntries())
	got := StripANSI(p.ViewContent())
	for _, want := range []string{"Attended agent", "1. Claude Usual · opus", "2. Cursor", "esc leave unchanged"} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}

	p.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if p.Choice() == nil || p.Choice().Cmd != "cursor" {
		t.Fatalf("choice = %+v, want cursor", p.Choice())
	}

	// Esc is the way out with nothing chosen, whatever the highlight is on.
	q := NewAttendedAgentPicker(attendedEntries())
	q.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	q.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	if !q.Done() {
		t.Fatal("esc must close the picker")
	}
	if q.Choice() != nil {
		t.Fatalf("esc chose %+v, want nothing", q.Choice())
	}
}

// The picker is hosted from a gate, where stdout carries the drain log, and from
// a dashboard, where it may be a data channel. It takes the stricter vow of the
// two (ADR-0202 decision 11, ADR-0264 decision 6): nothing reaches stdout on any
// path, the one with no terminal to pick with included.
func TestAttendedAgentPickerWritesNothingToStdout(t *testing.T) {
	var picked *AttendedAgentEntry
	var runErr error
	out := captureStdout(t, func() {
		p := NewAttendedAgentPicker(nil)
		p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		p.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
		_ = p.View()

		p = NewAttendedAgentPicker(attendedEntries())
		p.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
		_ = p.View()
		p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

		// No terminal: there is nothing to pick with, and the list is not printed
		// as a consolation.
		picked, runErr = RunAttendedAgentPicker(attendedEntries(), strings.NewReader("1\n"), nil, nil)
	})
	if out != "" {
		t.Fatalf("the picker wrote to stdout:\n%q", out)
	}
	if runErr != nil {
		t.Fatalf("RunAttendedAgentPicker error: %v", runErr)
	}
	if picked != nil {
		t.Fatalf("a non-TTY run picked %+v, want nothing", picked)
	}
}
