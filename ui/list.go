package ui

import (
	"fmt"
	"strings"
)

// Anchor controls where list rows sit within the viewport when fewer items
// than the body height are visible.
type Anchor int

const (
	// AnchorTop pins rows to the top of the viewport (empty lines below).
	AnchorTop Anchor = iota
	// AnchorBottom pins rows to the bottom (fzf-style blank lines above).
	AnchorBottom
)

// RowState is passed to the Cell renderer for each visible row/sub-line.
type RowState struct {
	Selected   bool
	Lifted     bool
	QuickLabel string
	Width      int
	// LineIndex is the zero-based sub-line inside a multi-line item. It is 0
	// for single-line items and for the first line of multi-line items.
	LineIndex int
}

// Opts configures a List's identity, rendering, and navigation behavior.
type Opts[T any] struct {
	Key          func(T) string           // stable identity for restore
	Cell         func(T, RowState) string // row content within RowState.Width
	Wrap         bool                     // up-at-top wraps to bottom
	Anchor       Anchor                   // Top | Bottom (fzf-style)
	ScrollMargin int                      // lines kept above cursor (quick-access reserves ~9)
	QuickLabel   func(dist int) string    // optional; nil = no quick-access column
	// Lifted marks a row the caller lifted to the top of the item slice, so the
	// list can say so in the prefix column's second cell. It is optional; nil
	// leaves that cell blank. A list with a QuickLabel has no room for it — both
	// want the same cell — so the two are not wired together.
	Lifted func(T) bool
	// LinesPerItem is the number of terminal lines each logical item occupies.
	// Defaults to 1. Cursor movement still operates on logical items.
	LinesPerItem int
	// TopEdgeOnChrome tells List that its consumer will put the hidden-above
	// count on existing chrome. The default is false, so a plain List spends a
	// row of its own and every picker gets a Scroll edge without opting in.
	TopEdgeOnChrome bool
}

// Region is a reserved block of trailing items the list keeps at the foot of the
// viewport, divided from the ordinary rows by a line of its own. It is how a
// Selection area reaches the screen (ADR-0224 decision 1): the caller moves the
// marked rows to the end of the item slice and says how many there are, and the
// list spends the lines — capping the block at a third of the viewport and
// saying how many members the cap left out. The cursor still walks one flat item
// slice, so a row in the region is reached and left like any other row.
type Region struct {
	// Count is how many trailing items belong to the region; zero means none.
	Count int
	// Separator renders the line between the region and the rest of the list.
	// Width is the visible columns the list is drawing into; the separator fills
	// that width rather than guessing one.
	Separator func(count, width int, edges ScrollEdges) string
}

// ScrollEdges counts the rows hidden beyond each boundary of a List render.
// Above and Below describe the ordinary list. RegionAbove and RegionBelow
// describe the reserved foot region.
type ScrollEdges struct {
	Above       int
	Below       int
	RegionAbove int
	RegionBelow int
}

// ScrollEdge renders one hidden-row count in the shared List grammar.
func ScrollEdge(arrow string, hidden int) string {
	if hidden <= 0 {
		return ""
	}
	return dimStyle().Render(fmt.Sprintf("%s %d", arrow, hidden))
}

// List is a passive, generic scrolling-list viewport. It owns cursor, scroll,
// height, navigation, identity-preserving reload, and per-row drawing. Models
// drive it by calling methods; it never sees tea.Msg.
type List[T any] struct {
	items  []T
	cursor int
	scroll int
	height int
	width  int
	opts   Opts[T]
	region Region
}

// NewList creates a list with the given items and options.
func NewList[T any](items []T, opts Opts[T]) *List[T] {
	return &List[T]{
		items:  items,
		height: 10,
		opts:   opts,
	}
}

// Cursor returns the current cursor index.
func (l *List[T]) Cursor() int {
	return l.cursor
}

// Len returns the number of items in the list.
func (l *List[T]) Len() int {
	return len(l.items)
}

// Items returns the current item slice. Callers must not mutate it.
func (l *List[T]) Items() []T {
	return l.items
}

// Scroll returns the scroll offset (index of the first visible item).
func (l *List[T]) Scroll() int {
	return l.scroll
}

// SetCursor moves the cursor to index i, clamped to bounds.
func (l *List[T]) SetCursor(i int) {
	if len(l.items) == 0 {
		l.cursor = 0
		l.adjustScroll()
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= len(l.items) {
		i = len(l.items) - 1
	}
	l.cursor = i
	l.adjustScroll()
}

// MoveUp moves the cursor up, wrapping to the bottom when Wrap is set.
func (l *List[T]) MoveUp() {
	if len(l.items) == 0 {
		return
	}
	if l.cursor > 0 {
		l.cursor--
	} else if l.opts.Wrap {
		l.cursor = len(l.items) - 1
	}
	l.adjustScroll()
}

// MoveDown moves the cursor down, wrapping to the top when Wrap is set.
func (l *List[T]) MoveDown() {
	if len(l.items) == 0 {
		return
	}
	if l.cursor < len(l.items)-1 {
		l.cursor++
	} else if l.opts.Wrap {
		l.cursor = 0
	}
	l.adjustScroll()
}

// HalfPageUp moves the cursor up by one page: the rows the viewport shows, less
// one that stays on screen as the reader's landmark.
func (l *List[T]) HalfPageUp() {
	if len(l.items) == 0 {
		return
	}
	l.cursor -= l.page()
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.adjustScroll()
}

// HalfPageDown moves the cursor down by one page: the rows the viewport shows,
// less one that stays on screen as the reader's landmark.
func (l *List[T]) HalfPageDown() {
	if len(l.items) == 0 {
		return
	}
	l.cursor += l.page()
	if l.cursor >= len(l.items) {
		l.cursor = len(l.items) - 1
	}
	l.adjustScroll()
}

// page is the paging distance. The viewport height is in terminal lines, and a
// row can cost more than one of them, so the distance is counted in the records
// actually on screen — one short, so the row the reader was on stays visible.
func (l *List[T]) page() int {
	return max(l.visibleItems()-1, 1)
}

// Resize sets the viewport body height and reclamps scroll.
func (l *List[T]) Resize(bodyHeight int) {
	l.height = bodyHeight
	l.adjustScroll()
}

// SetWidth sets the visible columns the list draws into. Region separators fill
// this width; row cells receive it on RowState.Width.
func (l *List[T]) SetWidth(w int) {
	l.width = w
}

// Selected returns the item at the cursor, or false when empty or out of bounds.
func (l *List[T]) Selected() (T, bool) {
	var zero T
	if l.cursor < 0 || l.cursor >= len(l.items) {
		return zero, false
	}
	return l.items[l.cursor], true
}

// SetItems swaps the item slice and reclamps the cursor without re-anchoring
// by Key. Callers that need identity restore use SetCursorToKey afterward.
func (l *List[T]) SetItems(items []T) {
	l.items = items
	l.clampCursor()
	l.adjustScroll()
}

// ReplaceItems swaps the item slice, re-anchoring the cursor by Key when
// possible and clamping when the key is gone.
func (l *List[T]) ReplaceItems(items []T) {
	var key string
	if l.cursor >= 0 && l.cursor < len(l.items) && l.opts.Key != nil {
		key = l.opts.Key(l.items[l.cursor])
	}
	l.items = items
	if key != "" && l.opts.Key != nil {
		if !l.SetCursorToKey(key) {
			l.clampCursor()
		}
	} else {
		l.clampCursor()
	}
	l.adjustScroll()
}

// SetCursorToKey moves the cursor to the item with the given key. Returns false
// when no matching item exists.
func (l *List[T]) SetCursorToKey(key string) bool {
	if l.opts.Key == nil {
		return false
	}
	for i, item := range l.items {
		if l.opts.Key(item) == key {
			l.cursor = i
			l.adjustScroll()
			return true
		}
	}
	return false
}

// JumpTo moves the cursor to index i and scrolls just far enough to bring it on
// screen, ignoring ScrollMargin. It serves deliberate jumps to a chosen row,
// where the margin's context lines would push the target away from the very edge
// the jump was aiming at.
func (l *List[T]) JumpTo(i int) {
	if len(l.items) == 0 {
		l.cursor = 0
		l.adjustScroll()
		return
	}
	l.cursor = min(max(i, 0), len(l.items)-1)
	l.rescroll(0)
}

// SetScroll places the viewport at the given offset, clamped so the list still
// fills the viewport and the cursor stays on screen. It lets a caller that knows
// where rows should land say so directly, instead of inferring an offset from
// cursor movement.
func (l *List[T]) SetScroll(scroll int) {
	ordinaryCount := len(l.items) - l.RegionCount()
	visible := l.visibleItemsForScroll(scroll)
	if visible <= 0 {
		l.scroll = 0
		return
	}
	if l.cursor < ordinaryCount {
		if scroll > l.cursor {
			scroll = l.cursor
		}
		if scroll <= l.cursor-visible {
			scroll = l.cursor - visible + 1
		}
	}
	l.scroll = min(max(scroll, 0), max(ordinaryCount-visible, 0))
}

// SetRegion declares the reserved block at the end of the item slice and
// reclamps scroll, because the block's lines come out of the scrolling area.
func (l *List[T]) SetRegion(r Region) {
	l.region = r
	l.adjustScroll()
}

// RegionCount is how many trailing items the region holds — what a region-aware
// jump key measures the edge of the cursor's own region against.
func (l *List[T]) RegionCount() int {
	return min(max(l.region.Count, 0), len(l.items))
}

// regionLayout is the geometry one render gives the region: which of its members
// fit, where the block starts, and how many terminal lines it takes away from the
// scrolling rest.
type regionLayout struct {
	count  int // members
	shown  int // members drawn
	scroll int // first member drawn
	lines  int // terminal lines the whole block takes
}

func (r regionLayout) hiddenBelow() int {
	return max(r.count-r.scroll-r.shown, 0)
}

func (l *List[T]) regionLayout() regionLayout {
	count := l.RegionCount()
	if count == 0 || l.height <= 0 {
		return regionLayout{}
	}
	lpi := l.LinesPerItem()
	// A third of the viewport, and never fewer than one row: the cap narrows
	// nothing, so the area always shows at least the row its count speaks for.
	rows := max(l.height/3/lpi, 1)
	lay := regionLayout{count: count, shown: min(count, rows)}
	// The block scrolls for one reason only: to keep a cursor that walked into it
	// on screen.
	start := len(l.items) - count
	if l.cursor >= start+lay.shown {
		lay.scroll = l.cursor - start - lay.shown + 1
	}
	lay.lines = min(lay.shown*lpi+1, l.height) // the separator always draws
	if lay.hiddenBelow() > 0 {
		lay.lines = min(lay.lines+1, l.height)
	}
	return lay
}

// visibleItems is how many logical items the scrolling part of the viewport can
// show at once — the unit both the scroll offset and the clamp are counted in.
// A region takes both its lines and its members out of that part.
func (l *List[T]) visibleItems() int {
	return l.visibleItemsForScroll(l.scroll)
}

func (l *List[T]) visibleItemsForScroll(scroll int) int {
	lay := l.regionLayout()
	height := l.height - lay.lines
	if !l.opts.TopEdgeOnChrome && scroll > 0 {
		height--
	}
	return min(max(height, 0)/l.LinesPerItem(), len(l.items)-lay.count)
}

// ScrollEdges returns the counts for the current render. Consumers with
// existing chrome use Above there; the List renders the other edges itself.
func (l *List[T]) ScrollEdges() ScrollEdges {
	lay := l.regionLayout()
	visible := l.visibleItems()
	ordinaryCount := len(l.items) - lay.count
	return ScrollEdges{
		Above:       max(l.scroll, 0),
		Below:       max(ordinaryCount-l.scroll-visible, 0),
		RegionAbove: max(lay.scroll, 0),
		RegionBelow: lay.hiddenBelow(),
	}
}

// VisibleRows returns exactly bodyHeight rendered lines. List owns the █
// indicator, quick-access prefix column, padding, and anchor blank lines. When
// LinesPerItem > 1, each logical item is rendered over that many physical lines;
// the cursor prefix and quick-access label appear only on the first line. A
// Region, when one is set, is drawn below its separator and keeps its lines at
// the viewport foot whatever the rest of the list scrolls to.
func (l *List[T]) VisibleRows() []string {
	height := l.height
	if height <= 0 {
		return nil
	}

	lpi := l.LinesPerItem()
	const prefixWidth = 2

	lay := l.regionLayout()
	region := l.regionLines(lay, prefixWidth)
	edges := l.ScrollEdges()

	restHeight := max(height-len(region), 0)
	topEdge := ""
	if !l.opts.TopEdgeOnChrome {
		topEdge = ScrollEdge("↑", edges.Above)
		if topEdge != "" {
			restHeight--
		}
	}
	ordinaryCount := len(l.items) - lay.count
	logicalVisible := min(restHeight/lpi, ordinaryCount)

	start := max(l.scroll, 0)
	if maxStart := ordinaryCount - logicalVisible; start > maxStart {
		start = maxStart
	}

	emptyBefore := 0
	if l.opts.Anchor == AnchorBottom {
		emptyBefore = restHeight - logicalVisible*lpi
	}

	lines := make([]string, 0, height)
	if topEdge != "" {
		lines = append(lines, "  "+topEdge)
	}
	for i := 0; i < emptyBefore; i++ {
		lines = append(lines, "")
	}
	for i := 0; i < logicalVisible; i++ {
		lines = append(lines, l.itemLines(start+i, prefixWidth)...)
	}
	for len(lines) < height-len(region) {
		lines = append(lines, "")
	}
	lines = append(lines, region...)
	return lines[:height]
}

// regionLines draws the reserved block: the separator that divides it from the
// ordinary rows, its visible members, and the note standing in for the ones the
// cap left out.
func (l *List[T]) regionLines(lay regionLayout, prefixWidth int) []string {
	if lay.count == 0 {
		return nil
	}
	out := make([]string, 0, lay.lines)
	edges := l.ScrollEdges()
	if l.region.Separator != nil {
		out = append(out, l.region.Separator(lay.count, l.width, edges))
	} else {
		out = append(out, "")
	}
	start := len(l.items) - lay.count
	for i := start + lay.scroll; i < start+lay.scroll+lay.shown && i < len(l.items); i++ {
		out = append(out, l.itemLines(i, prefixWidth)...)
	}
	if edges.RegionBelow > 0 {
		out = append(out, "  "+ScrollEdge("↓", edges.RegionBelow))
	}
	return out
}

// itemLines renders one logical item as the LinesPerItem terminal lines it
// occupies, prefix column included. The region block and the scrolling rest both
// draw their rows through it, so a row looks the same wherever it sits.
func (l *List[T]) itemLines(idx, prefixWidth int) []string {
	item := l.items[idx]
	selected := idx == l.cursor
	lifted := l.opts.Lifted != nil && l.opts.Lifted(item)

	quickLabel := ""
	if l.opts.QuickLabel != nil && !selected {
		dist := l.cursor - idx
		if dist >= 1 && dist <= 9 {
			quickLabel = l.opts.QuickLabel(dist)
		}
	}

	lpi := l.LinesPerItem()
	out := make([]string, 0, lpi)
	for sub := 0; sub < lpi; sub++ {
		cell := ""
		if l.opts.Cell != nil {
			cell = l.opts.Cell(item, RowState{
				Selected:   selected,
				Lifted:     lifted,
				QuickLabel: quickLabel,
				Width:      l.width,
				LineIndex:  sub,
			})
		}
		isFirstLine := sub == 0
		out = append(out, l.renderPrefix(isFirstLine && selected, isFirstLine && lifted, quickLabel, prefixWidth)+cell)
	}
	return out
}

// renderPrefix draws the two cells every row carries: the cursor block in the
// first, the lift mark in the second. Both cells are always spent, whichever way
// they are filled, so column offsets never shift between one row and the next —
// or between a render that lifts and one that does not.
func (l *List[T]) renderPrefix(selected, lifted bool, quickLabel string, prefixWidth int) string {
	mark := " "
	if lifted {
		mark = indicatorStyle().Render("▸")
	}
	if selected {
		indicator := indicatorStyle().Render("█")
		if l.opts.QuickLabel != nil {
			return strings.Repeat(" ", prefixWidth-1) + indicator
		}
		return indicator + mark
	}
	if quickLabel != "" {
		return dimStyle().Render(quickLabel)
	}
	return strings.Repeat(" ", prefixWidth-1) + mark
}

func (l *List[T]) clampCursor() {
	if len(l.items) == 0 {
		l.cursor = 0
		return
	}
	if l.cursor >= len(l.items) {
		l.cursor = len(l.items) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// LinesPerItem returns the current number of terminal lines per logical item.
func (l *List[T]) LinesPerItem() int {
	if l.opts.LinesPerItem <= 0 {
		return 1
	}
	return l.opts.LinesPerItem
}

// SetLinesPerItem changes the number of terminal lines each logical item
// occupies and reclamps scroll. Values below 1 are treated as 1.
func (l *List[T]) SetLinesPerItem(n int) {
	if n < 1 {
		n = 1
	}
	l.opts.LinesPerItem = n
	l.adjustScroll()
}

func (l *List[T]) adjustScroll() {
	l.rescroll(l.opts.ScrollMargin)
}

// rescroll recomputes the scroll offset for the current cursor. A region takes
// its lines and trailing members out of the ordinary list. A cursor inside the
// region leaves the ordinary rows where they stand because the region scrolls
// itself.
func (l *List[T]) rescroll(margin int) {
	lay := l.regionLayout()
	height := (l.height - lay.lines) / l.LinesPerItem()
	ordinaryCount := len(l.items) - lay.count
	if l.cursor < ordinaryCount {
		l.scroll = adjustScroll(l.cursor, l.scroll, height, ordinaryCount, margin)
		if !l.opts.TopEdgeOnChrome && l.scroll > 0 {
			height = max((l.height-lay.lines-1)/l.LinesPerItem(), 0)
			l.scroll = adjustScroll(l.cursor, l.scroll, height, ordinaryCount, margin)
		}
		return
	}
	visible := min(max(height, 0), ordinaryCount)
	l.scroll = min(max(l.scroll, 0), max(ordinaryCount-visible, 0))
}
