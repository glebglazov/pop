package dashboardshell

import (
	"testing"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks/drain"
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
			return work.Attribution{
				Ref:       ref.WorkRef{Kind: k.id, ContainerID: c.ID},
				CursorKey: c.CursorKey,
				Label:     "task set " + c.ID,
			}, true
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

// The launch reads the pane once and the entry page opens on the row it named.
// Paging away and back is not a launch, so the facts are never asked for again.
func TestEntryOnPageASeedsTheCursorFromTheLaunchingPane(t *testing.T) {
	d, f := taggedPaneDeps("set-g")
	s := newShellWith(t, PageWork, d)

	// set-g sorts last of the three rows the fake page loads, so the last index is
	// where the tagged row is and the first index is where an unseeded cursor rests.
	want := len(setRows()) - 1
	if got := s.PageDashboard(PageWork).ListCursor(); got != want {
		t.Fatalf("cursor = %d, want row %d (the pane's tagged set)", got, want)
	}
	if f.CurrentPaneFactsCalls != 1 {
		t.Fatalf("read the pane %d times at launch, want one round-trip", f.CurrentPaneFactsCalls)
	}

	s = pressV(t, s)
	s = pressV(t, s)
	if f.CurrentPaneFactsCalls != 1 {
		t.Fatalf("read the pane %d times after paging, want the launch's one", f.CurrentPaneFactsCalls)
	}
	if got := s.PageDashboard(PageWork).ListCursor(); got != want {
		t.Fatalf("cursor after paging = %d, want the row the launch seeded (%d)", got, want)
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
