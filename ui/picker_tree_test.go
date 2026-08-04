package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The tree gestures are exercised against a fixture tree — one project with two
// live children plus a plain neighbour row — because what is under test is the
// keyboard, not any particular caller's arrangement rules.

const (
	testCollapsed = "▸"
	testExpanded  = "▾"
)

// fixtureTree is a caller in miniature: it holds the expansion bits and renders
// rows from them, the way the project list does.
type fixtureTree struct {
	expanded map[string]bool
	relists  int
}

func newFixtureTree() *fixtureTree {
	return &fixtureTree{expanded: map[string]bool{}}
}

func (f *fixtureTree) rows(query string) []Item {
	f.relists++
	if query != "" {
		// The flattened universe a query searches: every row at depth zero under
		// its full name, including one that nesting folds away entirely.
		return []Item{
			{Name: "old", Path: "/old"},
			{Name: "hawk", Path: "/hawk"},
			{Name: "hawk/cold", Path: "/wt/cold"},
			{Name: "hawk/fix", Path: "/wt/fix"},
			{Name: "hawk/spike", Path: "/wt/spike"},
		}
	}
	rows := []Item{{Name: "old", Path: "/old"}}
	parent := Item{Name: "hawk", Path: "/hawk", Disclosure: testCollapsed}
	if f.expanded["/hawk"] {
		parent.Disclosure = testExpanded
	}
	rows = append(rows, parent)
	if f.expanded["/hawk"] {
		rows = append(rows,
			Item{Name: "fix", Path: "/wt/fix", Depth: 1},
			Item{Name: "spike", Path: "/wt/spike", Depth: 1},
		)
	}
	return rows
}

func (f *fixtureTree) Tree() Tree {
	return Tree{
		Rows: f.rows,
		SetExpanded: func(path string, expand bool) {
			if expand {
				f.expanded[path] = true
				return
			}
			delete(f.expanded, path)
		},
	}
}

func newTreePicker(t *testing.T) (*Picker, *fixtureTree) {
	t.Helper()
	tree := newFixtureTree()
	p := NewPicker(tree.rows(""), WithTree(tree.Tree()), WithQuickAccess("alt"))
	p.width = 60
	p.height = 10
	p.list.Resize(p.height)
	p.Init()
	return p, tree
}

func listedRows(p *Picker) []string {
	out := make([]string, 0, len(p.filtered))
	for _, it := range p.filtered {
		name := it.Name
		if it.Disclosure != "" {
			name += " " + it.Disclosure
		}
		if it.Depth > 0 {
			name = "  " + name
		}
		out = append(out, name)
	}
	return out
}

func assertListed(t *testing.T, p *Picker, want ...string) {
	t.Helper()
	if got := listedRows(p); !reflect.DeepEqual(got, want) {
		t.Errorf("listed rows:\n got %q\nwant %q", got, want)
	}
}

// focusRow puts the cursor on a row by path, keeping the picker's own mirror of
// the list cursor in step — Update syncs from it on every key press.
func focusRow(p *Picker, path string) {
	p.list.SetCursorToKey(path)
	p.syncFromList()
}

func pressRight(p *Picker) { p.Update(tea.KeyPressMsg{Code: tea.KeyRight}) }
func pressLeft(p *Picker)  { p.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) }

func typeQuery(p *Picker, s string) {
	for _, r := range s {
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Right is one gesture doing two things in sequence: it opens a closed row, and
// on an open one it walks into the first child.
func TestTreeRightExpandsThenDescends(t *testing.T) {
	p, tree := newTreePicker(t)
	focusRow(p, "/hawk")

	pressRight(p)
	assertListed(t, p, "old", "hawk "+testExpanded, "  fix", "  spike")
	if got := p.filtered[p.cursor].Path; got != "/hawk" {
		t.Fatalf("cursor after expand = %q, want the row that was opened", got)
	}
	if !tree.expanded["/hawk"] {
		t.Error("expansion was not recorded with the caller")
	}

	pressRight(p)
	if got := p.filtered[p.cursor].Path; got != "/wt/fix" {
		t.Errorf("cursor after second right = %q, want the first child", got)
	}

	// A childless row has nothing to open and nothing to descend into.
	focusRow(p, "/old")
	before := listedRows(p)
	pressRight(p)
	if got := p.filtered[p.cursor].Path; got != "/old" {
		t.Errorf("cursor moved off a childless row: %q", got)
	}
	assertListed(t, p, before...)
}

// Left on an open parent closes it; left on a child closes the parent and leaves
// the cursor on the parent, so the operator keeps their place instead of having
// the row under the cursor disappear.
func TestTreeLeftCollapsesAndLandsOnParent(t *testing.T) {
	p, tree := newTreePicker(t)
	focusRow(p, "/hawk")
	pressRight(p)
	focusRow(p, "/wt/spike")

	pressLeft(p)
	assertListed(t, p, "old", "hawk "+testCollapsed)
	if got := p.filtered[p.cursor].Path; got != "/hawk" {
		t.Errorf("cursor after collapsing from a child = %q, want the parent", got)
	}
	if tree.expanded["/hawk"] {
		t.Error("collapse was not recorded with the caller")
	}

	// And from the parent itself.
	pressRight(p)
	pressLeft(p)
	assertListed(t, p, "old", "hawk "+testCollapsed)
	if got := p.filtered[p.cursor].Path; got != "/hawk" {
		t.Errorf("cursor after collapsing a parent = %q, want it to stay put", got)
	}
}

// Typing hands the picker a different row set — the whole universe, flat, under
// full prefixed names — and the arrows go back to being the query's cursor keys.
func TestTreeQueryFlattensAndFreesTheArrows(t *testing.T) {
	p, tree := newTreePicker(t)
	focusRow(p, "/hawk")
	pressRight(p)

	typeQuery(p, "hawk")
	assertListed(t, p, "hawk", "hawk/cold", "hawk/fix", "hawk/spike")
	for _, it := range p.filtered {
		if it.Depth != 0 || it.Disclosure != "" {
			t.Errorf("row %q is still nested: depth=%d disclosure=%q", it.Name, it.Depth, it.Disclosure)
		}
	}

	// The arrows now edit the query. The listed rows do not change and the tree is
	// never asked to expand anything.
	relistsBefore, cursorBefore := tree.relists, p.cursor
	pressLeft(p)
	if p.input.Cursor() != len("hawk")-1 {
		t.Errorf("left did not move the query cursor: at %d", p.input.Cursor())
	}
	pressRight(p)
	if p.input.Cursor() != len("hawk") {
		t.Errorf("right did not move the query cursor: at %d", p.input.Cursor())
	}
	if tree.relists != relistsBefore {
		t.Errorf("arrows re-listed the tree %d times while a query was typed", tree.relists-relistsBefore)
	}
	if p.cursor != cursorBefore {
		t.Errorf("arrows moved the list cursor while a query was typed: %d -> %d", cursorBefore, p.cursor)
	}

	// Clearing the query brings the tree back, still expanded.
	p.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	assertListed(t, p, "old", "hawk "+testExpanded, "  fix", "  spike")
}

// Paging is untouched by the tree in either state: ctrl+b and ctrl+f are matched
// before the arrows and never reach the query field.
func TestTreePagingWorksInBothStates(t *testing.T) {
	p, _ := newTreePicker(t)
	focusRow(p, "/hawk")
	pressRight(p)

	p.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if p.cursor != 0 {
		t.Errorf("ctrl+b with an empty query: cursor = %d, want 0", p.cursor)
	}
	p.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if p.cursor != len(p.filtered)-1 {
		t.Errorf("ctrl+f with an empty query: cursor = %d, want %d", p.cursor, len(p.filtered)-1)
	}

	typeQuery(p, "hawk")
	p.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if p.cursor != 0 {
		t.Errorf("ctrl+b with a query typed: cursor = %d, want 0", p.cursor)
	}
	if p.input.Value() != "hawk" || p.input.Cursor() != len("hawk") {
		t.Errorf("ctrl+b edited the query: %q cursor %d", p.input.Value(), p.input.Cursor())
	}
	p.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if p.cursor != len(p.filtered)-1 {
		t.Errorf("ctrl+f with a query typed: cursor = %d, want %d", p.cursor, len(p.filtered)-1)
	}
}

// The quick-access digits number the rows on screen counting up from the cursor,
// so opening a group renumbers them and a child is reachable by hotkey.
func TestTreeQuickAccessDigitsFollowVisibleRows(t *testing.T) {
	p, _ := newTreePicker(t)
	focusRow(p, "/hawk")

	// Collapsed: the only row above the cursor is the neighbour.
	if got := quickAccessTarget(p, 1); got != "/old" {
		t.Errorf("alt+1 while collapsed = %q, want the row above the cursor", got)
	}

	pressRight(p)
	p.list.SetCursor(len(p.filtered) - 1) // the last child
	p.syncFromList()
	if got := quickAccessTarget(p, 1); got != "/wt/fix" {
		t.Errorf("alt+1 after expanding = %q, want the child now above the cursor", got)
	}
	if got := quickAccessTarget(p, 2); got != "/hawk" {
		t.Errorf("alt+2 after expanding = %q, want the parent", got)
	}
	if got := quickAccessTarget(p, 3); got != "/old" {
		t.Errorf("alt+3 after expanding = %q, want the neighbour, pushed down by two rows", got)
	}
}

// quickAccessTarget presses alt+<n> and reports the path it selected.
func quickAccessTarget(p *Picker, n int) string {
	p.Update(tea.KeyPressMsg{Code: rune('0' + n), Text: string(rune('0' + n)), Mod: tea.ModAlt})
	if p.result.Selected == nil {
		return ""
	}
	path := p.result.Selected.Path
	p.result = Result{}
	return path
}

// A picker with no tree keeps the arrows as query cursor keys, whatever the query
// is — nothing about the flat list changes.
func TestPickerWithoutTreeLeavesArrowsToTheQuery(t *testing.T) {
	p := NewPicker([]Item{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}})
	p.width, p.height = 60, 10
	p.list.Resize(p.height)
	p.Init()
	typeQuery(p, "b")

	pressLeft(p)
	if p.input.Cursor() != 0 {
		t.Errorf("left did not move the query cursor: at %d", p.input.Cursor())
	}
	if p.treeActive() {
		t.Error("a picker with no tree reported the tree gestures active")
	}
}
