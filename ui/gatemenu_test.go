package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sampleHITLSpec() GateMenuSpec {
	return GateMenuSpec{
		Headline: "Human-blocked: demo/01-hitl needs human work before the set can continue.",
		Tone:     GateMenuToneWarn,
		Preamble: []string{
			"--- 01-hitl.md ---",
			"## Acceptance criteria",
			"",
			"- [ ] ok",
			"------------------",
		},
		Items: []GateMenuItem{
			{Key: "1", Label: "Get agent assistance (default)", Details: []string{"claude --permission-mode auto <HITL assistance prompt>"}, Default: true},
			{Key: "2", Label: "Complete task"},
			{Key: "3", Label: "Defer task"},
			{Key: "4", Label: "Open a shell in the checkout"},
			{Key: "0", Label: "Exit"},
		},
	}
}

func TestGateMenuViewGoldenHITL(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := StripANSI(m.ViewContent())

	for _, want := range []string{
		"Human-blocked: demo/01-hitl needs human work before the set can continue.",
		"--- 01-hitl.md ---",
		"- [ ] ok",
		"1. Get agent assistance (default)",
		"claude --permission-mode auto <HITL assistance prompt>",
		"2. Complete task",
		"3. Defer task",
		"4. Open a shell in the checkout",
		"0. Exit",
		"enter select · digit jump",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
	// Cursor starts on the default item; indicator present once.
	if !strings.Contains(got, "▸") {
		t.Fatalf("default cursor indicator missing:\n%s", got)
	}
}

func TestGateMenuViewGoldenVerifyFailed(t *testing.T) {
	m := NewGateMenu(GateMenuSpec{
		Headline: "Verify-failed: demo did not clear the Verifier and needs a human decision.",
		Tone:     GateMenuToneError,
		Preamble: []string{
			"  Findings:",
			"    still flaky on CI",
		},
		Items: []GateMenuItem{
			{Key: "1", Label: "Accept (record a human-authored PASS)"},
			{Key: "2", Label: "Remediate (spawn a fix task)"},
			{Key: "3", Label: "Agent assistance", Details: []string{"claude --permission-mode auto <prompt>"}},
			{Key: "4", Label: "Open a shell in the checkout"},
			{Key: "0", Label: "Exit", Default: true},
		},
	})
	got := StripANSI(m.ViewContent())
	for _, want := range []string{
		"Verify-failed: demo",
		"Findings:",
		"still flaky on CI",
		"1. Accept (record a human-authored PASS)",
		"2. Remediate (spawn a fix task)",
		"3. Agent assistance",
		"0. Exit",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
}

func TestGateMenuViewGoldenInterruptFootnote(t *testing.T) {
	m := NewGateMenu(GateMenuSpec{
		Headline: "Interrupted: demo/01-a was stopped mid-run.",
		Tone:     GateMenuToneWarn,
		Items: []GateMenuItem{
			{Key: "1", Label: "Continue draining (default)", Default: true},
			{Key: "2", Label: "Agent assistance"},
			{Key: "3", Label: "Open a shell in the checkout"},
			{Key: "0", Label: "Exit"},
		},
		Footnote: "(press Ctrl-C again to force-quit)",
	})
	got := StripANSI(m.ViewContent())
	if !strings.Contains(got, "(press Ctrl-C again to force-quit)") {
		t.Fatalf("footnote missing:\n%s", got)
	}
}

func TestGateMenuDigitSelectsAndQuits(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	_, cmd := m.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	if cmd == nil {
		t.Fatal("digit should quit")
	}
	if m.Chosen() != "4" {
		t.Fatalf("chosen = %q, want 4", m.Chosen())
	}
}

func TestGateMenuEnterSelectsDefault(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should quit")
	}
	if m.Chosen() != "1" {
		t.Fatalf("chosen = %q, want 1 (default)", m.Chosen())
	}
}

func TestGateMenuArrowMovesCursorThenEnter(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should quit")
	}
	if m.Chosen() != "3" {
		t.Fatalf("chosen = %q, want 3", m.Chosen())
	}
}

func TestGateMenuEscSelectsExit(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc should quit")
	}
	if m.Chosen() != "0" {
		t.Fatalf("chosen = %q, want 0", m.Chosen())
	}
}

func TestGateMenuHelpToggle(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Fatal("C-h should open help")
	}
	view := fmt.Sprint(m.View())
	if !strings.Contains(StripANSI(view), "Help · Gate") {
		t.Fatalf("help overlay missing:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showHelp {
		t.Fatal("Esc should close help")
	}
}

func TestGateMenuInvalidDigitIgnored(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	_, cmd := m.Update(tea.KeyPressMsg{Code: '9', Text: "9"})
	if cmd != nil {
		t.Fatal("invalid digit must not quit")
	}
	if m.Chosen() != "" {
		t.Fatalf("chosen = %q, want empty", m.Chosen())
	}
}

func TestInvalidGateChoiceHint(t *testing.T) {
	got := invalidGateChoiceHint([]GateMenuItem{
		{Key: "1"}, {Key: "2"}, {Key: "3"}, {Key: "0"},
	})
	if got != "Choose 1, 2, 3, or 0." {
		t.Fatalf("hint = %q", got)
	}
}
