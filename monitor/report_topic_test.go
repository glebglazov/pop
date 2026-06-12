package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func reportTopicMockTmux(paneInfo map[string]string) *deps.MockTmux {
	return &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if len(args) >= 5 && args[0] == "display-message" && args[1] == "-t" {
				paneID := args[2]
				if args[4] == "#{session_name}\t#{pane_current_command}" {
					info, ok := paneInfo[paneID]
					if !ok {
						return "", fmt.Errorf("pane not found: %s", paneID)
					}
					return info, nil
				}
			}
			return "", nil
		},
	}
}

func setupReportTopicState(t *testing.T, panes map[string]*PaneEntry) (string, *Store) {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	if panes == nil {
		panes = map[string]*PaneEntry{}
	}
	data, _ := json.Marshal(&State{Panes: panes})
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	return statePath, NewStore(statePath, nil)
}

func loadReportTopicState(t *testing.T, path string) *State {
	t.Helper()
	state, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestStore_ReportTopic_AutoRegister(t *testing.T) {
	statePath, store := setupReportTopicState(t, nil)
	tmux := reportTopicMockTmux(map[string]string{
		"%7": "proj-x\tclaude",
	})

	err := store.ReportTopic(tmux, ReportTopicInput{PaneID: "%7", Topic: "refactor auth"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportTopicState(t, statePath)
	entry, ok := state.Panes["%7"]
	if !ok {
		t.Fatal("expected pane to be auto-registered")
	}
	if entry.Topic != "refactor auth" {
		t.Errorf("topic = %q, want %q", entry.Topic, "refactor auth")
	}
	if entry.Status != StatusClear {
		t.Errorf("status = %q, want clear", entry.Status)
	}
	if entry.Session != "proj-x" {
		t.Errorf("session = %q, want proj-x", entry.Session)
	}
}

func TestStore_ReportTopic_NoRegisterSuppressesRegistration(t *testing.T) {
	statePath, store := setupReportTopicState(t, nil)
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			t.Errorf("unexpected tmux call: %v", args)
			return "", nil
		},
	}

	err := store.ReportTopic(tmux, ReportTopicInput{PaneID: "%9", Topic: "ignored", NoRegister: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportTopicState(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d entries", len(state.Panes))
	}
}

func TestStore_ReportTopic_UpdatesTrackedPane(t *testing.T) {
	statePath, store := setupReportTopicState(t, map[string]*PaneEntry{
		"%2": {PaneID: "%2", Session: "proj-y", Status: StatusWorking, Topic: "old topic"},
	})
	tmux := reportTopicMockTmux(nil)

	err := store.ReportTopic(tmux, ReportTopicInput{PaneID: "%2", Topic: "new topic"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportTopicState(t, statePath)
	entry := state.Panes["%2"]
	if entry.Topic != "new topic" {
		t.Errorf("topic = %q, want %q", entry.Topic, "new topic")
	}
	// Topic changes must not disturb the pane's status.
	if entry.Status != StatusWorking {
		t.Errorf("status = %q, want working (unchanged)", entry.Status)
	}
}

func TestStore_ReportTopic_ClearWipesTopic(t *testing.T) {
	statePath, store := setupReportTopicState(t, map[string]*PaneEntry{
		"%3": {PaneID: "%3", Session: "proj-z", Status: StatusClear, Topic: "something"},
	})
	tmux := reportTopicMockTmux(nil)

	err := store.ReportTopic(tmux, ReportTopicInput{PaneID: "%3", Topic: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportTopicState(t, statePath)
	if entry := state.Panes["%3"]; entry.Topic != "" {
		t.Errorf("topic = %q, want cleared", entry.Topic)
	}
}

func TestStore_ReportTopic_ClearDoesNotRegisterUntracked(t *testing.T) {
	statePath, store := setupReportTopicState(t, nil)
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			t.Errorf("unexpected tmux call: %v", args)
			return "", nil
		},
	}

	err := store.ReportTopic(tmux, ReportTopicInput{PaneID: "%4", Topic: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportTopicState(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state (clear must not register), got %d entries", len(state.Panes))
	}
}

// Topic survives an unfollow (which clears only the Note) and is gone after
// retirement (the whole entry is removed).
func TestStore_Topic_UnfollowKeepsTopic_RetirementClears(t *testing.T) {
	statePath, store := setupReportTopicState(t, map[string]*PaneEntry{
		"%5": {
			PaneID:    "%5",
			Session:   "proj-q",
			Status:    StatusClear,
			Following: true,
			Note:      "a note",
			Topic:     "derived topic",
		},
	})
	tmux := reportTopicMockTmux(nil)

	if err := store.SetFollowing(tmux, "%5", false); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	state := loadReportTopicState(t, statePath)
	entry := state.Panes["%5"]
	if entry.Note != "" {
		t.Errorf("note = %q, want cleared by unfollow", entry.Note)
	}
	if entry.Topic != "derived topic" {
		t.Errorf("topic = %q, want left intact by unfollow", entry.Topic)
	}

	// Retirement removes the entry entirely, taking the Topic with it.
	if err := store.Remove("%5"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	state = loadReportTopicState(t, statePath)
	if _, ok := state.Panes["%5"]; ok {
		t.Error("expected pane (and its topic) gone after retirement")
	}
}

// Persisted state round-trips the Topic field through monitor.json.
func TestState_Topic_PersistsInJSON(t *testing.T) {
	statePath, store := setupReportTopicState(t, nil)
	tmux := reportTopicMockTmux(map[string]string{"%6": "proj\tnode"})

	if err := store.ReportTopic(tmux, ReportTopicInput{PaneID: "%6", Topic: "writing tests"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"topic": "writing tests"`) {
		t.Errorf("monitor.json did not persist topic field:\n%s", raw)
	}
}
