package wayfinder

import (
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Map kind's two rungs of Pane work attribution (ADR-0201). Each case runs
// through the snapshot builder rather than calling AttributePane directly: the
// answer is only correct if it agrees with the container the same build loaded,
// so the assertion is against that container's own cursor key.

// attributeMap builds one snapshot from the pane's facts and returns the
// attribution beside the containers it was resolved against.
func attributeMap(t *testing.T, k *MapKind, facts work.PaneFacts) (*work.Attribution, map[string]work.Container) {
	t.Helper()
	snap, err := work.BuildSnapshotForPane([]work.Kind{k}, facts)
	if err != nil {
		t.Fatalf("BuildSnapshotForPane: %v", err)
	}
	byID := map[string]work.Container{}
	for _, c := range snap.Containers {
		byID[c.ID] = c
	}
	return snap.Attribution, byID
}

// assertAttributes pins an attribution against the row it names: the identity
// every Work surface uses, and the cursor key the row itself carries. A Map's
// rungs each name exactly one container, so an answer carrying a second one is a
// failure of the rung, not a ranking.
func assertAttributes(t *testing.T, att *work.Attribution, rows map[string]work.Container, mapID string) {
	t.Helper()
	if att == nil {
		t.Fatalf("attribution = none, want the Map %q", mapID)
	}
	if len(att.Containers) != 1 {
		t.Fatalf("attribution = %+v, want exactly the Map %q", att.Containers, mapID)
	}
	c := att.Containers[0]
	if c.Ref != (ref.WorkRef{Kind: ref.KindMap, ContainerID: mapID}) {
		t.Fatalf("ref = %v, want map:%s", c.Ref, mapID)
	}
	row, ok := rows[mapID]
	if !ok {
		t.Fatalf("no row for %q among %d — this assertion needs a rendered row", mapID, len(rows))
	}
	if c.CursorKey != row.CursorKey {
		t.Fatalf("cursor key = %q, want the row's own %q", c.CursorKey, row.CursorKey)
	}
	if c.Label == "" {
		t.Fatal("label = empty: the surface has nothing to name the container by")
	}
}

// The strongest rung a Map has: the pane carries the ticket it is deciding. A
// ticket id is per-Map, so only the kind can say whose "01" this is — which is the
// whole reason the ladder is answered behind the seam.
func TestGrillingPaneAttributesToItsMap(t *testing.T) {
	k, _ := mapKindFixture(t)
	att, rows := attributeMap(t, k, work.PaneFacts{
		PaneID:  "%3",
		Session: MapSessionName("2026-07-01-active"),
		Ticket:  "01",
	})
	assertAttributes(t, att, rows, "2026-07-01-active")
}

// A Map's assist pane is tagged the way a Task set's is, keyed by the container
// either way (ADR-0184), so the same tag answers for a Map when the id is a Map's.
func TestAssistPaneAttributesToItsMap(t *testing.T) {
	k, _ := mapKindFixture(t)
	att, rows := attributeMap(t, k, work.PaneFacts{PaneID: "%4", Assist: "2026-07-01-active"})
	assertAttributes(t, att, rows, "2026-07-01-active")
}

// The session rung: a bare shell in a Map's session, which is where the human is
// standing when they open the dashboard from work they were just doing. The stamp
// is the authority, and the session name answers for a session stamped before pop
// wrote stamps.
func TestMapSessionAttributesToItsMap(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts work.PaneFacts
	}{
		{
			name: "work stamp",
			facts: work.PaneFacts{
				PaneID:   "%5",
				Session:  "some-session-name",
				WorkKind: string(ref.KindMap),
				WorkID:   "2026-07-02-arrived",
			},
		},
		{
			name:  "session name alone",
			facts: work.PaneFacts{PaneID: "%5", Session: MapSessionName("2026-07-02-arrived")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, _ := mapKindFixture(t)
			att, rows := attributeMap(t, k, tc.facts)
			assertAttributes(t, att, rows, "2026-07-02-arrived")
		})
	}
}

// The fixture's abandoned Map renders no row. It must still be attributed: a pane
// belonging to a hidden container has to be told apart from a pane belonging to
// nothing, or the surface cannot say why the cursor did not move (decision 6).
func TestHiddenMapIsStillAttributed(t *testing.T) {
	k, _ := mapKindFixture(t)
	att, rows := attributeMap(t, k, work.PaneFacts{
		PaneID:  "%6",
		Session: MapSessionName("2026-07-03-abandoned"),
	})
	if _, rendered := rows["2026-07-03-abandoned"]; rendered {
		t.Fatal("the abandoned Map rendered a row; this test needs a hidden one")
	}
	if att == nil {
		t.Fatal("attribution = none, want the hidden Map")
	}
	lead, ok := att.Leading()
	if !ok || lead.Ref.ContainerID != "2026-07-03-abandoned" {
		t.Fatalf("attribution = %+v, want the hidden Map", att.Containers)
	}
	if lead.Label == "" {
		t.Fatal("label = empty: a hidden container is reported by name or not at all")
	}
}

func TestPaneOutsideAnyMapAttributesNothing(t *testing.T) {
	k, _ := mapKindFixture(t)
	for _, facts := range []work.PaneFacts{
		{PaneID: "%1", Session: "editor"},
		{PaneID: "%1", Session: "editor", Ticket: "99"},
		{PaneID: "%1", Session: MapSessionName("2026-07-09-gone")},
		{PaneID: "%1", Session: "editor", WorkKind: string(ref.KindTaskSet), WorkID: "2026-07-01-active"},
	} {
		if att, _ := attributeMap(t, k, facts); att != nil {
			t.Fatalf("facts %+v attributed %+v, want nothing", facts, *att)
		}
	}
}

// Two Maps can hold a ticket of the same id, and the pane tag carries the id
// alone. The session those panes live in is what breaks the tie, because a
// Grilling pane is in its Map's session by construction.
func TestTicketHeldByTwoMapsIsBrokenByTheSession(t *testing.T) {
	storageDir := "/data/repos/repo-twins"
	tasksDir := filepath.Join(storageDir, "tasks")
	first := filepath.Join(storageDir, "maps", "2026-07-01-first")
	second := filepath.Join(storageDir, "maps", "2026-07-02-second")
	files := map[string]string{
		filepath.Join(first, "map.md"):                  "Status: active\n\n## Destination\nOne\n",
		filepath.Join(first, "issues", "01-one.md"):     "Type: research\nStatus: open\n\n# Q\n",
		filepath.Join(second, "map.md"):                 "Status: active\n\n## Destination\nTwo\n",
		filepath.Join(second, "issues", "01-other.md"):  "Type: research\nStatus: open\n\n# Q\n",
		filepath.Join(second, "issues", "02-second.md"): "Type: research\nStatus: open\n\n# Q\n",
	}
	wd := wayfinderTestDeps(t, t.TempDir(), "/repo/.git", files)
	group := repogroup.Group{
		DefPath:       tasksDir,
		StatePath:     tasks.StatePathFor(tasksDir),
		StorageDir:    storageDir,
		RepoKey:       "repo-key",
		RepoCommonDir: "/repo/.git",
		ProjectName:   "pop",
		Rep:           &repogroup.Checkout{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
	}
	k := NewMapKind(&MapKindDeps{
		Wayfinder: wd,
		Config:    &config.Config{},
		Groups:    func() ([]repogroup.Group, error) { return []repogroup.Group{group}, nil },
	})

	att, rows := attributeMap(t, k, work.PaneFacts{
		PaneID:  "%7",
		Session: MapSessionName("2026-07-02-second"),
		Ticket:  "01",
	})
	assertAttributes(t, att, rows, "2026-07-02-second")
}
