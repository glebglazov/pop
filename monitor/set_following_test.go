package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
)

// setFollowingFake builds a stateful tmux fake. paneInfo maps pane ID ->
// "session\tcommand"; an absent pane yields a PaneInfo error.
func setFollowingFake(paneInfo map[string]string) *tmuxtest.Fake {
	infos := map[string]tmux.PaneInfo{}
	for id, raw := range paneInfo {
		parts := strings.SplitN(raw, "\t", 2)
		if len(parts) == 2 {
			infos[id] = tmux.PaneInfo{Session: parts[0], Command: parts[1]}
		}
	}
	return &tmuxtest.Fake{PaneInfos: infos}
}

func setupSetFollowingState(t *testing.T, panes map[string]*PaneEntry) (string, *Store) {
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

func loadSetFollowingState(t *testing.T, path string) *State {
	t.Helper()
	state, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestStore_SetFollowing_AutoRegisterOnFollow(t *testing.T) {
	statePath, store := setupSetFollowingState(t, nil)
	tmux := setFollowingFake(map[string]string{
		"%8": "proj-x\tclaude",
	})

	before := time.Now()
	err := store.SetFollowing(tmux, "%8", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	state := loadSetFollowingState(t, statePath)
	entry, ok := state.Panes["%8"]
	if !ok {
		t.Fatal("expected pane to be auto-registered")
	}
	if !entry.Following {
		t.Error("expected Following = true")
	}
	if entry.Status != StatusClear {
		t.Errorf("status = %q, want clear", entry.Status)
	}
	if entry.Session != "proj-x" {
		t.Errorf("session = %q, want proj-x", entry.Session)
	}
	if entry.LastActiveAt.Before(before) || entry.LastActiveAt.After(after) {
		t.Errorf("LastActiveAt = %v, want between %v and %v", entry.LastActiveAt, before, after)
	}
}

func TestStore_SetFollowing_UnfollowOnUntrackedIsNoOp(t *testing.T) {
	statePath, store := setupSetFollowingState(t, nil)
	// Unfollowing an untracked pane must not look the pane up at all.
	fake := &tmuxtest.Fake{PaneInfoFunc: func(paneID string) (tmux.PaneInfo, error) {
		t.Errorf("unexpected pane lookup for %s", paneID)
		return tmux.PaneInfo{}, nil
	}}

	err := store.SetFollowing(fake, "%9", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadSetFollowingState(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d entries", len(state.Panes))
	}
}

func TestStore_SetFollowing_FollowingNoOp(t *testing.T) {
	before := time.Now().Add(-1 * time.Hour)
	statePath, store := setupSetFollowingState(t, map[string]*PaneEntry{
		"%4": {
			PaneID:    "%4",
			Session:   "proj-c",
			Status:    StatusClear,
			Following: true,
			UpdatedAt: before,
		},
	})
	tmux := setFollowingFake(nil)

	origData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	err = store.SetFollowing(tmux, "%4", true)
	if err != nil {
		t.Fatal(err)
	}

	afterData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterData) != string(origData) {
		t.Error("expected no state write when following unchanged")
	}
}

func TestStore_SetFollowing_FollowTrackedPanePreservesStatus(t *testing.T) {
	statePath, store := setupSetFollowingState(t, map[string]*PaneEntry{
		"%3": {
			PaneID:  "%3",
			Session: "proj-b",
			Status:  StatusWorking,
		},
	})
	tmux := setFollowingFake(nil)

	err := store.SetFollowing(tmux, "%3", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadSetFollowingState(t, statePath)
	entry := state.Panes["%3"]
	if !entry.Following {
		t.Error("expected Following = true")
	}
	if entry.Status != StatusWorking {
		t.Errorf("status = %q, want unchanged working", entry.Status)
	}
}

func TestStore_SetFollowing_UpdatesTimestampOnChange(t *testing.T) {
	before := time.Now().Add(-1 * time.Hour)
	statePath, store := setupSetFollowingState(t, map[string]*PaneEntry{
		"%5": {
			PaneID:    "%5",
			Session:   "proj-d",
			Status:    StatusClear,
			Following: false,
			UpdatedAt: before,
		},
	})
	tmux := setFollowingFake(nil)

	err := store.SetFollowing(tmux, "%5", true)
	if err != nil {
		t.Fatal(err)
	}

	state := loadSetFollowingState(t, statePath)
	if !state.Panes["%5"].UpdatedAt.After(before) {
		t.Errorf("UpdatedAt = %v, want after %v", state.Panes["%5"].UpdatedAt, before)
	}
}

func TestStore_SetFollowing_TmuxLookupFailureReturnsError(t *testing.T) {
	_, store := setupSetFollowingState(t, nil)
	// %99 is absent from the fake, so PaneInfo returns an error.
	tmux := setFollowingFake(nil)

	err := store.SetFollowing(tmux, "%99", true)
	if err == nil {
		t.Fatal("expected error on tmux lookup failure")
	}
}
