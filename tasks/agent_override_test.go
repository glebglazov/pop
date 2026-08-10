package tasks

import (
	"testing"

	"github.com/glebglazov/pop/config"
)

func TestPromoteGroupEntriesOrdersRemainder(t *testing.T) {
	entries := []AgentGroupEntry{
		{Position: 1, DisplayName: "First", Cmd: "claude --model opus"},
		{Position: 2, DisplayName: "Second", Cmd: "cursor"},
		{Position: 3, DisplayName: "Third", Cmd: "codex"},
	}
	got := promoteGroupEntries(entries, "cursor")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Cmd != "cursor" || got[0].Position != 1 {
		t.Fatalf("head = %+v, want cursor at position 1", got[0])
	}
	if got[1].Cmd != "claude --model opus" || got[1].Position != 2 {
		t.Fatalf("second = %+v", got[1])
	}
	if got[2].Cmd != "codex" || got[2].Position != 3 {
		t.Fatalf("third = %+v", got[2])
	}
}

// Inline renders drop the model clause entirely when the entry pins none; only
// the catalog's own column says who decides.
func TestFormatAgentEntryNamesOnlyTheEntryWhenNoModel(t *testing.T) {
	e := AgentGroupEntry{DisplayName: "Cursor Usual", Cmd: "cursor"}
	if got := FormatAgentEntry(e); got != "Cursor Usual" {
		t.Fatalf("FormatAgentEntry = %q, want %q", got, "Cursor Usual")
	}
	if got := e.ModelLabel(); got != AgentEntryNoModelLabel {
		t.Fatalf("ModelLabel = %q, want %q", got, AgentEntryNoModelLabel)
	}
}

func TestEffectiveAttendedEntryUsesOverride(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
			{DisplayName: "Cursor", Cmd: "cursor"},
		}},
	}}
	overrides := NewAgentOverrides()
	overrides.Promote("attended", "cursor")
	got := EffectiveAttendedEntry(cfg, overrides)
	if got.Cmd != "cursor" {
		t.Fatalf("cmd = %q, want cursor", got.Cmd)
	}
	if FormatAgentEntry(got) != "Cursor" {
		t.Fatalf("render = %q", FormatAgentEntry(got))
	}
	status := FormatAgentOverrideStatus(got)
	if status != "agent Cursor · "+AgentOverrideKeyLabel {
		t.Fatalf("status = %q", status)
	}
}

func TestAgentOverridesNotPersistedAcrossNewBag(t *testing.T) {
	a := NewAgentOverrides()
	a.Promote("implement", "codex")
	b := NewAgentOverrides()
	if b.Cmd("implement") != "" {
		t.Fatalf("fresh bag should be empty, got %q", b.Cmd("implement"))
	}
}
