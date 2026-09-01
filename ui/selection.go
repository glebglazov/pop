package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// SelectionMode is the word a surface holds at the left of its bottom line while
// rows are marked. It is the whole visible difference between a plural surface
// and a singular one, which is why it outranks a flash for that line.
const SelectionMode = "-- SELECT --"

// Selection is the human's mark over a list of rows: the set of row keys tab was
// pressed on. It is run-scoped — one command invocation holds it and nothing
// persists it — and keyed by whatever stable identity the surface already gives
// a row (a pane id on the Monitor, a container's cursor key on the Work
// dashboard), which is what carries a mark across the wholesale row rebuild both
// surfaces perform on every poll (ADR-0246 decision 1).
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

// CaptionRule is the dim rule a named block of list chrome opens or closes with:
// label set into a horizontal rule that spans width, so the block reads as a
// block rather than as one more row. It is one grammar for every such rule on a
// list surface — the Selection area's counted divider and an action menu's top
// caption are the same object seen twice, which is what lets a human read a
// bottom-anchored menu as being *about* something without adjacency to say so
// (ADR-0224 decision 5). A non-positive width still draws a short rule so
// callers that have not sized the surface yet do not lose the label.
func CaptionRule(label string, width int) string {
	const dash = "─"
	label = " " + label + " "
	left := 3
	if width <= 0 {
		return dimStyle().Render(strings.Repeat(dash, left) + label + strings.Repeat(dash, left))
	}
	labelW := lipgloss.Width(label)
	if width <= labelW {
		return dimStyle().Render(TruncateToWidth(strings.Repeat(dash, left)+label, width))
	}
	rest := width - labelW
	if left > rest {
		left = rest
	}
	right := rest - left
	return dimStyle().Render(strings.Repeat(dash, left) + label + strings.Repeat(dash, right))
}

// ScrollEdgeLine puts a hidden-row count at the right end of existing chrome.
// A zero count leaves the chrome unchanged.
func ScrollEdgeLine(line string, width int, arrow string, hidden int) string {
	edge := ScrollEdge(arrow, hidden)
	if edge == "" {
		return TruncateString(line, width)
	}
	edgeWidth := lipgloss.Width(edge)
	if width <= edgeWidth {
		return TruncateToWidth(edge, width)
	}
	line = TruncateToWidth(line, width-edgeWidth-1)
	padding := width - lipgloss.Width(line) - edgeWidth
	return line + strings.Repeat(" ", max(padding, 1)) + edge
}

// SelectionSeparator is the dim rule that opens the Selection area. It is also
// the boundary between two lists, so it carries the ordinary rows hidden below
// at its left and the region rows hidden above at its right.
func SelectionSeparator(count, width int, edge ...ScrollEdges) string {
	if len(edge) == 0 || (edge[0].Below == 0 && edge[0].RegionAbove == 0) {
		return CaptionRule(fmt.Sprintf("%d selected", count), width)
	}
	left := ScrollEdge("↓", edge[0].Below)
	right := ScrollEdge("↑", edge[0].RegionAbove)
	return captionRuleWithEnds(fmt.Sprintf("%d selected", count), width, left, right)
}

func captionRuleWithEnds(label string, width int, left, right string) string {
	plainLeft, plainRight := StripANSI(left), StripANSI(right)
	label = " " + label + " "
	if width <= 0 {
		return dimStyle().Render(strings.TrimSpace(plainLeft + " ───" + label + "─── " + plainRight))
	}
	fixed := lipgloss.Width(label)
	if plainLeft != "" {
		fixed += lipgloss.Width(plainLeft) + 1
	}
	if plainRight != "" {
		fixed += lipgloss.Width(plainRight) + 1
	}
	if width <= fixed {
		return dimStyle().Render(TruncateToWidth(strings.TrimSpace(plainLeft+" "+label+" "+plainRight), width))
	}
	dashes := width - fixed
	leftDashes := dashes / 2
	rightDashes := dashes - leftDashes
	var b strings.Builder
	if plainLeft != "" {
		b.WriteString(plainLeft)
		b.WriteByte(' ')
	}
	b.WriteString(strings.Repeat("─", leftDashes))
	b.WriteString(label)
	b.WriteString(strings.Repeat("─", rightDashes))
	if plainRight != "" {
		b.WriteByte(' ')
		b.WriteString(plainRight)
	}
	return dimStyle().Render(b.String())
}

// ConfirmPrompt is the inline y/N question a bulk verb asks before it runs,
// rendered for the bottom line the hints and the flash share. Both plural
// surfaces ask it the same way, in the house accent, because the grammar is the
// whole of what a human has to recognise: a line ending in `? y/N` means one key
// is about to write over everything marked (ADR-0246 decision 7).
func ConfirmPrompt(label string) string {
	return killPromptStyle().Render("  " + label + "? y/N")
}

// SelectionRegion is the list Region a Selection of count rows asks for: the
// marked rows reserved at the foot of the viewport, below the house separator.
func SelectionRegion(count int) Region {
	return Region{
		Count: count,
		Separator: func(count, width int, edges ScrollEdges) string {
			return SelectionSeparator(count, width, edges)
		},
	}
}
