package wayfinder

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// pickedAttendedConfig is a two-entry attended list whose head is not the entry
// these tests pick, so a launch reading the head instead of the pick is visible
// in both the command and the title.
func pickedAttendedConfig() (*config.Config, string) {
	picked := "claude --model opus"
	cfg := &config.Config{Work: &config.WorkConfig{Attended: &config.AgentGroupConfig{Agents: config.AgentEntries{
		{DisplayName: "Head", Cmd: "claude"},
		{DisplayName: "Picked", Cmd: picked},
	}}}}
	return cfg, picked
}

// A Map's assist launch honours the entry the launch site picked, end to end
// (ADR-0264 decision 8): the pane runs that entry's whole cmd, and its title
// names that entry rather than the head of the list.
func TestAssistPaneRunsAndIsTitledByThePickedEntry(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	d.Tasks.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	fake := atTime(d, at(9))
	cfg, picked := pickedAttendedConfig()

	pane, err := AssistMap(d, cfg, "", claimMapID, picked)
	if err != nil {
		t.Fatalf("AssistMap: %v", err)
	}
	command := strings.Join(fake.SentCommands[pane.PaneID], " ")
	if !strings.Contains(command, "--model opus") {
		t.Fatalf("assist pane runs %q, want the picked entry's model", command)
	}
	want := "assist · Picked · opus"
	if fake.PaneTitles[pane.PaneID] != want || pane.Title != want {
		t.Fatalf("assist pane title = %q (reported %q), want %q", fake.PaneTitles[pane.PaneID], pane.Title, want)
	}
}

// A fan-out takes one pick for the whole spawn: every pane it opens runs the
// picked entry and is titled by it, because a prompt per pane is not a choice
// (ADR-0264 decision 8).
func TestFanOutRunsThePickedEntryInEveryPane(t *testing.T) {
	t.Parallel()
	d, _ := claimFixture(t)
	d.Tasks.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	fake := atTime(d, at(9))
	cfg, picked := pickedAttendedConfig()

	out, err := FanOutFrontier(d, cfg, "", claimMapID, picked)
	if err != nil {
		t.Fatalf("FanOutFrontier: %v", err)
	}
	if len(out.Spawned) < 2 {
		t.Fatalf("fanned out %d panes, want the whole frontier", len(out.Spawned))
	}
	for _, spawned := range out.Spawned {
		paneID := spawned.Pane.PaneID
		command := strings.Join(fake.SentCommands[paneID], " ")
		if !strings.Contains(command, "--model opus") {
			t.Fatalf("grilling pane for %s runs %q, want the picked entry's model", spawned.Ticket.ID, command)
		}
		if title := fake.PaneTitles[paneID]; !strings.HasSuffix(title, " · Picked · opus") {
			t.Fatalf("grilling pane title = %q, want it named by the picked entry", title)
		}
	}
}
