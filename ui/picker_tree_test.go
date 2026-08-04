package ui

import (
	"fmt"
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

// Right is one gesture doing two things in sequence: it opens a closed row and
// lands on the bottom of the group it opened, and from an open parent it walks
// into the first child.
func TestTreeRightExpandsThenDescends(t *testing.T) {
	p, tree := newTreePicker(t)
	focusRow(p, "/hawk")

	pressRight(p)
	assertListed(t, p, "old", "hawk "+testExpanded, "  fix", "  spike")
	if got := p.filtered[p.cursor].Path; got != "/wt/spike" {
		t.Fatalf("cursor after expand = %q, want the last child of the group", got)
	}
	if !tree.expanded["/hawk"] {
		t.Error("expansion was not recorded with the caller")
	}

	focusRow(p, "/hawk")
	pressRight(p)
	if got := p.filtered[p.cursor].Path; got != "/wt/fix" {
		t.Errorf("cursor after right on an open parent = %q, want the first child", got)
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

// groupTree is the fixture for the scrolling gestures: a group of arbitrary size
// with plain rows above and below it, so a viewport can be smaller than the group
// and rows below it can be watched for movement.
type groupTree struct {
	before, children, after int
	expanded                bool
}

func (g *groupTree) rows(string) []Item {
	var rows []Item
	for i := range g.before {
		rows = append(rows, Item{Name: fmt.Sprintf("a%d", i), Path: fmt.Sprintf("/a%d", i)})
	}
	parent := Item{Name: "hawk", Path: "/hawk", Disclosure: testCollapsed}
	if g.expanded {
		parent.Disclosure = testExpanded
	}
	rows = append(rows, parent)
	if g.expanded {
		for i := range g.children {
			rows = append(rows, Item{Name: fmt.Sprintf("c%d", i), Path: fmt.Sprintf("/wt/c%d", i), Depth: 1})
		}
	}
	for i := range g.after {
		rows = append(rows, Item{Name: fmt.Sprintf("b%d", i), Path: fmt.Sprintf("/b%d", i)})
	}
	return rows
}

func newGroupPicker(t *testing.T, g *groupTree, height int, quickAccess bool) *Picker {
	t.Helper()
	tree := Tree{
		Rows:        g.rows,
		SetExpanded: func(_ string, expand bool) { g.expanded = expand },
	}
	opts := []PickerOption{WithTree(tree)}
	if quickAccess {
		opts = append(opts, WithQuickAccess("alt"))
	}
	p := NewPicker(g.rows(""), opts...)
	p.width = 60
	p.height = height
	p.list.Resize(height)
	p.Init()
	return p
}

// rowIndex and screenLine read where a path sits in the list and on the screen;
// -1 means it is not on screen at all.
func rowIndex(p *Picker, path string) int {
	for i, it := range p.filtered {
		if it.Path == path {
			return i
		}
	}
	return -1
}

func screenLine(p *Picker, path string) int {
	idx := rowIndex(p, path)
	if idx < 0 {
		return -1
	}
	line := idx - p.list.Scroll()
	if line < 0 || line >= p.height {
		return -1
	}
	return line
}

// Opening a group is a request to see it, so the cursor lands on the group's last
// child: the list follows the cursor and the whole group scrolls in behind it. The
// last child sits on the bottom line — the expand jump ignores the quick-access
// context margin — and the nine children above it carry the quick-access digits.
// A group taller than the viewport pushes its parent off the top; left collapses
// and lands the cursor back on it.
func TestTreeExpandRevealsTheWholeGroup(t *testing.T) {
	g := &groupTree{before: 3, children: 12}
	p := newGroupPicker(t, g, 10, true)
	focusRow(p, "/hawk")

	pressRight(p)
	if got := p.filtered[p.cursor].Path; got != "/wt/c11" {
		t.Fatalf("cursor after expand = %q, want the group's last child", got)
	}
	if got := screenLine(p, "/wt/c11"); got != p.height-1 {
		t.Errorf("last child on screen line %d, want the bottom line %d", got, p.height-1)
	}
	if got := screenLine(p, "/hawk"); got != -1 {
		t.Errorf("parent still on screen at line %d; a group taller than the viewport pushes it off", got)
	}
	for line, want := range map[int]string{0: "/wt/c2", 9: "/wt/c11"} {
		if got := screenLine(p, want); got != line {
			t.Errorf("%s on screen line %d, want %d — the group did not scroll in whole", want, got, line)
		}
	}

	// Every child on screen above the cursor is one alt+digit away.
	for dist := 1; dist <= 9; dist++ {
		want := fmt.Sprintf("/wt/c%d", 11-dist)
		if got := quickAccessTarget(p, dist); got != want {
			t.Errorf("alt+%d = %q, want %q", dist, got, want)
		}
	}

	pressLeft(p)
	if got := p.filtered[p.cursor].Path; got != "/hawk" {
		t.Errorf("cursor after collapsing = %q, want the parent back", got)
	}
	// The collapsed list no longer fills the viewport: the clamp wins, the bottom
	// anchor pads above, and no row is invented to hold the parent's old line.
	if p.list.Scroll() != 0 || len(p.filtered) != 4 {
		t.Errorf("after collapsing: scroll %d over %d rows, want 0 over 4", p.list.Scroll(), len(p.filtered))
	}
}

// Collapsing reverses the expand literally: the rows below the group keep their
// screen lines and the parent lands where its last visible child sat. The offset
// is read at collapse time, so moving around inside the open group cannot make the
// landing stale.
func TestTreeCollapseKeepsRowsBelowOnTheirLines(t *testing.T) {
	g := &groupTree{before: 12, children: 3, after: 12}
	p := newGroupPicker(t, g, 10, false)
	focusRow(p, "/hawk")
	pressRight(p)

	// Wander inside the open group: down past its end, then back onto a child.
	p.list.SetCursor(rowIndex(p, "/b1"))
	p.syncFromList()
	focusRow(p, "/wt/c1")

	lastVisibleChild := screenLine(p, "/wt/c2")
	below := map[string]int{"/b0": screenLine(p, "/b0"), "/b1": screenLine(p, "/b1")}
	if lastVisibleChild < 0 || below["/b0"] < 0 || below["/b1"] < 0 {
		t.Fatalf("fixture is off screen: last child %d, rows below %v", lastVisibleChild, below)
	}

	pressLeft(p)
	if got := p.filtered[p.cursor].Path; got != "/hawk" {
		t.Fatalf("cursor after collapsing = %q, want the parent", got)
	}
	if got := screenLine(p, "/hawk"); got != lastVisibleChild {
		t.Errorf("parent landed on line %d, want %d — where its last visible child sat", got, lastVisibleChild)
	}
	for path, want := range below {
		if got := screenLine(p, path); got != want {
			t.Errorf("%s moved from line %d to %d; rows below the group must hold their lines", path, want, got)
		}
	}
}
