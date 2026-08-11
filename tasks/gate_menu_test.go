package tasks

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

func TestPromptGateMenuPrintsAttendedEntry(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("0\n")
	cfg := &config.Config{Work: &config.WorkConfig{
		Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
			{DisplayName: "Claude Usual", Cmd: "claude --model opus"},
		}},
	}}
	spec := ui.GateMenuSpec{
		Headline: "Human-blocked: demo/01-hitl",
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Get agent assistance (default)", Default: true, Assists: true},
			{Key: "0", Label: "Exit"},
		},
	}
	key, _, err := promptGateMenu(&out, in, newPromptReader(in), spec, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if key != "0" {
		t.Fatalf("key = %q, want 0", key)
	}
	want := "1. Get agent assistance (default) · Claude Usual · opus"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("menu missing attended render %q:\n%s", want, out.String())
	}
}

// The retired chord is no longer a gate choice: on the line path it is not a
// listed option, so the menu re-prompts rather than opening anything.
func TestPromptGateMenuRetiredOverrideChordOpensNothing(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("alt+a\n0\n")
	_, _, err := promptGateMenu(&out, in, newPromptReader(in), ui.GateMenuSpec{
		Items: []ui.GateMenuItem{
			{Key: "1", Label: "Get agent assistance (default)", Default: true, Assists: true},
			{Key: "0", Label: "Exit"},
		},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Choose 1, or 0.") {
		t.Fatalf("menu should have re-prompted:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Agent override") {
		t.Fatalf("no override picker may appear:\n%s", out.String())
	}
}
