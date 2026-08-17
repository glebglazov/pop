package ui

import "fmt"

// SelectionMode is the word a surface holds at the left of its bottom line while
// rows are marked. It is the whole visible difference between a plural surface
// and a singular one, which is why it outranks a flash for that line.
const SelectionMode = "-- SELECT --"

// Selection is the human's mark over a list of rows: the set of row keys tab was
// pressed on. It is run-scoped — one command invocation holds it and nothing
// persists it — and keyed by whatever stable identity the surface already gives
// a row (a pane id on the Monitor, a container's cursor key on the Work
// dashboard), which is what carries a mark across the wholesale row rebuild both
// surfaces perform on every poll (ADR-0215 decision 1).
//
// Selection mode is derived rather than stored: it is exactly Active, so
// dropping the last mark leaves the mode and no second flag can desync from the
// marks and aim a verb at the wrong target.
//
// The zero value is an empty Selection, ready to use.
type Selection struct {
	keys map[string]bool
}

// Toggle marks an unmarked key and unmarks a marked one. It reports whether the
// key is marked afterwards.
func (s *Selection) Toggle(key string) bool {
	if key == "" {
		return false
	}
	if s.keys[key] {
		delete(s.keys, key)
		return false
	}
	if s.keys == nil {
		s.keys = make(map[string]bool)
	}
	s.keys[key] = true
	return true
}

// Has reports whether the row with this key is marked.
func (s *Selection) Has(key string) bool { return s.keys[key] }

// Len is how many rows are marked — the count the area's separator states.
func (s *Selection) Len() int { return len(s.keys) }

// Active reports selection mode: the Selection is non-empty.
func (s *Selection) Active() bool { return len(s.keys) > 0 }

// Clear drops every mark, which is how the mode is left.
func (s *Selection) Clear() { s.keys = nil }

// Retain drops the marks whose rows are gone. A row that leaves the surface's
// own set takes its mark with it and says nothing: the human marked a row, and a
// row that no longer exists cannot be a target.
func (s *Selection) Retain(present func(key string) bool) {
	for key := range s.keys {
		if !present(key) {
			delete(s.keys, key)
		}
	}
}

// SplitSelected divides rows into the marked ones and the rest, each half keeping
// the caller's own order — so the area reads as the same list filtered to the
// marks rather than as a record of the order they were made in. A row is moved
// and never copied: it lands in exactly one of the two halves, which is what
// keeps one row one key for the list's cursor and its key-based re-anchoring.
func SplitSelected[T any](s *Selection, rows []T, key func(T) string) (marked, rest []T) {
	for _, row := range rows {
		if s.Has(key(row)) {
			marked = append(marked, row)
		} else {
			rest = append(rest, row)
		}
	}
	return marked, rest
}

// SelectionSeparator is the dim line that divides the Selection area from the
// rest of the list. It carries the count, which is what makes the cap below a
// rendering limit rather than a narrowing: the number of targets is stated even
// when not every one is drawn.
func SelectionSeparator(count int) string {
	return dimStyle.Render(fmt.Sprintf("  %d selected", count))
}

// SelectionOverflow is the line that stands in for the members the viewport cap
// left out of the area.
func SelectionOverflow(hidden int) string {
	return dimStyle.Render(fmt.Sprintf("  … +%d more selected", hidden))
}

// ConfirmPrompt is the inline y/N question a bulk verb asks before it runs,
// rendered for the bottom line the hints and the flash share. Both plural
// surfaces ask it the same way, in the house accent, because the grammar is the
// whole of what a human has to recognise: a line ending in `? y/N` means one key
// is about to write over everything marked (ADR-0215 decision 7).
func ConfirmPrompt(label string) string {
	return killPromptStyle.Render("  " + label + "? y/N")
}

// SelectionRegion is the list Region a Selection of count rows asks for: the
// marked rows reserved at the top of the list, under the house separator and
// overflow lines.
func SelectionRegion(count int) Region {
	return Region{
		Count:     count,
		Separator: SelectionSeparator,
		Overflow:  SelectionOverflow,
	}
}
