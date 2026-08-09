package tasks

import (
	"reflect"
	"testing"

	"github.com/glebglazov/pop/config"
)

func TestAgentSpecModel(t *testing.T) {
	cases := []struct {
		spec string
		want string
	}{
		{"claude --model opus", "opus"},
		{"claude --model=opus", "opus"},
		{`cursor --model "composer-2.5[effort=low]"`, "composer-2.5[effort=low]"},
		{"claude", ""},
		{"claude --dangerously-skip-permissions", ""},
		{"claude --model", ""},
	}
	for _, tt := range cases {
		if got := AgentSpecModel(tt.spec); got != tt.want {
			t.Fatalf("AgentSpecModel(%q) = %q, want %q", tt.spec, got, tt.want)
		}
	}
}

// TestAgentSpecModelLeavesArgsAlone pins that reading an entry's model does not
// disturb the rest of the command: the spec still resolves to the same argv.
func TestAgentSpecModelLeavesArgsAlone(t *testing.T) {
	const spec = `claude --model opus --append-system-prompt "be nice"`
	if got := AgentSpecModel(spec); got != "opus" {
		t.Fatalf("model = %q, want opus", got)
	}
	invocation, err := ResolveAgentInvocation(spec, "", "prompt text", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "opus", "--append-system-prompt", "be nice", "--dangerously-skip-permissions", "-p", "--output-format", "stream-json", "--verbose", "prompt text"}
	if !reflect.DeepEqual(invocation.Args, want) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, want)
	}
}

func TestAgentGroupCatalogs(t *testing.T) {
	cfg := &config.Config{Work: &config.WorkConfig{
		Implement: &config.ImplementConfig{Agents: config.AgentEntriesFromCommands("codex")},
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
			{Cmd: "cursor"},
		}},
	}}

	catalogs := AgentGroupCatalogs(cfg)
	if got, want := len(catalogs), len(AgentGroupOrder); got != want {
		t.Fatalf("catalogs = %d, want %d", got, want)
	}
	for i, catalog := range catalogs {
		if catalog.Group != AgentGroupOrder[i] {
			t.Fatalf("catalog %d = %q, want %q", i, catalog.Group, AgentGroupOrder[i])
		}
	}

	attended := catalogs[3]
	if attended.Group != "attended" {
		t.Fatalf("group = %q, want attended", attended.Group)
	}
	want := []AgentGroupEntry{
		{Position: 1, DisplayName: "Claude Usual", Cmd: "claude --model opus", Preset: "claude", Model: "opus"},
		{Position: 2, Cmd: "cursor", Preset: "cursor"},
	}
	if !reflect.DeepEqual(attended.Entries, want) {
		t.Fatalf("attended entries = %#v, want %#v", attended.Entries, want)
	}
	if got := attended.Entries[0].Label(); got != "Claude Usual" {
		t.Fatalf("label = %q, want the display name", got)
	}
	if got := attended.Entries[1].Label(); got != "cursor" {
		t.Fatalf("label = %q, want the command", got)
	}
	// An entry naming no model defers to the agent, never to a guessed name.
	if got := attended.Entries[1].ModelLabel(); got != AgentEntryNoModelLabel {
		t.Fatalf("model label = %q, want %q", got, AgentEntryNoModelLabel)
	}
	if got := attended.Entries[0].ModelLabel(); got != "opus" {
		t.Fatalf("model label = %q, want opus", got)
	}

	if got, want := len(catalogs[1].Entries), 0; got != want {
		t.Fatalf("verify entries = %d, want %d", got, want)
	}
}
