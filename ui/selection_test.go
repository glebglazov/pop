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
	if got := StripANSI(region.Separator(5, 40)); !strings.Contains(got, "5 selected") {
		t.Errorf("separator = %q, want the count set into the rule", got)
	}
	if got := StripANSI(region.Overflow(2)); strings.TrimSpace(got) != "… +2 more selected" {
		t.Errorf("overflow = %q, want the hidden count", got)
	}
}

// TestSelectionSeparatorIsARule pins the divider's shape: a dim horizontal rule
// that spans the width it is given, with the count set into it, and truncates
// to that width on a narrow terminal rather than wrapping.
func TestSelectionSeparatorIsARule(t *testing.T) {
	wide := StripANSI(SelectionSeparator(2, 40))
	if got := len([]rune(wide)); got != 40 {
		t.Fatalf("wide rule width = %d, want 40: %q", got, wide)
	}
	if !strings.HasPrefix(wide, "───") || !strings.HasSuffix(wide, "─") {
		t.Errorf("wide rule = %q, want leading and trailing box-drawing dashes", wide)
	}
	if !strings.Contains(wide, "2 selected") {
		t.Errorf("wide rule = %q, want the count set into it", wide)
	}
	if strings.Contains(wide, "\n") {
		t.Errorf("wide rule wrapped: %q", wide)
	}

	narrow := StripANSI(SelectionSeparator(2, 10))
	if got := len([]rune(narrow)); got != 10 {
		t.Fatalf("narrow rule width = %d, want 10: %q", got, narrow)
	}
	if strings.Contains(narrow, "\n") {
		t.Errorf("narrow rule wrapped: %q", narrow)
	}
}

func keysOf(rows []row) string {
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	return strings.Join(keys, ",")
}
