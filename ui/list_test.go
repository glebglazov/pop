package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func strItems(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "item-" + strconv.Itoa(i)
	}
	return out
}

func newTestList(items []string, anchor Anchor, wrap bool, margin int, quickLabel func(int) string) *List[string] {
	return NewList(items, Opts[string]{
		Key:          func(s string) string { return s },
		Cell:         func(s string, _ RowState) string { return s },
		Wrap:         wrap,
		Anchor:       anchor,
		ScrollMargin: margin,
		QuickLabel:   quickLabel,
	})
}

func TestListVisibleRowsExactHeight(t *testing.T) {
	l := newTestList(strItems(3), AnchorTop, false, 0, nil)
	l.Resize(5)
	rows := l.VisibleRows()
	if len(rows) != 5 {
		t.Fatalf("VisibleRows() len = %d, want 5", len(rows))
	}
}

func TestListAnchorTop(t *testing.T) {
	l := newTestList(strItems(2), AnchorTop, false, 0, nil)
	l.Resize(4)
	rows := l.VisibleRows()
	if rows[0] == "" || rows[1] == "" {
		t.Fatalf("top anchor should place items first, got %q %q", rows[0], rows[1])
	}
	if rows[2] != "" || rows[3] != "" {
		t.Fatalf("top anchor should pad below, got %q %q", rows[2], rows[3])
	}
}

func TestListAnchorBottom(t *testing.T) {
	l := newTestList(strItems(2), AnchorBottom, false, 0, nil)
	l.Resize(4)
	rows := l.VisibleRows()
	if rows[0] != "" || rows[1] != "" {
		t.Fatalf("bottom anchor should pad above, got %q %q", rows[0], rows[1])
	}
	if rows[2] == "" || rows[3] == "" {
		t.Fatalf("bottom anchor should place items last, got %q %q", rows[2], rows[3])
	}
}

func TestListMoveUpDownWrap(t *testing.T) {
	l := newTestList(strItems(3), AnchorTop, true, 0, nil)
	l.SetCursor(0)

	l.MoveUp()
	if l.Cursor() != 2 {
		t.Fatalf("wrap up from 0: cursor = %d, want 2", l.Cursor())
	}

	l.MoveDown()
	if l.Cursor() != 0 {
		t.Fatalf("wrap down from 2: cursor = %d, want 0", l.Cursor())
	}
}

func TestListMoveUpDownNoWrap(t *testing.T) {
	l := newTestList(strItems(3), AnchorTop, false, 0, nil)
	l.SetCursor(0)
	l.MoveUp()
	if l.Cursor() != 0 {
		t.Fatalf("no wrap up: cursor = %d, want 0", l.Cursor())
	}
	l.SetCursor(2)
	l.MoveDown()
	if l.Cursor() != 2 {
		t.Fatalf("no wrap down: cursor = %d, want 2", l.Cursor())
	}
}

func TestListHalfPage(t *testing.T) {
	l := newTestList(strItems(20), AnchorTop, false, 0, nil)
	l.Resize(5)
	l.SetCursor(10)

	l.HalfPageUp()
	if l.Cursor() != 5 {
		t.Fatalf("HalfPageUp: cursor = %d, want 5", l.Cursor())
	}

	l.HalfPageDown()
	if l.Cursor() != 10 {
		t.Fatalf("HalfPageDown: cursor = %d, want 10", l.Cursor())
	}

	l.SetCursor(2)
	l.HalfPageUp()
	if l.Cursor() != 0 {
		t.Fatalf("HalfPageUp clamp: cursor = %d, want 0", l.Cursor())
	}

	l.SetCursor(18)
	l.HalfPageDown()
	if l.Cursor() != 19 {
		t.Fatalf("HalfPageDown clamp: cursor = %d, want 19", l.Cursor())
	}
}

func TestListScrollMargin(t *testing.T) {
	l := newTestList(strItems(20), AnchorTop, false, 3, nil)
	l.Resize(10)
	l.SetCursor(15)

	// Cursor should not be at top of viewport; margin keeps lines above.
	before := l.scroll
	if l.Cursor()-before < 3 {
		t.Fatalf("scroll margin: cursor-scroll = %d, want >= 3 (scroll=%d cursor=%d)",
			l.Cursor()-before, before, l.Cursor())
	}
}

func TestListReplaceItemsReanchor(t *testing.T) {
	l := newTestList([]string{"a", "b", "c"}, AnchorTop, false, 0, nil)
	l.SetCursor(1)
	l.ReplaceItems([]string{"x", "b", "y"})
	if l.Cursor() != 1 {
		t.Fatalf("after replace, cursor = %d, want 1 (re-anchored on b)", l.Cursor())
	}
}

func TestListReplaceItemsClamp(t *testing.T) {
	l := newTestList([]string{"a", "b", "c"}, AnchorTop, false, 0, nil)
	l.SetCursor(2)
	l.ReplaceItems([]string{"x", "y"})
	if l.Cursor() != 1 {
		t.Fatalf("after replace with missing key, cursor = %d, want 1 (clamped)", l.Cursor())
	}
}

func TestListSetCursorToKey(t *testing.T) {
	l := newTestList([]string{"a", "b", "c"}, AnchorTop, false, 0, nil)
	if !l.SetCursorToKey("b") {
		t.Fatal("SetCursorToKey(b) = false, want true")
	}
	if l.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", l.Cursor())
	}
	if l.SetCursorToKey("missing") {
		t.Fatal("SetCursorToKey(missing) = true, want false")
	}
}

func TestListSelected(t *testing.T) {
	l := newTestList([]string{"a", "b"}, AnchorTop, false, 0, nil)
	l.SetCursor(1)
	item, ok := l.Selected()
	if !ok || item != "b" {
		t.Fatalf("Selected() = %q, %v, want b, true", item, ok)
	}
	l.ReplaceItems(nil)
	_, ok = l.Selected()
	if ok {
		t.Fatal("Selected() on empty list should be false")
	}
}

func TestListIndicatorAndPadding(t *testing.T) {
	l := newTestList([]string{"only"}, AnchorTop, false, 0, nil)
	l.Resize(1)
	rows := l.VisibleRows()
	plain := StripANSI(rows[0])
	if !strings.Contains(plain, "█") {
		t.Fatalf("selected row missing indicator: %q", plain)
	}
	if !strings.HasSuffix(plain, "only") {
		t.Fatalf("selected row missing cell: %q", plain)
	}

	l.ReplaceItems([]string{"first", "second"})
	l.Resize(2)
	l.SetCursor(1)
	rows = l.VisibleRows()
	// second row is selected; find the line with "second"
	foundSelected := false
	foundUnselected := false
	for _, row := range rows {
		plain := StripANSI(row)
		if strings.Contains(plain, "second") && strings.Contains(plain, "█") {
			foundSelected = true
		}
		if strings.Contains(plain, "first") && !strings.Contains(plain, "█") {
			if !strings.HasPrefix(plain, "  ") {
				t.Fatalf("unselected row should have 2-space prefix: %q", plain)
			}
			foundUnselected = true
		}
	}
	if !foundSelected || !foundUnselected {
		t.Fatalf("indicator test: selected=%v unselected=%v rows=%v", foundSelected, foundUnselected, rows)
	}
}

func TestListQuickAccessPrefix(t *testing.T) {
	q := NewQuickAccess("alt")
	l := newTestList(strItems(5), AnchorBottom, false, 9, q.LabelFunc())
	l.Resize(5)
	l.SetCursor(4)

	rows := l.VisibleRows()
	foundLabel := false
	for _, row := range rows {
		if strings.Contains(StripANSI(row), "⌥1") {
			foundLabel = true
			break
		}
	}
	if !foundLabel {
		t.Fatalf("quick access label missing from rows: %v", rows)
	}
}

func TestListResizeReclampsScroll(t *testing.T) {
	l := newTestList(strItems(30), AnchorTop, false, 0, nil)
	l.Resize(10)
	l.SetCursor(25)
	l.Resize(5)
	rows := l.VisibleRows()
	if len(rows) != 5 {
		t.Fatalf("after resize VisibleRows len = %d, want 5", len(rows))
	}
}

func newMultilineTestList(items []string) *List[string] {
	return NewList(items, Opts[string]{
		Key:  func(s string) string { return s },
		Cell: func(s string, rs RowState) string { return fmt.Sprintf("%s/%d", s, rs.LineIndex) },
		LinesPerItem: 2,
	})
}

func TestListMultilineVisibleRowsCount(t *testing.T) {
	l := newMultilineTestList(strItems(5))
	l.Resize(6)
	rows := l.VisibleRows()
	if len(rows) != 6 {
		t.Fatalf("VisibleRows() len = %d, want 6", len(rows))
	}
	want := []string{
		"item-0/0", "item-0/1",
		"item-1/0", "item-1/1",
		"item-2/0", "item-2/1",
	}
	for i, w := range want {
		plain := StripANSI(rows[i])
		if !strings.HasSuffix(plain, w) {
			t.Fatalf("row %d = %q, want suffix %q", i, plain, w)
		}
	}
}

func TestListMultilineMoveDownSkipsTwoLines(t *testing.T) {
	l := newMultilineTestList(strItems(5))
	l.Resize(6)
	l.SetCursor(0)
	l.MoveDown()
	if l.Cursor() != 1 {
		t.Fatalf("MoveDown cursor = %d, want 1", l.Cursor())
	}
	rows := l.VisibleRows()
	if len(rows) != 6 {
		t.Fatalf("VisibleRows() len = %d, want 6", len(rows))
	}
	// The selected item (item-1) occupies rows[2] and rows[3]; only the first
	// row of the selected item carries the cursor indicator.
	selectedFirst := false
	selectedSecond := false
	for i, row := range rows {
		plain := StripANSI(row)
		if strings.Contains(plain, "item-1/0") && strings.Contains(plain, "█") {
			selectedFirst = true
		}
		if strings.Contains(plain, "item-1/1") && strings.Contains(plain, "█") {
			selectedSecond = true
		}
		if i < 2 && strings.Contains(plain, "█") {
			t.Fatalf("cursor indicator appeared above selected item at row %d", i)
		}
	}
	if !selectedFirst {
		t.Fatalf("selected item's first line missing indicator")
	}
	if selectedSecond {
		t.Fatalf("selected item's second line should not carry the indicator")
	}
}

func TestListMultilineScrollWindow(t *testing.T) {
	l := newMultilineTestList(strItems(10))
	l.Resize(4)
	l.SetCursor(8)

	if l.Scroll() != 7 {
		t.Fatalf("scroll = %d, want 7", l.Scroll())
	}
	rows := l.VisibleRows()
	if len(rows) != 4 {
		t.Fatalf("VisibleRows() len = %d, want 4", len(rows))
	}
	for _, row := range rows {
		plain := StripANSI(row)
		if plain == "" {
			continue
		}
		if !strings.Contains(plain, "item-7") && !strings.Contains(plain, "item-8") {
			t.Fatalf("unexpected visible item in row %q", plain)
		}
	}
}

func TestListMultilineAnchorBottom(t *testing.T) {
	l := NewList(strItems(2), Opts[string]{
		Key:          func(s string) string { return s },
		Cell:         func(s string, rs RowState) string { return fmt.Sprintf("%s/%d", s, rs.LineIndex) },
		Anchor:       AnchorBottom,
		LinesPerItem: 2,
	})
	l.Resize(6)
	rows := l.VisibleRows()
	if len(rows) != 6 {
		t.Fatalf("VisibleRows() len = %d, want 6", len(rows))
	}
	for i := 0; i < 2; i++ {
		if rows[i] != "" {
			t.Fatalf("anchor bottom should pad above: rows[%d] = %q", i, rows[i])
		}
	}
	for i, want := range []string{"item-0/0", "item-0/1", "item-1/0", "item-1/1"} {
		plain := StripANSI(rows[i+2])
		if !strings.HasSuffix(plain, want) {
			t.Fatalf("row %d = %q, want suffix %q", i+2, plain, want)
		}
	}
}
