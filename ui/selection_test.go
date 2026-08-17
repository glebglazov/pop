package ui

import (
	"strings"
	"testing"
)

// row stands in for another surface's row: a type the Selection has never heard
// of, keyed by whatever identity that surface already carries. It is what the
// primitive being shared means — the Work dashboard brings its own container and
// its own cursor key and gets the same Selection.
type row struct {
	key  string
	name string
}

func rowKey(r row) string { return r.key }

// TestSelectionIsKeyedAndDerived pins the primitive's own contract: marks are
// keys, the mode is the set being non-empty, and a row whose key has gone loses
// its mark.
func TestSelectionIsKeyedAndDerived(t *testing.T) {
	var s Selection
	if s.Active() || s.Len() != 0 {
		t.Fatal("a fresh Selection is not empty")
	}

	if !s.Toggle("b") || !s.Active() {
		t.Fatal("toggling a key did not mark it")
	}
	s.Toggle("d")
	if s.Toggle("b") {
		t.Error("toggling a marked key did not unmark it")
	}
	if s.Len() != 1 || !s.Has("d") {
		t.Fatalf("selection holds %d keys, want only d", s.Len())
	}

	s.Toggle("gone")
	s.Retain(func(key string) bool { return key == "d" })
	if s.Len() != 1 || !s.Has("d") {
		t.Fatalf("Retain left %d keys, want only the one still present", s.Len())
	}

	s.Clear()
	if s.Active() {
		t.Error("Clear left the mode on")
	}
}

// TestSplitSelectedMovesRows pins that a marked row is moved and never copied,
// and that both halves keep the caller's own order rather than the order the
// marks were made in.
func TestSplitSelectedMovesRows(t *testing.T) {
	rows := []row{{"a", "one"}, {"b", "two"}, {"c", "three"}, {"d", "four"}}
	var s Selection
	s.Toggle("d")
	s.Toggle("b")

	marked, rest := SplitSelected(&s, rows, rowKey)
	if got := keysOf(marked); got != "b,d" {
		t.Errorf("marked = %s, want b,d in the caller's order", got)
	}
	if got := keysOf(rest); got != "a,c" {
		t.Errorf("rest = %s, want a,c", got)
	}
	if len(marked)+len(rest) != len(rows) {
		t.Errorf("%d rows out of %d in — a row must land in exactly one half", len(marked)+len(rest), len(rows))
	}
}

// TestSelectionRegionChrome pins the lines the area is read by, which both
// surfaces render from this one place.
func TestSelectionRegionChrome(t *testing.T) {
	region := SelectionRegion(5)
	if region.Count != 5 {
		t.Errorf("region count = %d, want 5", region.Count)
	}
	if got := StripANSI(region.Separator(5)); strings.TrimSpace(got) != "5 selected" {
		t.Errorf("separator = %q, want the count", got)
	}
	if got := StripANSI(region.Overflow(2)); strings.TrimSpace(got) != "… +2 more selected" {
		t.Errorf("overflow = %q, want the hidden count", got)
	}
}

func keysOf(rows []row) string {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	return strings.Join(keys, ",")
}
