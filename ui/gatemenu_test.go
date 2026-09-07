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

// alt+a opened the retired agent-override picker (ADR-0202 decision 5). It must
// now do nothing at all rather than quietly select an option: muscle memory for
// it points at a surface that no longer exists.
func TestGateMenuRetiredOverrideChordDoesNothing(t *testing.T) {
	m := NewGateMenu(sampleHITLSpec())
	before := StripANSI(m.ViewChoices())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	if cmd != nil {
		t.Fatal("alt+a must not quit the menu")
	}
	if m.Chosen() != "" {
		t.Fatalf("chosen = %q, want empty", m.Chosen())
	}
	if got := StripANSI(m.ViewChoices()); got != before {
		t.Fatalf("alt+a moved the menu:\n%s", got)
	}
	if strings.Contains(before, "A-a") {
		t.Fatalf("hint still offers the retired chord:\n%s", before)
	}
}

func TestGateMenuAssistsAppendsAttendedLabel(t *testing.T) {
	spec := GateMenuSpec{
		Headline:      "Assist",
		AttendedLabel: "claude · agent's own configuration",
		Items: []GateMenuItem{
			{Key: "1", Label: "Agent assistance (default)", Default: true, Assists: true},
			{Key: "0", Label: "Exit"},
		},
	}
	got := StripANSI(NewGateMenu(spec).ViewContent())
	if !strings.Contains(got, "1. Agent assistance (default) · claude · agent's own configuration") {
		t.Fatalf("missing attended label:\n%s", got)
	}
}

// The affordance is on the row that names the agent, and only where a choice
// exists (ADR-0264 decisions 3 and 4): with one usable entry the row is what it
// has always been, and a digit launches without the chooser ever appearing.
func TestGateMenuAttendedPickHintAndKey(t *testing.T) {
	spec := func(pickable bool) GateMenuSpec {
		return GateMenuSpec{
			Headline:         "Assist",
			AttendedLabel:    "Claude Usual · opus",
			AttendedPickable: pickable,
			Items: []GateMenuItem{
				{Key: "1", Label: "Agent assistance (default)", Default: true, Assists: true},
				{Key: "0", Label: "Exit"},
			},
		}
	}

	lonely := NewGateMenu(spec(false))
	if got := StripANSI(lonely.ViewChoices()); !strings.Contains(got, "1. Agent assistance (default) · Claude Usual · opus\n") {
		t.Fatalf("a list of one must render as it always did:\n%s", got)
	}
	if _, cmd := lonely.Update(tea.KeyPressMsg{Code: tea.KeyTab}); cmd != nil {
		t.Fatal("tab must do nothing with nothing to choose between")
	}
	if lonely.PickedAttended() {
		t.Fatal("tab asked for a chooser that is not on offer")
	}

	m := NewGateMenu(spec(true))
	if got := StripANSI(m.ViewChoices()); !strings.Contains(got, "· Claude Usual · opus · tab to change") {
		t.Fatalf("the row does not name the key that changes it:\n%s", got)
	}

	// A digit is still a launch, never a detour through the chooser.
	digit := NewGateMenu(spec(true))
	digit.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if digit.Chosen() != "1" || digit.PickedAttended() {
		t.Fatalf("1 chose %q, pick=%v; want 1 and no chooser", digit.Chosen(), digit.PickedAttended())
	}

	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab}); cmd == nil {
		t.Fatal("tab should close the menu asking for the chooser")
	}
	if !m.PickedAttended() || m.Chosen() != "" {
		t.Fatalf("pick=%v chosen=%q, want a pick request and no choice", m.PickedAttended(), m.Chosen())
	}
}

// A refusal from the override layer belongs in the frame the menu is drawn in,
// beside the row it is about, rather than on the stdout the frame repaints over.
func TestGateMenuNoticeRendersAboveTheChoices(t *testing.T) {
	m := NewGateMenu(GateMenuSpec{
		Notice: "Agent unchanged — the override file is read-only",
		Items:  []GateMenuItem{{Key: "1", Label: "Agent assistance (default)", Default: true, Assists: true}},
	})
	got := StripANSI(m.ViewChoices())
	if !strings.HasPrefix(strings.TrimSpace(got), "Agent unchanged — the override file is read-only") {
		t.Fatalf("the refusal is not above the choices:\n%s", got)
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

func TestGateMenuViewGoldenRoutineRefine(t *testing.T) {
	m := NewGateMenu(GateMenuSpec{
		Headline: `Refine routine "gate" — paused, schedule "every 6h", no runs yet`,
		Tone:     GateMenuToneDefault,
		Items: []GateMenuItem{
			{Key: "1", Label: "Agent session (default)", Default: true},
			{Key: "2", Label: "Fire test run", Aliases: []string{"fire"}},
			{Key: "3", Label: "View last report"},
			{Key: "4", Label: "Edit prompt"},
			{Key: "5", Label: "Edit schedule"},
			{Key: "6", Label: "Resume routine & exit", Aliases: []string{"resume"}},
			{Key: "0", Label: "Exit (stay paused)"},
		},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := StripANSI(m.ViewContent())
	for _, want := range []string{
		`Refine routine "gate"`,
		"1. Agent session (default)",
		"2. Fire test run",
		"3. View last report",
		"4. Edit prompt",
		"5. Edit schedule",
		"6. Resume routine & exit",
		"0. Exit (stay paused)",
		"enter select · digit jump",
		"▸",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
}

func TestGateMenuViewGoldenProjectRefine(t *testing.T) {
	m := NewGateMenu(GateMenuSpec{
		Headline: `Refine Project routine "project:audit" — manual-fire-only, no runs yet`,
		Tone:     GateMenuToneDefault,
		Items: []GateMenuItem{
			{Key: "1", Label: "Agent session (default)", Default: true},
			{Key: "2", Label: "Fire test run", Aliases: []string{"fire"}},
			{Key: "3", Label: "View last report"},
			{Key: "4", Label: "Edit prompt"},
			{Key: "0", Label: "Exit"},
		},
	})
	got := StripANSI(m.ViewContent())
	for _, want := range []string{
		`Refine Project routine "project:audit"`,
		"manual-fire-only",
		"1. Agent session (default)",
		"2. Fire test run",
		"4. Edit prompt",
		"0. Exit",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("view missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"Edit schedule", "Resume routine"} {
		if strings.Contains(got, absent) {
			t.Fatalf("project refine must not offer %q:\n%s", absent, got)
		}
	}
}

func TestGateMenuAliasSelectsItem(t *testing.T) {
	m := NewGateMenu(GateMenuSpec{
		Items: []GateMenuItem{
			{Key: "1", Label: "Agent session", Default: true},
			{Key: "2", Label: "Fire test run", Aliases: []string{"fire"}},
			{Key: "0", Label: "Exit"},
		},
	})
	_, cmd := m.Update(tea.KeyPressMsg{Text: "fire"})
	if cmd == nil {
		t.Fatal("alias should quit")
	}
	if m.Chosen() != "2" {
		t.Fatalf("chosen = %q, want 2", m.Chosen())
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

// A gate's preamble carries a whole task body, which can dwarf the pane. It
// must stay out of the repainting frame: an inline frame taller than the
// terminal cannot be redrawn in place, so every keypress appends another copy
// to scrollback and evicts the drain log above it.
func TestGateMenuFrameExcludesPreambleAndFitsThePane(t *testing.T) {
	spec := sampleHITLSpec()
	spec.Preamble = make([]string, 200)
	for i := range spec.Preamble {
		spec.Preamble[i] = fmt.Sprintf("body line %d", i)
	}

	m := NewGateMenu(spec)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	frame := StripANSI(m.ViewChoices())
	if strings.Contains(frame, "body line") || strings.Contains(frame, spec.Headline) {
		t.Fatalf("context leaked into the live frame:\n%s", frame)
	}
	if got := strings.Count(clampToPane(m.ViewChoices(), 24), "\n"); got > 24 {
		t.Fatalf("frame is %d lines in a 24-line pane", got)
	}

	// The context is still rendered, just above the frame and only once.
	ctx := StripANSI(m.ViewContext())
	if !strings.Contains(ctx, "body line 199") || !strings.Contains(ctx, spec.Headline) {
		t.Fatalf("context lost the preamble or headline:\n%s", ctx)
	}
	// The non-TTY line path and the golden tests still see one whole render.
	if whole := StripANSI(m.ViewContent()); !strings.Contains(whole, "body line 0") ||
		!strings.Contains(whole, "2. Complete task") {
		t.Fatalf("ViewContent should still be context+choices:\n%s", whole)
	}
}
