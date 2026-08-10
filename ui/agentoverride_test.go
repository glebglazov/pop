package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func testOverrideGroups() []AgentOverrideGroup {
	return []AgentOverrideGroup{
		{ID: "implement", Label: "implement", Entries: []AgentOverrideEntry{
			{ID: "claude", Label: "claude · agent's own configuration"},
		}},
		{ID: "verify", Label: "verify", Entries: []AgentOverrideEntry{
			{ID: "codex", Label: "codex · agent's own configuration"},
		}},
		{ID: "routine", Label: "routine", Entries: []AgentOverrideEntry{
			{ID: "claude", Label: "claude · agent's own configuration"},
		}},
		{ID: "attended", Label: "attended", Entries: []AgentOverrideEntry{
			{ID: "claude --model opus", Label: "Claude Usual · opus"},
			{ID: "cursor", Label: "cursor · agent's own configuration"},
		}},
	}
}

func TestAgentOverridePickerDigitOpensGroupAndPicksEntry(t *testing.T) {
	p := NewAgentOverridePicker(testOverrideGroups())
	p.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	if p.level != 1 || p.groupIdx != 3 {
		t.Fatalf("level=%d groupIdx=%d, want entries of attended", p.level, p.groupIdx)
	}
	_, cmd := p.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if !p.Done() || p.Choice() == nil {
		t.Fatal("digit on entry should apply and close")
	}
	if p.Choice().Group != "attended" || p.Choice().Cmd != "cursor" {
		t.Fatalf("choice = %+v", p.Choice())
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd")
	}
}

// Enter carries a human forward through both levels without touching a digit,
// and esc walks the same path back out.
func TestAgentOverridePickerEnterAdvancesEscStepsBack(t *testing.T) {
	p := NewAgentOverridePicker(testOverrideGroups())
	for i := 0; i < 3; i++ {
		p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.level != 1 || p.groupIdx != 3 || p.Done() {
		t.Fatalf("enter at group level should open it: level=%d groupIdx=%d done=%v", p.level, p.groupIdx, p.Done())
	}

	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.level != 0 || p.Done() {
		t.Fatalf("esc in a group should return to the group list, level=%d done=%v", p.level, p.Done())
	}
	if p.cursor != 3 {
		t.Fatalf("esc should restore the highlight to the group it left, cursor=%d", p.cursor)
	}

	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.Choice() == nil || p.Choice().Group != "attended" || p.Choice().Cmd != "cursor" {
		t.Fatalf("enter on an entry should pick it, got %+v", p.Choice())
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd")
	}
}

func TestAgentOverridePickerEscAtGroupListExitsUnchanged(t *testing.T) {
	p := NewAgentOverridePicker(testOverrideGroups())
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !p.Done() || p.Choice() != nil || !p.Cancelled() {
		t.Fatalf("esc at group level should exit unchanged: done=%v choice=%v cancelled=%v", p.Done(), p.Choice(), p.Cancelled())
	}
}

func TestAgentOverridePickerZeroStepsBackLikeEsc(t *testing.T) {
	p := NewAgentOverridePicker(testOverrideGroups())
	p.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	p.Update(tea.KeyPressMsg{Code: '0', Text: "0"})
	if p.level != 0 || p.Done() {
		t.Fatalf("0 should return to groups, level=%d done=%v", p.level, p.Done())
	}
	p.Update(tea.KeyPressMsg{Code: '0', Text: "0"})
	if !p.Done() || p.Choice() != nil {
		t.Fatalf("0 at group level should exit unchanged: done=%v choice=%v", p.Done(), p.Choice())
	}
}

func TestAgentOverridePickerArrowsEnterPastNine(t *testing.T) {
	entries := make([]AgentOverrideEntry, 12)
	for i := range entries {
		entries[i] = AgentOverrideEntry{ID: string(rune('a' + i)), Label: string(rune('a' + i))}
	}
	p := NewAgentOverridePicker([]AgentOverrideGroup{{ID: "attended", Label: "attended", Entries: entries}})
	p.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	for i := 0; i < 10; i++ {
		p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if p.cursor != 10 {
		t.Fatalf("cursor = %d, want 10", p.cursor)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.Choice() == nil || p.Choice().Cmd != string(rune('a'+10)) {
		t.Fatalf("enter past nine should pick cursor, got %+v", p.Choice())
	}
}

func TestAgentOverridePickerAltAInert(t *testing.T) {
	p := NewAgentOverridePicker(testOverrideGroups())
	p.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	if p.level != 0 || p.Done() {
		t.Fatal("alt+a must be inert inside the picker")
	}
}

func TestAgentOverridePickerViewListsGroups(t *testing.T) {
	p := NewAgentOverridePicker(testOverrideGroups())
	view := p.ViewContent()
	for _, want := range []string{"implement", "verify", "routine", "attended", "1.", "4."} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestIsAgentOverrideKey(t *testing.T) {
	if !IsAgentOverrideKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt}) {
		t.Fatal("alt+a should match")
	}
	if IsAgentOverrideKey(tea.KeyPressMsg{Code: 'a', Text: "a"}) {
		t.Fatal("plain a must not match")
	}
	if IsAgentOverrideKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}) {
		t.Fatal("ctrl+a must not match")
	}
}
