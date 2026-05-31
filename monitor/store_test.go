package monitor

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func mockStoreDeps(panes map[string]*PaneEntry) (*Deps, *[]byte) {
	var savedData []byte
	stateData, _ := json.Marshal(&State{Panes: panes})

	d := &Deps{
		FS: &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "XDG_DATA_HOME" {
					return "/mock/data"
				}
				return ""
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				if path == "/mock/data/pop/monitor.json" {
					if savedData != nil {
						return savedData, nil
					}
					if panes == nil {
						return nil, os.ErrNotExist
					}
					return stateData, nil
				}
				return nil, os.ErrNotExist
			},
			MkdirAllFunc: func(string, os.FileMode) error { return nil },
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedData = data
				return nil
			},
		},
	}
	return d, &savedData
}

func reloadStateFromSaved(t *testing.T, savedData []byte) *State {
	t.Helper()
	if savedData == nil {
		t.Fatal("expected saved data, got nil")
	}
	var s State
	if err := json.Unmarshal(savedData, &s); err != nil {
		t.Fatalf("unmarshal saved state: %v", err)
	}
	return &s
}

func TestStore_UpdatePane(t *testing.T) {
	t.Run("applies mutator and saves", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: StatusWorking},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		err := store.UpdatePane("%1", func(entry *PaneEntry) {
			entry.Status = StatusClear
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if state.Panes["%1"].Status != StatusClear {
			t.Errorf("status = %q, want %q", state.Panes["%1"].Status, StatusClear)
		}
	})

	t.Run("no-op for unknown pane", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: StatusWorking},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		err := store.UpdatePane("%99", func(entry *PaneEntry) {
			entry.Status = StatusClear
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if *saved != nil {
			t.Error("expected no write for unknown pane")
		}
	})

	t.Run("returns load error", func(t *testing.T) {
		d := &Deps{
			FS: &deps.MockFileSystem{
				ReadFileFunc: func(path string) ([]byte, error) {
					return nil, os.ErrPermission
				},
			},
		}
		store := NewStore("/mock/data/pop/monitor.json", d)

		err := store.UpdatePane("%1", func(entry *PaneEntry) {
			entry.Status = StatusClear
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestStore_MarkClear(t *testing.T) {
	d, saved := mockStoreDeps(map[string]*PaneEntry{
		"%1": {PaneID: "%1", Session: "proj", Status: StatusUnread},
	})
	store := NewStore("/mock/data/pop/monitor.json", d)

	if err := store.MarkClear("%1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := reloadStateFromSaved(t, *saved)
	if state.Panes["%1"].Status != StatusClear {
		t.Errorf("status = %q, want %q", state.Panes["%1"].Status, StatusClear)
	}
}

func TestStore_MarkUnread(t *testing.T) {
	d, saved := mockStoreDeps(map[string]*PaneEntry{
		"%1": {PaneID: "%1", Session: "proj", Status: StatusClear},
	})
	store := NewStore("/mock/data/pop/monitor.json", d)

	if err := store.MarkUnread("%1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := reloadStateFromSaved(t, *saved)
	if state.Panes["%1"].Status != StatusUnread {
		t.Errorf("status = %q, want %q", state.Panes["%1"].Status, StatusUnread)
	}
}

func TestStore_ToggleFollow(t *testing.T) {
	t.Run("toggles false to true", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Following: false},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.ToggleFollow("%1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if !state.Panes["%1"].Following {
			t.Error("expected Following = true")
		}
	})

	t.Run("toggles true to false", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Following: true},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.ToggleFollow("%1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if state.Panes["%1"].Following {
			t.Error("expected Following = false")
		}
	})
}

func TestStore_SetNote(t *testing.T) {
	d, saved := mockStoreDeps(map[string]*PaneEntry{
		"%1": {PaneID: "%1", Session: "proj"},
	})
	store := NewStore("/mock/data/pop/monitor.json", d)

	if err := store.SetNote("%1", "check this"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := reloadStateFromSaved(t, *saved)
	if state.Panes["%1"].Note != "check this" {
		t.Errorf("note = %q, want %q", state.Panes["%1"].Note, "check this")
	}
}

func TestStore_Remove(t *testing.T) {
	t.Run("removes tracked pane", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj"},
			"%2": {PaneID: "%2", Session: "proj"},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.Remove("%1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if _, ok := state.Panes["%1"]; ok {
			t.Error("pane %1 should have been removed")
		}
		if _, ok := state.Panes["%2"]; !ok {
			t.Error("pane %2 should still exist")
		}
	})

	t.Run("no-op for unknown pane", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj"},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.Remove("%99"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if _, ok := state.Panes["%1"]; !ok {
			t.Error("pane %1 should still exist")
		}
	})
}

func TestStore_RecordVisit(t *testing.T) {
	t.Run("updates LastActiveAt on tracked pane without changing status", func(t *testing.T) {
		before := time.Now().Add(-1 * time.Hour)
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: StatusWorking, LastActiveAt: before},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.RecordVisit("%1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if !state.Panes["%1"].LastActiveAt.After(before) {
			t.Errorf("LastActiveAt = %v, want after %v", state.Panes["%1"].LastActiveAt, before)
		}
		if state.Panes["%1"].Status != StatusWorking {
			t.Errorf("status = %q, want %q (should not change)", state.Panes["%1"].Status, StatusWorking)
		}
	})

	t.Run("no-op for unknown pane", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj"},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.RecordVisit("%99"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if *saved != nil {
			t.Error("expected no write for unknown pane")
		}
	})
}

func TestStore_DismissUnread(t *testing.T) {
	t.Run("transitions unread to clear and sets LastActiveAt", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: StatusUnread},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.DismissUnread("%1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if state.Panes["%1"].Status != StatusClear {
			t.Errorf("status = %q, want %q", state.Panes["%1"].Status, StatusClear)
		}
		if state.Panes["%1"].LastActiveAt.IsZero() {
			t.Error("expected LastActiveAt to be set")
		}
	})

	t.Run("does not change status if not unread", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: StatusWorking},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.DismissUnread("%1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		state := reloadStateFromSaved(t, *saved)
		if state.Panes["%1"].Status != StatusWorking {
			t.Errorf("status = %q, want %q (should not change)", state.Panes["%1"].Status, StatusWorking)
		}
		if state.Panes["%1"].LastActiveAt.IsZero() {
			t.Error("expected LastActiveAt to be set even for non-unread pane")
		}
	})

	t.Run("no-op for unknown pane", func(t *testing.T) {
		d, saved := mockStoreDeps(map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: StatusUnread},
		})
		store := NewStore("/mock/data/pop/monitor.json", d)

		if err := store.DismissUnread("%99"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if *saved != nil {
			t.Error("expected no write for unknown pane")
		}
	})
}
