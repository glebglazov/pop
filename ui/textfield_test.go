package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// key builds a printable-rune key press.
func fieldRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// typeInto feeds each rune of s to the field.
func typeInto(f *TextField, s string) {
	for _, r := range s {
		f.Update(fieldRune(r))
	}
}

func TestTextFieldStartsEmptyFocused(t *testing.T) {
	f := NewTextField()
	if f.Value() != "" {
		t.Errorf("Value = %q, want empty", f.Value())
	}
	if f.Cursor() != 0 {
		t.Errorf("Cursor = %d, want 0", f.Cursor())
	}
	if !f.focused {
		t.Error("NewTextField should be focused")
	}
}

func TestTextFieldInsertMultibyte(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "café日本") // ASCII + accented + CJK
	if f.Value() != "café日本" {
		t.Errorf("Value = %q, want %q", f.Value(), "café日本")
	}
	// Cursor counts runes, not bytes.
	if f.Cursor() != 6 {
		t.Errorf("Cursor = %d, want 6 (rune count)", f.Cursor())
	}
}

func TestTextFieldMidInsertMultibyte(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "aé")
	// Move left once: cursor sits between 'a' and 'é'.
	f.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if f.Cursor() != 1 {
		t.Fatalf("Cursor after left = %d, want 1", f.Cursor())
	}
	typeInto(&f, "本")
	if f.Value() != "a本é" {
		t.Errorf("mid-insert Value = %q, want %q", f.Value(), "a本é")
	}
	if f.Cursor() != 2 {
		t.Errorf("Cursor = %d, want 2", f.Cursor())
	}
}

func TestTextFieldBackspaceMultibyte(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "café")
	f.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if f.Value() != "caf" {
		t.Errorf("after backspace Value = %q, want %q", f.Value(), "caf")
	}
	if f.Cursor() != 3 {
		t.Errorf("Cursor = %d, want 3", f.Cursor())
	}

	// Backspace at start of an empty buffer is a no-op.
	empty := NewTextField()
	empty.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if empty.Value() != "" || empty.Cursor() != 0 {
		t.Errorf("backspace on empty changed state: value=%q cursor=%d", empty.Value(), empty.Cursor())
	}
}

func TestTextFieldBackspaceMidBufferMultibyte(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "日本語")
	// Move cursor between 日 and 本, then backspace deletes 日.
	f.SetCursor(1)
	f.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if f.Value() != "本語" {
		t.Errorf("Value = %q, want %q", f.Value(), "本語")
	}
	if f.Cursor() != 0 {
		t.Errorf("Cursor = %d, want 0", f.Cursor())
	}
}

func TestTextFieldCursorMovementClamps(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "abc")
	// Right past end clamps at len.
	for i := 0; i < 3; i++ {
		f.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if f.Cursor() != 3 {
		t.Errorf("Cursor after over-right = %d, want 3", f.Cursor())
	}
	// Left clamps at 0.
	for i := 0; i < 5; i++ {
		f.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if f.Cursor() != 0 {
		t.Errorf("Cursor after over-left = %d, want 0", f.Cursor())
	}
}

func TestTextFieldEmacsCursorKeys(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "héllo")
	// ctrl+b moves left, ctrl+f moves right.
	f.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if f.Cursor() != 4 {
		t.Errorf("Cursor after ctrl+b = %d, want 4", f.Cursor())
	}
	f.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if f.Cursor() != 5 {
		t.Errorf("Cursor after ctrl+f = %d, want 5", f.Cursor())
	}
}

func TestTextFieldHomeEnd(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "café本")
	f.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if f.Cursor() != 0 {
		t.Errorf("Cursor after home = %d, want 0", f.Cursor())
	}
	f.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if f.Cursor() != 5 {
		t.Errorf("Cursor after end = %d, want 5", f.Cursor())
	}

	// ctrl+a = home, ctrl+e = end.
	f.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if f.Cursor() != 0 {
		t.Errorf("Cursor after ctrl+a = %d, want 0", f.Cursor())
	}
	f.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if f.Cursor() != 5 {
		t.Errorf("Cursor after ctrl+e = %d, want 5", f.Cursor())
	}
}

func TestTextFieldClear(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "日本語")
	f.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if f.Value() != "" {
		t.Errorf("ctrl+u should clear, got %q", f.Value())
	}
	if f.Cursor() != 0 {
		t.Errorf("Cursor after clear = %d, want 0", f.Cursor())
	}
}

func TestTextFieldSetValueClampsCursor(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "abcdef")
	if f.Cursor() != 6 {
		t.Fatalf("Cursor = %d, want 6", f.Cursor())
	}
	// Shrinking the buffer clamps the cursor into the new bounds.
	f.SetValue("ab")
	if f.Cursor() != 2 {
		t.Errorf("Cursor after shrink = %d, want 2", f.Cursor())
	}
	// SetCursor clamps out-of-range requests.
	f.SetCursor(99)
	if f.Cursor() != 2 {
		t.Errorf("Cursor after over-set = %d, want 2", f.Cursor())
	}
	f.SetCursor(-5)
	if f.Cursor() != 0 {
		t.Errorf("Cursor after negative set = %d, want 0", f.Cursor())
	}
}

func TestTextFieldViewHasPromptGlyph(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "hi")
	view := StripANSI(f.View())
	if len(view) < 2 || view[:len("❯ ")] != "❯ " {
		t.Errorf("View = %q, want prefix %q", view, "❯ ")
	}
}

func TestTextFieldIgnoresNonKeyMsg(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "abc")
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if f.Value() != "abc" || f.Cursor() != 3 {
		t.Errorf("non-key msg mutated field: value=%q cursor=%d", f.Value(), f.Cursor())
	}
}
