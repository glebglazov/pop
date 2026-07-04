package ui

import (
	"fmt"
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

// --- Help overlay tests ---

func TestNamePromptHelp_OpensOnCtrlH(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Error("showHelp should be true after C-h")
	}
}

func TestNamePromptHelp_SecondCtrlHDismisses(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Fatal("showHelp should be true after first C-h")
	}
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if m.showHelp {
		t.Error("showHelp should be false after second C-h")
	}
}

func TestNamePromptHelp_EscDismisses(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Fatal("showHelp should be true after C-h")
	}
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showHelp {
		t.Error("showHelp should be false after Esc in help mode")
	}
}

func TestNamePromptHelp_SwallowsKeysWhileOpen(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Fatal("showHelp should be true after C-h")
	}

	// Typing should be swallowed
	m = m.send(typeRune('a'))
	if m.field.Value() != "" {
		t.Error("typing in help mode should not modify buffer")
	}
	if !m.showHelp {
		t.Error("showHelp should remain true after swallowed key")
	}

	// Enter should be swallowed (not submit)
	m = m.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.submitted {
		t.Error("Enter in help mode should not submit")
	}
}

func TestNamePromptHelp_ContentRendered(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "base-branch")
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})

	view := m.viewHelp()
	if !containsSubstring(view, "Help") {
		t.Error("help view should contain Help title")
	}
	if !containsSubstring(view, "Name") {
		t.Error("help view should contain 'Name' subtitle")
	}
	if !containsSubstring(view, "Confirm and submit") {
		t.Error("help view should contain submit description")
	}
	if !containsSubstring(view, "Cancel") {
		t.Error("help view should contain cancel description")
	}
	if !containsSubstring(view, "C-h toggle") {
		t.Error("help view should contain 'C-h toggle' footer")
	}
	if !containsSubstring(view, "Esc close") {
		t.Error("help view should contain 'Esc close' footer")
	}
}

func TestNamePromptHelp_HintIncludesCtrlH(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "base-branch")
	m.width = 60
	m.height = 20

	view := fmt.Sprint(m.viewNormal())
	if !containsSubstring(view, "C-h help") {
		t.Error("hint should include 'C-h help'")
	}
}

func TestNamePromptHelp_F1DoesNothing(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")
	m = m.send(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.showHelp {
		t.Error("F1 should not open help")
	}
	// Cancel should not have been triggered
	if m.cancelled {
		t.Error("F1 should not cancel")
	}
}

func TestNamePromptHelp_TextEntryStillWorksAfterDismiss(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "")

	// Open and close help
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})

	if m.showHelp {
		t.Fatal("showHelp should be false after dismiss")
	}

	// Typing should work again
	m = m.send(typeRune('a'))
	if m.field.Value() != "a" {
		t.Errorf("buffer should contain 'a', got %q", m.field.Value())
	}
}

func TestNamePromptHelp_ViewSwitchesToHelp(t *testing.T) {
	m := newNamePrompt("h", "feature-x", "base-branch")
	m.width = 60
	m.height = 20

	// Normal view
	view := fmt.Sprint(m.View())
	if !containsSubstring(view, "enter confirm") {
		t.Error("normal view should contain hint")
	}
	if containsSubstring(view, "C-h toggle") {
		t.Error("normal view should not contain help footer")
	}

	// Help view
	m = m.send(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	view = fmt.Sprint(m.View())
	if !containsSubstring(view, "C-h toggle") {
		t.Error("help view should contain help footer")
	}
}
