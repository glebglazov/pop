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

func TestNamePromptDefaultsToDerivedName(t *testing.T) {
	m := newNamePrompt("Name it", "feature-x")
	if got := string(m.value); got != "feature-x" {
		t.Errorf("initial buffer = %q, want %q", got, "feature-x")
	}
	if m.cursor != len("feature-x") {
		t.Errorf("cursor = %d, want %d (end)", m.cursor, len("feature-x"))
	}
	// Submitting as-is keeps the default.
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.submitted || m.cancelled {
		t.Fatalf("Enter should submit: submitted=%v cancelled=%v", m.submitted, m.cancelled)
	}
	if got := m.result(); got != "feature-x" {
		t.Errorf("result = %q, want %q", got, "feature-x")
	}
}

func TestNamePromptPrintableRunes(t *testing.T) {
	m := newNamePrompt("h", "")
	for _, r := range "abc" {
		m = m.send(typeRune(r))
	}
	if got := string(m.value); got != "abc" {
		t.Errorf("buffer = %q, want %q", got, "abc")
	}
	if m.cursor != 3 {
		t.Errorf("cursor = %d, want 3", m.cursor)
	}
}

func TestNamePromptBackspace(t *testing.T) {
	m := newNamePrompt("h", "abc")
	m = m.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := string(m.value); got != "ab" {
		t.Errorf("after backspace buffer = %q, want %q", got, "ab")
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
	// Backspace at start of an empty buffer is a no-op.
	empty := newNamePrompt("h", "")
	empty = empty.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(empty.value) != 0 || empty.cursor != 0 {
		t.Errorf("backspace on empty buffer changed state: value=%q cursor=%d", string(empty.value), empty.cursor)
	}
}

func TestNamePromptCursorMovementAndInsert(t *testing.T) {
	m := newNamePrompt("h", "ac")
	// Move left once: cursor between a and c.
	m = m.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.cursor != 1 {
		t.Fatalf("cursor after left = %d, want 1", m.cursor)
	}
	// Insert 'b' in the middle.
	m = m.send(typeRune('b'))
	if got := string(m.value); got != "abc" {
		t.Errorf("mid-insert buffer = %q, want %q", got, "abc")
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.cursor)
	}
	// Right past end is clamped.
	m = m.send(tea.KeyPressMsg{Code: tea.KeyRight})
	m = m.send(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.cursor != 3 {
		t.Errorf("cursor after over-right = %d, want 3", m.cursor)
	}
	// Left clamps at 0.
	for i := 0; i < 5; i++ {
		m = m.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if m.cursor != 0 {
		t.Errorf("cursor after over-left = %d, want 0", m.cursor)
	}
}

func TestNamePromptHomeEnd(t *testing.T) {
	m := newNamePrompt("h", "abc")
	m = m.send(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.cursor != 0 {
		t.Errorf("cursor after home = %d, want 0", m.cursor)
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.cursor != 3 {
		t.Errorf("cursor after end = %d, want 3", m.cursor)
	}
}

func TestNamePromptEmptySubmitUsesDefault(t *testing.T) {
	m := newNamePrompt("h", "feature-x")
	// Clear the buffer, then submit.
	m = m.send(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if len(m.value) != 0 {
		t.Fatalf("ctrl+u should clear buffer, got %q", string(m.value))
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
	m := newNamePrompt("h", "feature-x")
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
	m := newNamePrompt("h", "feature-x")
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.cancelled {
		t.Error("Esc should cancel")
	}
	if m.submitted {
		t.Error("Esc must not mark submitted")
	}
}

func TestNamePromptCtrlCCancels(t *testing.T) {
	m := newNamePrompt("h", "feature-x")
	m = m.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.cancelled {
		t.Error("ctrl+c should cancel")
	}
}
