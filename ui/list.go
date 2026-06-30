package ui

import "strings"

// Anchor controls where list rows sit within the viewport when fewer items
// than the body height are visible.
type Anchor int

const (
	// AnchorTop pins rows to the top of the viewport (empty lines below).
	AnchorTop Anchor = iota
	// AnchorBottom pins rows to the bottom (fzf-style blank lines above).
	AnchorBottom
)

// RowState is passed to the Cell renderer for each visible row.
type RowState struct {
	Selected   bool
	QuickLabel string
	Width      int
}

// Opts configures a List's identity, rendering, and navigation behavior.
type Opts[T any] struct {
	Key          func(T) string           // stable identity for restore
	Cell         func(T, RowState) string // row content within RowState.Width
	Wrap         bool                     // up-at-top wraps to bottom
	Anchor       Anchor                   // Top | Bottom (fzf-style)
	ScrollMargin int                      // lines kept above cursor (quick-access reserves ~9)
	QuickLabel   func(dist int) string    // optional; nil = no quick-access column
}

// List is a passive, generic scrolling-list viewport. It owns cursor, scroll,
// height, navigation, identity-preserving reload, and per-row drawing. Models
// drive it by calling methods; it never sees tea.Msg.
type List[T any] struct {
	items  []T
	cursor int
	scroll int
	height int
	opts   Opts[T]
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

// HalfPageUp moves the cursor up by one page (body height).
func (l *List[T]) HalfPageUp() {
	if len(l.items) == 0 {
		return
	}
	page := l.height
	if page < 1 {
		page = 1
	}
	l.cursor -= page
	if l.cursor < 0 {
		l.cursor = 0
	}
	l.adjustScroll()
}

// HalfPageDown moves the cursor down by one page (body height).
func (l *List[T]) HalfPageDown() {
	if len(l.items) == 0 {
		return
	}
	page := l.height
	if page < 1 {
		page = 1
	}
	l.cursor += page
	if l.cursor >= len(l.items) {
		l.cursor = len(l.items) - 1
	}
	l.adjustScroll()
}

// Resize sets the viewport body height and reclamps scroll.
func (l *List[T]) Resize(bodyHeight int) {
	l.height = bodyHeight
	l.adjustScroll()
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

// VisibleRows returns exactly bodyHeight rendered lines. List owns the █
// indicator, quick-access prefix column, padding, and anchor blank lines.
func (l *List[T]) VisibleRows() []string {
	height := l.height
	if height <= 0 {
		return nil
	}

	lines := make([]string, height)
	itemCount := len(l.items)

	visible := height
	if visible > itemCount {
		visible = itemCount
	}

	start := l.scroll
	emptyBefore := 0
	emptyAfter := 0

	if l.opts.Anchor == AnchorBottom {
		emptyBefore = height - visible
	} else {
		emptyAfter = height - visible
	}

	quickAccess := l.opts.QuickLabel != nil
	prefixWidth := 2

	lineIdx := 0
	for i := 0; i < emptyBefore; i++ {
		lines[lineIdx] = ""
		lineIdx++
	}

	for i := 0; i < visible; i++ {
		itemIdx := start + i
		if itemIdx >= itemCount {
			break
		}
		item := l.items[itemIdx]
		selected := itemIdx == l.cursor

		quickLabel := ""
		if quickAccess && !selected {
			dist := l.cursor - itemIdx
			if dist >= 1 && dist <= 9 {
				quickLabel = l.opts.QuickLabel(dist)
			}
		}

		cell := ""
		if l.opts.Cell != nil {
			cell = l.opts.Cell(item, RowState{
				Selected:   selected,
				QuickLabel: quickLabel,
				Width:      0,
			})
		}

		lines[lineIdx] = l.renderPrefix(selected, quickLabel, prefixWidth) + cell
		lineIdx++
	}

	for i := 0; i < emptyAfter; i++ {
		if lineIdx < height {
			lines[lineIdx] = ""
			lineIdx++
		}
	}

	return lines
}

func (l *List[T]) renderPrefix(selected bool, quickLabel string, prefixWidth int) string {
	if selected {
		indicator := indicatorStyle.Render("█")
		if l.opts.QuickLabel != nil {
			return strings.Repeat(" ", prefixWidth-1) + indicator
		}
		return indicator + " "
	}
	if quickLabel != "" {
		return dimStyle.Render(quickLabel)
	}
	return strings.Repeat(" ", prefixWidth)
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

func (l *List[T]) adjustScroll() {
	l.scroll = adjustScroll(l.cursor, l.scroll, l.height, len(l.items), l.opts.ScrollMargin)
}
