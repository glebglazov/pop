---
status: accepted
---

# The shared TUI list foundation is for selectable lists; pagers and overlay placement stay bespoke

## Context

`ui.Frame` (screen chrome + body-height budget) and `ui.List[T]` (a passive,
generic scrolling list that owns cursor, scroll, wrap, identity-preserving
reload, and per-row drawing) are pop's shared foundation for list-shaped TUIs.
The project picker, monitor dashboard, and multi-set selection already sit on
them; the **Queue dashboard** hand-rolled its own equivalents across six render
paths (cursor clamp ×7, wrap ×4, `gg`/`G` ×3, two reload-by-key routines, a
single-region footer budget), and its two main surfaces — the task-set table and
the detail task list — had **no scroll window at all**, so they overflowed a
short terminal.

We are porting the Queue dashboard onto `Frame` + `List`. This ADR records the
**boundary** of that foundation, because the boundary is the surprising part —
not the port itself.

## Decision

`Frame` + `List[T]` is the home for **selectable list views**: surfaces with a
cursor over discrete rows. The Queue dashboard's task-set table, detail task
list, bind picker, and drain picker move fully onto it (gaining a scroll window
as a side effect — the overflow is fixed, not preserved). The action-menu
overlays adopt `List` for their **item cursor only**.

Two things are deliberately **kept bespoke and out of `List`**:

- **The task text peek is a text pager, not a list.** It scrolls lines of a file
  with no selected item. `List[T]` always renders a cursor indicator on the
  selected row; a pager has none. It stays a hand-rolled scroller.
- **Overlay placement stays bespoke.** Deciding whether an action menu renders
  below the cursor row or flips above it (`dashboardMenuPlaceBelow`) is
  overlay-positioning math with no counterpart in `Frame`'s fixed region model,
  and `Frame` does not grow one.

To keep the Queue dashboard *fully* on `Frame` with zero hand-assembled chrome,
`Frame` gains one optional single-line `Status` region for transient action
feedback (the queue's `statusMsg`); its refresh `err` maps onto the existing
amber `Warnings` region. The quick-access (`⌥`/`^` digit) column stays off — the
Queue dashboard is action-key-driven, and `List` leaves the affordance available
for later.

## Considered options

- **Force the text peek onto `List[T]` too.** Rejected: it requires adding a
  no-cursor / no-selection mode to `List`, distorting a deep, narrow interface
  for every caller to swallow one surface that isn't a list. Keeping the pager
  separate keeps `List` deep.
- **Give `Frame` an overlay-placement concept.** Rejected: overlay flipping is
  specific to the menu surface; generalizing it into `Frame` would be a region
  for one caller with no shared meaning.

## Consequences

- A future architecture review that spots the hand-rolled peek scroller should
  read this ADR before re-suggesting a `List` port — the exclusion is deliberate.
- The list mechanics (clamp, wrap, `gg`/`G`, reload-by-key) are tested once in
  `ui/list_test.go`; the Queue dashboard's tests for that generic behavior are
  dropped, and its remaining tests assert against `List`'s public methods rather
  than private cursor fields. A regression guard covers the newly-fixed
  height-clamp behavior.
- Consistent with the house TUI style established in
  [ADR-0076](0076-worktree-picker-owns-interactive-creation.md): pop hand-rolls
  on raw bubbletea rather than reaching for heavier component libraries — the
  shared foundation is that house style, factored out.
