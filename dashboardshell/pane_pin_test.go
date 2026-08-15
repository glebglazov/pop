package dashboardshell

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// attributingKind is a page kind that claims the pane tagged for one of its rows —
// the seam a real kind implements, on the fake the shell tests already page
// through.
type attributingKind struct {
	*pageKind
	claims string
}

func (k *attributingKind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.Set != k.claims {
		return work.Attribution{}, false
	}
	for _, c := range k.containers {
		if c.ID == k.claims {
			return work.AttributeOne(work.AttributedContainer{
				Ref:       ref.WorkRef{Kind: k.id, ContainerID: c.ID},
				CursorKey: c.CursorKey,
				Label:     "task set " + c.ID,
			}), true
		}
	}
	return work.Attribution{}, false
}

// taggedPaneDeps builds deps whose page-A kind claims setID, in a pane tagged for
// it, and returns the fake so a test can count what the launch asked tmux.
func taggedPaneDeps(setID string) (*drain.Deps, *tmuxtest.Fake) {
	f := &tmuxtest.Fake{
		CurrentPaneID:      "%9",
		CurrentSessionName: "work",
		PaneTagValues:      map[string]map[tmuxmod.PaneTag]string{"%9": {tmuxmod.TagSet: setID}},
	}
	d := countedDeps(nil, nil, nil)
	d.Tmux = f
	d.Kinds = func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{&attributingKind{
			pageKind: &pageKind{id: ref.KindTaskSet, containers: setRows(),
				columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set"},
			claims: setID,
		}}
	}
	return d, f
}

// The launch reads the pane once and the entry page opens with the row it named
// pinned to the top and marked — with the cursor left where it always rests. Paging
// away and back is not a launch, so the facts are never asked for again.
func TestEntryOnPageAPinsTheLaunchingPanesRow(t *testing.T) {
	d, f := taggedPaneDeps("set-g")
	s := newShellWith(t, PageWork, d)

	// set-g sorts last of the three rows the fake page loads, so seeing it above
	// set-a is the whole of the pin.
	assertPinnedFirst(t, s, "set-g", "set-a")
	if got := s.PageDashboard(PageWork).ListCursor(); got != 0 {
		t.Fatalf("cursor = %d, want the untouched first row: the pin moves rows, not the cursor", got)
	}
	if f.CurrentPaneFactsCalls != 1 {
		t.Fatalf("read the pane %d times at launch, want one round-trip", f.CurrentPaneFactsCalls)
	}

	s = pressV(t, s)
	s = pressV(t, s)
	if f.CurrentPaneFactsCalls != 1 {
		t.Fatalf("read the pane %d times after paging, want the launch's one", f.CurrentPaneFactsCalls)
	}
	assertPinnedFirst(t, s, "set-g", "set-a")
}

// assertPinnedFirst reads the rendered page the way the human does: the pinned row
// above the row that outranks it under the ordinary sort, carrying the pin mark.
func assertPinnedFirst(t *testing.T, s Shell, pinned, below string) {
	t.Helper()
	view := ui.StripANSI(s.View().Content)
	at, under := strings.Index(view, pinned), strings.Index(view, below)
	if at < 0 || under < 0 {
		t.Fatalf("view names %q at %d and %q at %d:\n%s", pinned, at, below, under, view)
	}
	if at > under {
		t.Fatalf("%q renders below %q, want it pinned above:\n%s", pinned, below, view)
	}
	line := view[at:]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	if start := strings.LastIndexByte(view[:at], '\n'); start >= 0 {
		line = view[start+1:][:len(line)+at-start-1]
	}
	if !strings.Contains(line, "\u25b8") {
		t.Fatalf("pinned row = %q, want the pin mark in its prefix column", line)
	}
}

// Decision 5: the dashboard opens on its usual page. `pop routine dashboard`
// enters on page B, and a launch that is not page A's reads no pane at all —
// the answer would belong to a row on the other page.
func TestEntryOnPageBReadsNoPaneFacts(t *testing.T) {
	d, _ := taggedPaneDeps("set-g")

	if facts := launchPaneFacts(PageWork, d); facts.Set != "set-g" {
		t.Fatalf("page A facts = %+v, want the pane's set tag — the fixture is not arranging a pane", facts)
	}
	if facts := launchPaneFacts(PageRoutines, d); !facts.Empty() {
		t.Fatalf("page B facts = %+v, want none", facts)
	}
}
