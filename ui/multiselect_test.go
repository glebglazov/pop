package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyDown() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyDown} }
func keyUp() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyUp} }
func keySpace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: ' '} }
func keyEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func keyEsc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }

func TestMultiSelectCursorStartsOnFirstUnlocked(t *testing.T) {
	m := NewMultiSelect("pick", []MultiSelectItem{
		{Label: "done-1", Locked: true, LockedMark: "✓"},
		{Label: "done-2", Locked: true, LockedMark: "✓"},
		{Label: "open-1"},
		{Label: "open-2"},
	})
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want first unlocked (2)", m.cursor)
	}
}

func TestMultiSelectAllLockedCursorAtZero(t *testing.T) {
	m := NewMultiSelect("pick", []MultiSelectItem{
		{Label: "done-1", Locked: true, LockedMark: "✓"},
		{Label: "done-2", Locked: true, LockedMark: "✓"},
	})
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 when all locked", m.cursor)
	}
}

func TestMultiSelectToggleAndConfirm(t *testing.T) {
	m := NewMultiSelect("pick", []MultiSelectItem{
		{Label: "done-1", Locked: true, LockedMark: "✓"},
		{Label: "open-1"},
		{Label: "open-2"},
	})

	// cursor on index 1; check it, move down, check index 2.
	m.Update(keySpace())
	m.Update(keyDown())
	m.Update(keySpace())
	_, cmd := m.Update(keyEnter())
	if cmd == nil {
		t.Fatal("enter should quit")
	}

	res := m.Result()
	if !res.Confirmed {
		t.Fatal("result not confirmed")
	}
	if !reflect.DeepEqual(res.Checked, []int{1, 2}) {
		t.Fatalf("checked = %v, want [1 2]", res.Checked)
	}
}

func TestMultiSelectLockedRowCannotToggle(t *testing.T) {
	m := NewMultiSelect("pick", []MultiSelectItem{
		{Label: "done-1", Locked: true, LockedMark: "✓"},
		{Label: "open-1"},
	})
	// Move cursor onto the locked row and try to toggle it.
	m.Update(keyUp()) // wraps from index 1 to index... up from 1 -> 0 (locked)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
	m.Update(keySpace())
	if m.checked[0] {
		t.Fatal("locked row was toggled")
	}
	m.Update(keyEnter())
	if len(m.Result().Checked) != 0 {
		t.Fatalf("checked = %v, want none", m.Result().Checked)
	}
}

func TestMultiSelectCancelEmptyResult(t *testing.T) {
	m := NewMultiSelect("pick", []MultiSelectItem{
		{Label: "open-1"},
		{Label: "open-2"},
	})
	m.Update(keySpace()) // check something first
	_, cmd := m.Update(keyEsc())
	if cmd == nil {
		t.Fatal("esc should quit")
	}
	res := m.Result()
	if res.Confirmed {
		t.Fatal("esc must not confirm")
	}
	if len(res.Checked) != 0 {
		t.Fatalf("cancelled result should be empty, got %v", res.Checked)
	}
}

func TestMultiSelectInitialCheckedHonored(t *testing.T) {
	m := NewMultiSelect("pick", []MultiSelectItem{
		{Label: "open-1", Checked: true},
		{Label: "open-2"},
	})
	m.Update(keyEnter())
	if !reflect.DeepEqual(m.Result().Checked, []int{0}) {
		t.Fatalf("checked = %v, want [0]", m.Result().Checked)
	}
}

func TestMultiSelectViewRendersLockAndCheckbox(t *testing.T) {
	m := NewMultiSelect("pick tasks", []MultiSelectItem{
		{Label: "done-1", Locked: true, LockedMark: "✓"},
		{Label: "open-1", Checked: true},
		{Label: "open-2"},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	out := StripANSI(m.view())

	if !strings.Contains(out, "pick tasks") {
		t.Fatalf("title missing:\n%s", out)
	}
	if !strings.Contains(out, "✓ done-1") {
		t.Fatalf("locked mark missing:\n%s", out)
	}
	if !strings.Contains(out, "[x] open-1") {
		t.Fatalf("checked checkbox missing:\n%s", out)
	}
	if !strings.Contains(out, "[ ] open-2") {
		t.Fatalf("unchecked checkbox missing:\n%s", out)
	}
}
