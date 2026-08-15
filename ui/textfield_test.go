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

func TestTextFieldDeleteWordBack(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "one two three")
	f.Update(ctrlKey('w'))
	if f.Value() != "one two " || f.Cursor() != 8 {
		t.Errorf("after ctrl+w: value=%q cursor=%d, want %q/8", f.Value(), f.Cursor(), "one two ")
	}

	// Trailing spaces behind the cursor go with the word.
	sp := NewTextField()
	typeInto(&sp, "café 本語   ")
	sp.Update(ctrlKey('w'))
	if sp.Value() != "café " || sp.Cursor() != 5 {
		t.Errorf("after ctrl+w over spaces: value=%q cursor=%d, want %q/5", sp.Value(), sp.Cursor(), "café ")
	}

	// Mid-buffer: only the text behind the cursor is removed.
	mid := NewTextField()
	typeInto(&mid, "alpha beta")
	mid.SetCursor(9)
	mid.Update(ctrlKey('w'))
	if mid.Value() != "alpha a" || mid.Cursor() != 6 {
		t.Errorf("mid-buffer ctrl+w: value=%q cursor=%d, want %q/6", mid.Value(), mid.Cursor(), "alpha a")
	}

	// At position 0 it is a no-op.
	edge := NewTextField()
	typeInto(&edge, "abc")
	edge.SetCursor(0)
	edge.Update(ctrlKey('w'))
	if edge.Value() != "abc" || edge.Cursor() != 0 {
		t.Errorf("ctrl+w at 0: value=%q cursor=%d, want %q/0", edge.Value(), edge.Cursor(), "abc")
	}
}

func TestTextFieldWordMotion(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "one two three")
	f.Update(altKey('b'))
	if f.Cursor() != 8 || f.Value() != "one two three" {
		t.Errorf("after alt+b: cursor=%d value=%q, want 8 and buffer unchanged", f.Cursor(), f.Value())
	}
	f.Update(altKey('b'))
	if f.Cursor() != 4 {
		t.Errorf("after second alt+b: cursor=%d, want 4", f.Cursor())
	}
	f.Update(altKey('f'))
	if f.Cursor() != 7 || f.Value() != "one two three" {
		t.Errorf("after alt+f: cursor=%d value=%q, want 7 and buffer unchanged", f.Cursor(), f.Value())
	}
	f.Update(altKey('f'))
	if f.Cursor() != 13 {
		t.Errorf("after second alt+f: cursor=%d, want 13", f.Cursor())
	}
	// alt+f at the end and alt+b at the start stay put.
	f.Update(altKey('f'))
	if f.Cursor() != 13 {
		t.Errorf("alt+f at end: cursor=%d, want 13", f.Cursor())
	}
	f.SetCursor(0)
	f.Update(altKey('b'))
	if f.Cursor() != 0 {
		t.Errorf("alt+b at start: cursor=%d, want 0", f.Cursor())
	}
}

func TestTextFieldDeleteWordForward(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "one two three")
	f.SetCursor(4)
	f.Update(altKey('d'))
	if f.Value() != "one  three" || f.Cursor() != 4 {
		t.Errorf("after alt+d: value=%q cursor=%d, want %q/4", f.Value(), f.Cursor(), "one  three")
	}

	// Leading spaces ahead of the cursor go with the word.
	sp := NewTextField()
	typeInto(&sp, "本語   café")
	sp.SetCursor(2)
	sp.Update(altKey('d'))
	if sp.Value() != "本語" || sp.Cursor() != 2 {
		t.Errorf("alt+d over spaces: value=%q cursor=%d, want %q/2", sp.Value(), sp.Cursor(), "本語")
	}

	// At the end of the buffer it is a no-op.
	edge := NewTextField()
	typeInto(&edge, "abc")
	edge.Update(altKey('d'))
	if edge.Value() != "abc" || edge.Cursor() != 3 {
		t.Errorf("alt+d at end: value=%q cursor=%d, want %q/3", edge.Value(), edge.Cursor(), "abc")
	}
}

func TestTextFieldKillLine(t *testing.T) {
	f := NewTextField()
	typeInto(&f, "café 日本語")
	f.SetCursor(3)
	f.Update(ctrlKey('k'))
	if f.Value() != "caf" || f.Cursor() != 3 {
		t.Errorf("after ctrl+k: value=%q cursor=%d, want %q/3", f.Value(), f.Cursor(), "caf")
	}
	// At the end it is a no-op.
	f.Update(ctrlKey('k'))
	if f.Value() != "caf" || f.Cursor() != 3 {
		t.Errorf("ctrl+k at end: value=%q cursor=%d, want %q/3", f.Value(), f.Cursor(), "caf")
	}
	// From position 0 it empties the buffer.
	f.SetCursor(0)
	f.Update(ctrlKey('k'))
	if f.Value() != "" || f.Cursor() != 0 {
		t.Errorf("ctrl+k at 0: value=%q cursor=%d, want empty/0", f.Value(), f.Cursor())
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
