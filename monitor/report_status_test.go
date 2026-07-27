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

// reportStatusFake builds a stateful tmux fake. paneInfo maps pane ID ->
// "session\tcommand"; activePanes marks which panes are the attended pane.
func reportStatusFake(paneInfo map[string]string, activePanes map[string]bool) *tmuxtest.Fake {
	infos := map[string]tmux.PaneInfo{}
	for id, raw := range paneInfo {
		parts := strings.SplitN(raw, "\t", 2)
		if len(parts) == 2 {
			infos[id] = tmux.PaneInfo{Session: parts[0], Command: parts[1]}
		}
	}
	return &tmuxtest.Fake{PaneInfos: infos, ActivePanes: activePanes}
}

func setupReportStatusState(t *testing.T, panes map[string]*PaneEntry) (string, *Store) {
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

func loadReportStatusState(t *testing.T, path string) *State {
	t.Helper()
	state, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestStore_ReportStatus_AutoRegistration(t *testing.T) {
	statePath, store := setupReportStatusState(t, nil)
	tmux := reportStatusFake(map[string]string{
		"%7": "test-session\tclaude",
	}, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%7",
		Status: StatusWorking,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportStatusState(t, statePath)
	entry, ok := state.Panes["%7"]
	if !ok {
		t.Fatal("expected pane to be auto-registered")
	}
	if entry.Status != StatusWorking {
		t.Errorf("status = %q, want %q", entry.Status, StatusWorking)
	}
	if entry.Session != "test-session" {
		t.Errorf("session = %q, want test-session", entry.Session)
	}
	if entry.LastActiveAt.IsZero() {
		t.Error("expected LastActiveAt to be seeded on auto-register")
	}
}

func TestStore_ReportStatus_NoRegisterSuppression(t *testing.T) {
	statePath, store := setupReportStatusState(t, nil)
	tmux := reportStatusFake(map[string]string{
		"%1": "some-session\tzsh",
	}, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID:     "%1",
		Status:     StatusClear,
		NoRegister: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportStatusState(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d entries", len(state.Panes))
	}
}

func TestStore_ReportStatus_NoRegisterStillUpdatesTracked(t *testing.T) {
	statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
		"%3": {PaneID: "%3", Session: "proj", Status: StatusWorking},
	})
	tmux := reportStatusFake(nil, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID:     "%3",
		Status:     StatusClear,
		NoRegister: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportStatusState(t, statePath)
	if state.Panes["%3"].Status != StatusClear {
		t.Errorf("status = %q, want clear", state.Panes["%3"].Status)
	}
}

func TestStore_ReportStatus_DismissUnreadInActivePane(t *testing.T) {
	t.Run("policy on and pane active downgrades to clear", func(t *testing.T) {
		statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "test", Status: StatusWorking},
		})
		tmux := reportStatusFake(nil, map[string]bool{"%1": true})

		err := store.ReportStatus(tmux, ReportStatusInput{
			PaneID:                "%1",
			Status:                StatusUnread,
			DismissUnreadInActive: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		state := loadReportStatusState(t, statePath)
		if state.Panes["%1"].Status != StatusClear {
			t.Errorf("got %q, want clear", state.Panes["%1"].Status)
		}
	})

	t.Run("policy off keeps unread", func(t *testing.T) {
		statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "test", Status: StatusWorking},
		})
		tmux := reportStatusFake(nil, map[string]bool{"%1": true})

		err := store.ReportStatus(tmux, ReportStatusInput{
			PaneID:                "%1",
			Status:                StatusUnread,
			DismissUnreadInActive: false,
		})
		if err != nil {
			t.Fatal(err)
		}

		state := loadReportStatusState(t, statePath)
		if state.Panes["%1"].Status != StatusUnread {
			t.Errorf("got %q, want unread", state.Panes["%1"].Status)
		}
	})

	t.Run("policy on but pane inactive keeps unread", func(t *testing.T) {
		statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
			"%1": {PaneID: "%1", Session: "test", Status: StatusWorking},
		})
		tmux := reportStatusFake(nil, map[string]bool{"%1": false})

		err := store.ReportStatus(tmux, ReportStatusInput{
			PaneID:                "%1",
			Status:                StatusUnread,
			DismissUnreadInActive: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		state := loadReportStatusState(t, statePath)
		if state.Panes["%1"].Status != StatusUnread {
			t.Errorf("got %q, want unread", state.Panes["%1"].Status)
		}
	})
}

func TestStore_ReportStatus_StatusNoOp(t *testing.T) {
	before := time.Now().Add(-1 * time.Hour)
	statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
		"%1": {
			PaneID:    "%1",
			Session:   "test",
			Status:    StatusWorking,
			UpdatedAt: before,
		},
	})
	tmux := reportStatusFake(nil, nil)

	origData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	err = store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%1",
		Status: StatusWorking,
	})
	if err != nil {
		t.Fatal(err)
	}

	afterData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterData) != string(origData) {
		t.Error("expected no state write when status unchanged")
	}
}

func TestStore_ReportStatus_VisitOnClear(t *testing.T) {
	before := time.Now().Add(-1 * time.Hour)
	statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
		"%1": {
			PaneID:       "%1",
			Session:      "test",
			Status:       StatusUnread,
			LastActiveAt: before,
		},
	})
	tmux := reportStatusFake(nil, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%1",
		Status: StatusClear,
	})
	if err != nil {
		t.Fatal(err)
	}

	state := loadReportStatusState(t, statePath)
	entry := state.Panes["%1"]
	if entry.Status != StatusClear {
		t.Errorf("status = %q, want clear", entry.Status)
	}
	if !entry.LastActiveAt.After(before) {
		t.Errorf("LastActiveAt = %v, want after %v", entry.LastActiveAt, before)
	}
}

func TestStore_ReportStatus_ClearAlreadyClearUpdatesVisit(t *testing.T) {
	before := time.Now().Add(-1 * time.Hour)
	statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
		"%1": {
			PaneID:       "%1",
			Session:      "test",
			Status:       StatusClear,
			LastActiveAt: before,
		},
	})
	tmux := reportStatusFake(nil, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%1",
		Status: StatusClear,
	})
	if err != nil {
		t.Fatal(err)
	}

	state := loadReportStatusState(t, statePath)
	if !state.Panes["%1"].LastActiveAt.After(before) {
		t.Errorf("LastActiveAt = %v, want after %v", state.Panes["%1"].LastActiveAt, before)
	}
}

func TestStore_ReportStatus_AppliesLabel(t *testing.T) {
	statePath, store := setupReportStatusState(t, nil)
	tmux := reportStatusFake(map[string]string{
		"%9": "proj-x\tnode",
	}, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%9",
		Status: StatusClear,
		Label:  "cursor",
	})
	if err != nil {
		t.Fatal(err)
	}

	state := loadReportStatusState(t, statePath)
	if state.Panes["%9"].Label != "cursor" {
		t.Errorf("label = %q, want cursor", state.Panes["%9"].Label)
	}
}

func TestStore_ReportStatus_LegacyAliasViaNormalize(t *testing.T) {
	statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
		"%1": {PaneID: "%1", Session: "test", Status: StatusWorking},
	})
	tmux := reportStatusFake(nil, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%1",
		Status: NormalizeStatus("needs_attention"),
	})
	if err != nil {
		t.Fatal(err)
	}

	state := loadReportStatusState(t, statePath)
	if state.Panes["%1"].Status != StatusUnread {
		t.Errorf("got %q, want unread", state.Panes["%1"].Status)
	}
}

func TestStore_ReportStatus_ReadAliasViaNormalize(t *testing.T) {
	statePath, store := setupReportStatusState(t, map[string]*PaneEntry{
		"%5": {PaneID: "%5", Session: "test", Status: StatusWorking},
	})
	tmux := reportStatusFake(map[string]string{
		"%6": "test-session\tzsh",
	}, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%6",
		Status: NormalizeStatus("read"),
	})
	if err != nil {
		t.Fatal(err)
	}

	state := loadReportStatusState(t, statePath)
	if state.Panes["%6"].Status != StatusClear {
		t.Errorf("%%6 status = %q, want clear", state.Panes["%6"].Status)
	}

	err = store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%5",
		Status: NormalizeStatus("read"),
	})
	if err != nil {
		t.Fatal(err)
	}

	state = loadReportStatusState(t, statePath)
	if state.Panes["%5"].Status != StatusClear {
		t.Errorf("%%5 status = %q, want clear", state.Panes["%5"].Status)
	}
}

func TestStore_ReportStatus_TmuxLookupFailureIsNoOp(t *testing.T) {
	statePath, store := setupReportStatusState(t, nil)
	// %99 is absent from the fake, so PaneInfo errors — auto-register is a no-op.
	tmux := reportStatusFake(nil, nil)

	err := store.ReportStatus(tmux, ReportStatusInput{
		PaneID: "%99",
		Status: StatusWorking,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadReportStatusState(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d entries", len(state.Panes))
	}
}

func TestStore_ReportStatus_MultipleAgentPanesAutoRegister(t *testing.T) {
	statePath, store := setupReportStatusState(t, nil)
	tmux := reportStatusFake(map[string]string{
		"%20": "proj-a\topencode",
		"%21": "proj-b\tclaude",
		"%22": "proj-c\tpi",
		"%23": "proj-d\tnode",
	}, nil)

	for _, paneID := range []string{"%20", "%21", "%22", "%23"} {
		if err := store.ReportStatus(tmux, ReportStatusInput{
			PaneID: paneID,
			Status: StatusClear,
		}); err != nil {
			t.Fatalf("unexpected error for %s: %v", paneID, err)
		}
	}

	state := loadReportStatusState(t, statePath)
	if len(state.Panes) != 4 {
		t.Fatalf("expected 4 panes, got %d", len(state.Panes))
	}
	for _, paneID := range []string{"%20", "%21", "%22", "%23"} {
		if state.Panes[paneID].Status != StatusClear {
			t.Errorf("%s: status = %q, want clear", paneID, state.Panes[paneID].Status)
		}
	}
}
