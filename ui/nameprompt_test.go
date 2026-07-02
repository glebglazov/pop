package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// send feeds a key message to the model and returns the updated model.
func (m *namePromptModel) send(msg tea.KeyPressMsg) *namePromptModel {
	updated, _ := m.Update(msg)
	return updated.(*namePromptModel)
}

func typeRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestNamePromptStartsEmptyAndDefaultsOnSubmit(t *testing.T) {
	m := newNamePrompt("Name it", "feature-x", "master")
	// The field starts empty (typed name = the NEW branch), not pre-filled.
	if got := m.field.Value(); got != "" {
		t.Errorf("initial buffer = %q, want empty", got)
	}
	if m.field.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.field.cursor)
	}
	// Submitting the empty field falls back to the branch-derived default.
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.submitted || m.cancelled {
		t.Fatalf("Enter should submit: submitted=%v cancelled=%v", m.submitted, m.cancelled)
	}
	if got := m.result(); got != "feature-x" {
		t.Errorf("result = %q, want %q", got, "feature-x")
	}
}

func TestNamePromptPrintableRunes(t *testing.T) {
	m := newNamePrompt("h", "", "")
	for _, r := range "abc" {
		m = m.send(typeRune(r))
	}
	if got := m.field.Value(); got != "abc" {
		t.Errorf("buffer = %q, want %q", got, "abc")
	}
	if m.field.cursor != 3 {
		t.Errorf("cursor = %d, want 3", m.field.cursor)
	}
}

func TestNamePromptBackspace(t *testing.T) {
	m := newNamePrompt("h", "", "")
	for _, r := range "abc" {
		m = m.send(typeRune(r))
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.field.Value(); got != "ab" {
		t.Errorf("after backspace buffer = %q, want %q", got, "ab")
	}
	if m.field.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.field.cursor)
	}
	// Backspace at start of an empty buffer is a no-op.
	empty := newNamePrompt("h", "", "")
	empty = empty.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(empty.field.value) != 0 || empty.field.cursor != 0 {
		t.Errorf("backspace on empty buffer changed state: value=%q cursor=%d", empty.field.Value(), empty.field.cursor)
	}
}

func TestNamePromptCursorMovementAndInsert(t *testing.T) {
	m := newNamePrompt("h", "", "")
	for _, r := range "ac" {
		m = m.send(typeRune(r))
	}
	// Move left once: cursor between a and c.
	m = m.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.field.cursor != 1 {
		t.Fatalf("cursor after left = %d, want 1", m.field.cursor)
	}
	// Insert 'b' in the middle.
	m = m.send(typeRune('b'))
	if got := m.field.Value(); got != "abc" {
		t.Errorf("mid-insert buffer = %q, want %q", got, "abc")
	}
	if m.field.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.field.cursor)
	}
	// Right past end is clamped.
	m = m.send(tea.KeyPressMsg{Code: tea.KeyRight})
	m = m.send(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.field.cursor != 3 {
		t.Errorf("cursor after over-right = %d, want 3", m.field.cursor)
	}
	// Left clamps at 0.
	for i := 0; i < 5; i++ {
		m = m.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if m.field.cursor != 0 {
		t.Errorf("cursor after over-left = %d, want 0", m.field.cursor)
	}
}

func TestNamePromptHomeEnd(t *testing.T) {
	m := newNamePrompt("h", "", "")
	for _, r := range "abc" {
		m = m.send(typeRune(r))
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.field.cursor != 0 {
		t.Errorf("cursor after home = %d, want 0", m.field.cursor)
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.field.cursor != 3 {
		t.Errorf("cursor after end = %d, want 3", m.field.cursor)
	}
}

func TestNamePromptEmptySubmitUsesDefault(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	// Clear the buffer, then submit.
	m = m.send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if len(m.field.value) != 0 {
		t.Fatalf("ctrl+u should clear buffer, got %q", m.field.Value())
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.submitted {
		t.Fatal("Enter should submit")
	}
	if got := m.result(); got != "feature-x" {
		t.Errorf("empty submit result = %q, want default %q", got, "feature-x")
	}
}

func TestNamePromptEditedSubmit(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	for _, r := range "custom" {
		m = m.send(typeRune(r))
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.result(); got != "custom" {
		t.Errorf("edited submit result = %q, want %q", got, "custom")
	}
}

func TestNamePromptEscCancels(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.cancelled {
		t.Error("Esc should cancel")
	}
	if m.submitted {
		t.Error("Esc must not mark submitted")
	}
}

func TestNamePromptCtrlCCancels(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.cancelled {
		t.Error("ctrl+c should cancel")
	}
}
