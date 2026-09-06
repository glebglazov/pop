package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The detail view's `/` is the row list's search one level down (ADR-0261
// decision 5): the same committed, case-insensitive, terms-AND-ed grammar, over
// the items of the container the detail is open on. These tests drive it through
// the keyboard, so what they pin is what a human reaches.

func detailSearchRows() []DashboardRow {
	return []DashboardRow{
		{
			Project: "pop", CursorKey: "pop\x00pumps", ID: "pumps", Kind: ref.KindTaskSet,
			Items: []work.Item{
				{ID: "01", Title: "Wire the pump", Type: "AFK", Status: "open"},
				{ID: "02", Title: "Review the pump", Type: "HITL", Status: "done"},
				{ID: "03", Title: "Ship it", Type: "AFK", Status: "blocked"},
			},
		},
		{
			Project: "pop", CursorKey: "pop\x00other", ID: "other", Kind: ref.KindTaskSet,
			Items: []work.Item{{ID: "09", Title: "Elsewhere", Type: "AFK", Status: "open"}},
		},
	}
}

// detailSearchModel opens the detail view on the first of two containers.
func detailSearchModel(kind work.Kind) QueueDashboard {
	d := &drain.Deps{Kinds: func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{kind} }}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: detailSearchRows()})
	m.width, m.height = 120, 24
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	return updated.(QueueDashboard)
}

func detailItemIDs(m QueueDashboard) []string {
	var ids []string
	for _, item := range m.detail.list.Items() {
		ids = append(ids, item.ID)
	}
	return ids
}

func detailArtifactNames(m QueueDashboard) []string {
	var names []string
	for _, artifact := range m.detail.artifactList.Items() {
		names = append(names, artifact.Name)
	}
	return names
}

func TestDetailSearchNarrowsTheItemList(t *testing.T) {
	m := typeSearch(detailSearchModel(&itemVerbKind{}), "PUMP", true)
	if got := detailItemIDs(m); strings.Join(got, ",") != "01,02" {
		t.Fatalf("applied 'PUMP': items = %v, want the two pump tasks (case-insensitive)", got)
	}
	if m.detail.searchTyping {
		t.Fatal("enter must end the typing phase")
	}
	view := m.View().Content
	if !strings.Contains(view, "search: PUMP") {
		t.Fatalf("the term in force must be visible:\n%s", view)
	}

	// The whole keymap is back over the narrowed items: j moves, r opens the
	// cursored item's menu.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	if m.detail.list.Cursor() != 1 {
		t.Fatalf("j after enter: cursor = %d, want 1", m.detail.list.Cursor())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(QueueDashboard)
	if m.itemMenu == nil || m.itemMenu.item.ID != "02" {
		t.Fatalf("r after enter did not open the second narrowed item's menu: %+v", m.itemMenu)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)

	// The poll that rebuilds the detail re-applies the term rather than putting
	// the whole container back.
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: detailSearchRows()}})
	m = updated.(QueueDashboard)
	if got := detailItemIDs(m); strings.Join(got, ",") != "01,02" {
		t.Fatalf("after the poll: items = %v, want the term still in force", got)
	}
}

func TestDetailSearchAndsTermsAcrossFieldsAndSkipsStatus(t *testing.T) {
	m := typeSearch(detailSearchModel(&itemVerbKind{}), "afk pump", true)
	if got := detailItemIDs(m); strings.Join(got, ",") != "01" {
		t.Fatalf("applied 'afk pump': items = %v, want only the AFK pump task", got)
	}

	m = typeSearch(m, "blocked", true)
	if got := detailItemIDs(m); len(got) != 0 {
		t.Fatalf("an item's status is not searched: 'blocked' matched %v", got)
	}
	view := m.View().Content
	if !strings.Contains(view, `No items match search "blocked"`) || !strings.Contains(view, "/ then enter to clear") {
		t.Fatalf("an emptied list must name the term and the way out:\n%s", view)
	}
}

func TestDetailSearchTypingOwnsTheKeyboard(t *testing.T) {
	m := detailSearchModel(&itemVerbKind{})
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, ch := range "jkvr" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if m.detail.searchInput.Value() != "jkvr" {
		t.Fatalf("buffer = %q, want every key typed rather than acted on", m.detail.searchInput.Value())
	}
	if m.itemMenu != nil || m.detail.artifacts || m.detail.list.Cursor() != 0 {
		t.Fatalf("j/k/v/r acted while typing: menu=%v artifacts=%v cursor=%d",
			m.itemMenu, m.detail.artifacts, m.detail.list.Cursor())
	}
	if m.searchTyping || m.searchTerm != "" {
		t.Fatal("the detail's search must not touch the row list's buffer or term")
	}
	if view := m.View().Content; !strings.Contains(view, "enter apply search · esc cancel") {
		t.Fatalf("the typing phase must name its three reserved keys:\n%s", view)
	}
}

func TestDetailSearchEscRestoresAndEmptyQueryClears(t *testing.T) {
	m := typeSearch(detailSearchModel(&itemVerbKind{}), "pump", true)

	m = typeSearch(m, "zzz", false)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)
	if m.detail == nil {
		t.Fatal("esc while typing must abandon the edit, not close the detail")
	}
	if m.detail.searchTerm != "pump" || strings.Join(detailItemIDs(m), ",") != "01,02" {
		t.Fatalf("esc: term = %q items = %v, want the term that was in force", m.detail.searchTerm, detailItemIDs(m))
	}

	m = typeSearch(m, "", true)
	if got := detailItemIDs(m); len(got) != 3 {
		t.Fatalf("applying an empty query: items = %v, want the whole container back", got)
	}
}

func TestDetailSearchNarrowsTheArtifactList(t *testing.T) {
	kind := &artifactDetailKind{id: ref.KindTaskSet, artifacts: []work.Artifact{
		{Type: "refine", Name: "refine-2026.md", Path: "/w/pumps/refine/refine-2026.md"},
		{Type: "spec", Name: "spec.md", Path: "/w/pumps/spec.md"},
		{Type: "progress", Name: "progress.txt", Path: "/w/pumps/progress.txt"},
	}}
	m := detailSearchModel(kind)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(QueueDashboard)
	if !m.detail.artifacts {
		t.Fatal("v did not switch to the artifact view")
	}

	m = typeSearch(m, "SPEC", true)
	if got := detailArtifactNames(m); strings.Join(got, ",") != "spec.md" {
		t.Fatalf("applied 'SPEC': artifacts = %v, want the spec document", got)
	}

	m = typeSearch(m, "spec refine", true)
	if got := detailArtifactNames(m); len(got) != 0 {
		t.Fatalf("terms are AND-ed over artifacts too: matched %v", got)
	}
	view := m.View().Content
	if !strings.Contains(view, `No artifacts match search "spec refine"`) {
		t.Fatalf("an emptied artifact list must name the term:\n%s", view)
	}
	// The search hides artifact rows; it does not take the Artifact view away, so
	// `v` is still the way back to the items.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if m = updated.(QueueDashboard); m.detail.artifacts {
		t.Fatal("v must still cross back to the item list under an emptying search")
	}
}

func TestDetailSearchAndRowSearchAreSeparate(t *testing.T) {
	m := typeSearch(detailSearchModel(&itemVerbKind{}), "pump", true)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)
	if m.detail != nil {
		t.Fatal("esc outside the typing phase must close the detail")
	}
	if m.searchTerm != "" || len(m.snap.Containers) != 2 {
		t.Fatalf("the detail's term leaked to the rows: term = %q rows = %d", m.searchTerm, len(m.snap.Containers))
	}

	m = typeSearch(m, "other", true)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = updated.(QueueDashboard)
	if m.detail.searchTerm != "" || len(detailItemIDs(m)) != 1 {
		t.Fatalf("the rows' term leaked into the detail: term = %q items = %v", m.detail.searchTerm, detailItemIDs(m))
	}
	if m.searchTerm != "other" {
		t.Fatalf("row search term = %q, want it untouched by the detail", m.searchTerm)
	}
}
