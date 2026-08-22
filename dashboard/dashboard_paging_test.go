package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

func ctrlKey(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// TestMainListHalfPageKeys pins the half-page motion on the main list: the same
// C-d/C-u the peek and the pickers already answer to now moves the row cursor by
// a viewport at a time, clamped at both ends.
func TestMainListHalfPageKeys(t *testing.T) {
	rows := make([]DashboardRow, 0, 10)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		rows = append(rows, DashboardRow{Project: "pop", CursorKey: "pop\x00" + id, RawStatus: tasks.StatusReady, ID: id})
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.list.Resize(4)

	updated, _ := m.Update(ctrlKey('d'))
	got := updated.(QueueDashboard)
	if got.list.Cursor() != 3 {
		t.Fatalf("after C-d cursor = %d, want 3", got.list.Cursor())
	}

	updated, _ = got.Update(ctrlKey('d'))
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 6 {
		t.Fatalf("after second C-d cursor = %d, want 6", got.list.Cursor())
	}

	updated, _ = got.Update(ctrlKey('u'))
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 3 {
		t.Fatalf("after C-u cursor = %d, want 3", got.list.Cursor())
	}

	updated, _ = got.Update(ctrlKey('u'))
	updated, _ = updated.(QueueDashboard).Update(ctrlKey('u'))
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 0 {
		t.Fatalf("C-u past the top cursor = %d, want 0", got.list.Cursor())
	}
}

// TestDetailListHalfPageKeys pins the same motion one level down, on the item
// list a detail view opens.
func TestDetailListHalfPageKeys(t *testing.T) {
	items := make([]work.Item, 0, 10)
	for _, id := range []string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10"} {
		items = append(items, work.Item{ID: id, Title: "Task " + id, Type: "AFK", Status: "open"})
	}
	row := DashboardRow{Project: "pop", CursorKey: "pop\x00set", ID: "set", Items: items}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	if got.detail == nil {
		t.Fatalf("l did not open the detail view")
	}
	got.detail.list.Resize(3)

	updated, _ = got.Update(ctrlKey('d'))
	got = updated.(QueueDashboard)
	if got.detail.list.Cursor() != 2 {
		t.Fatalf("after C-d detail cursor = %d, want 2", got.detail.list.Cursor())
	}

	updated, _ = got.Update(ctrlKey('u'))
	got = updated.(QueueDashboard)
	if got.detail.list.Cursor() != 0 {
		t.Fatalf("after C-u detail cursor = %d, want 0", got.detail.list.Cursor())
	}
}

// TestHalfPageKeysAreDocumented keeps the help overlays the complete listing
// (ADR-0204): both lists that answer C-d/C-u say so.
func TestHalfPageKeysAreDocumented(t *testing.T) {
	row := DashboardRow{Project: "pop", CursorKey: "pop\x00set", ID: "set", Items: []work.Item{{ID: "01", Title: "One", Type: "AFK", Status: "open"}}}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})

	documented := func(t *testing.T, m QueueDashboard, where string) {
		t.Helper()
		for _, entry := range m.helpEntries() {
			if entry.Key == "ctrl+d/ctrl+u" {
				return
			}
		}
		t.Fatalf("%s help overlay does not document ctrl+d/ctrl+u", where)
	}

	documented(t, m, "main list")

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	documented(t, updated.(QueueDashboard), "detail")
}
