---
status: accepted
relates: "supersedes decision 3 of [ADR-0254](0254-a-selection-is-a-human-mark-that-outranks-a-preset-and-turns-verbs-plural.md) on where the Selection area sits, restores [ADR-0209](0209-an-attributed-pane-pins-its-rows-to-the-top-and-says-nothing-else.md)'s Pane pin to an uncontested head, and moves the action menu onto the reserved-block mechanism [ADR-0197](0197-work-view-presets-replace-the-inclusion-toggles.md) decision 9 built for the preset menu"
---

# The Selection area sits at the foot, and every menu is bottom chrome

Marked rows move to the **foot** of the list under a dividing rule rather than lifting to
its head. Every **Action menu** — singular, plural, and the detail view's — renders as
bottom chrome through **Frame**'s reserved block at one fixed screen position. `A`, the
**Pinned action menu**, is removed. **List** gains a **Scroll edge**: a count wherever a
boundary hides rows.

## Context

ADR-0254 put the **Selection area** at the head because that is where a reserved region
had to go — `ui.List` supports exactly one region and only at the head (`ui/list.go`
`regionLayout`). The placement was inherited from the mechanism, not chosen.

In use it reads badly on both dashboards. Marking a row displaces the list the human is
working in: every ordinary row shifts down, and the rows the eye was tracking move
by one for each `tab`. The information the human wants after marking — *what did I mark,
and what can I do to it* — arrives at the top of the screen while their attention is in
the middle of it, and the ordinary list, which is the thing they are still choosing from,
is pushed away from the header it is read under.

The action menu had the matching problem from the other direction. The singular menu is
spliced into the table under the cursored row and flips above it when it will not fit
(`dashboard/dashboard.go` `renderDashboardTableWithMenu`, `dashboardMenuPlaceBelow`), so
its position is a function of where the cursor is and how much room is left — it moves
around the screen as the cursor does. The plural menu is pinned to the region's foot, so
there are already two placement rules for one object.

Meanwhile the **Work view preset** menu (`f`) had solved all of this: ADR-0197 decision 9
renders it as a Frame `Block`, a region reserved between the footnote and the hint line, so
the table body shrinks by exactly its height and the list cannot render past the pane.
Fixed position, exact budget, and Frame clips an over-tall block with its own indicator.

## Decision

**1. The Selection area moves to the foot of the list, and `Region` means a foot region.**
Order top to bottom: **Pane pin** block, ordinary rows, the `N selected` rule, the marked
rows. The region parks at the bottom of the viewport whatever the list's length rather than
hugging the last ordinary row — a region whose position depends on how many rows happen to
exist is not reserved, and the point of the move is that the divider and the menu are at the
same screen row every time. `ui.List.Region` is redefined as a foot region with no placement
field: both callers are the Selection, so a head/foot option would ship with one value used,
and an unused option in shared infrastructure is untested behaviour that reads as supported.

Everything else about the area is unchanged — the exemption from presets and search, the
`height/3` cap, rows in list sort rather than marking order, moved not copied, the cursor
never landing there by default. Only the direction the region grows in is new.

**2. The Pane pin is uncontested at the head again.** ADR-0209 pins attributed rows first;
ADR-0254 then put the Selection area above them. With the area at the foot, the head of the
list means one thing — *which of these is me* — which is an arrival question, answered where
the eye already starts.

**3. `gg`/`G` keep their shape, mirrored.** Region-aware jumps stop at the divider from
either side: from the ordinary rows `G` reaches the last of them, a second press crosses into
the area; `gg` from inside the area reaches its first row, then the list's top. One press per
crossing, so the divider is never jumped by accident.

**4. Every action menu is a Frame `Block` at the foot.** The singular menu, the plural menu
and the detail view's item menus all render through the same reserved block the preset menu
uses, immediately above the hint line. This deletes `renderDashboardTableWithMenu`, its
two-line twin, `dashboardMenuPlaceBelow` and the flip. Opening a menu shrinks the list body
by exactly its height; the list re-clamps its scroll to keep the cursor visible, so rows
leave from the *top* and scroll back when the menu closes. The list stays live under an open
menu — `A` already established that, and a menu that froze the list would make the plural
case the one dead surface.

Where the actions are is a fact about the surface, not about which row is cursored or how
many are marked. Under the old rule a human learned three positions; under this one, none.

**5. A menu names its target on its own top rule.** `── actions · orders-api ──` for a
singular menu, `── actions · 6 selected ──` for a plural one, and the row cursor stays
painted. Adjacency was carrying that fact before, and a menu at the foot has no adjacency
to spend, so the caption pays for the move.

**6. `A` is removed.** The **Pinned action menu** existed to sweep one verb down many rows,
and a **Selection** now does that. The two overlap almost exactly: `A` sweeps **In-place
verbs**, and the plural grants cover mute, unmute, copy name and the whole status family —
complete, open/reopen, skip, archive, unarchive, abandon. What is genuinely lost is the
sweep of bind and unbind, which are in-place but singular-only because each resolves a
different checkout. That is accepted: binding several sets in a row is rare and each is a
separate decision anyway, which is why the verb is singular in the first place. `J`/`K`
re-filtering and the pinned mode go with it, `a` does not inherit stickiness, and `A` is
left unbound.

The Selection is also the better of the two where they overlap. You can see what you marked
before the verb fires, and a wrong `tab` costs a `tab`, where a wrong sweep costs a
correction per row.

**7. `List` gains a Scroll edge.** A count at any boundary hiding rows — `↑ 4`, `↓ 3` — for
every consumer of `ui.List`, pickers included. It rides chrome that already exists: the Work
dashboard's table rule, the monitor's two-column header row, and the `N selected` divider,
which carries one count at each end because it is the boundary of two lists at once
(`↓ 4 ──── 6 selected ────  ↑ 2`). It spends a line of its own only where there is no rule to
ride — the closing `↓ 3` under the area when the cap hides members below it, drawn only in
that state.

This is new because it is newly needed. A reserved region and a bottom-anchored menu both
take lines out of the scrolling area, so rows now leave the viewport for reasons the human
did not initiate, and a silently scrolling list cannot distinguish those from rows that do
not exist.

**8. Both surfaces, as far as each has the parts.** The region and the scroll edges land on
the Work dashboard and the **Dashboard** alike. The monitor has no action menu — its plural
verbs are direct keys with an inline `y/N` on the hint line — so decisions 4 to 6 do not
reach it.

## Considered options

**Drop the region entirely and mark rows in place.** The literal reading of "a divider
between selected and unselected". Rejected: the region is what makes a mark exempt from
hiding (ADR-0254 decision 2), and without it a marked row can be hidden by a preset or
search, which resurrects the *selected but invisible* state and every count and phrasing
that decision deleted.

**Append marked rows to the flat list instead of reserving a foot region.** Cheaper — no
change to `ui.List`. Rejected for the same reason: rows that scroll away are rows that can
be off-screen targets.

**Pin the menu to the viewport bottom independently of the region.** Rejected as a second
rule for no gain: the region is already at the foot, so hanging the menu off it puts the
menu at the bottom anyway, and "the menu hangs under what it targets" is one fact instead
of two positions.

**Keep the region capped but let the menu be capped too.** Rejected. The cap exists so a
selection cannot consume the surface, and the region persists for the whole selection; a
menu lives from one keypress to the next verb and can be greedy because it is about to go
away.

**Remove targeting by cursored row, so every verb reads the Selection.** Considered when
`A`'s removal raised whether the cursor is still a target. Rejected: it makes marking
mandatory, taxing the commonest act on the dashboard — drain this one set — two keys
forever, and it strands every singular-only verb, which is most of the handoffs. The cursor
remains the target when nothing is marked, which is decision 5's singular caption.

## Consequences

- `ui.Selection`'s `SelectionOverflow` line is replaced by the Scroll edge grammar, so the
  region no longer has an overflow rendering of its own.
- The action menu and the preset menu now occupy the same screen rows and cannot both be
  open, which was already true and is now visible.
- The detail view's item menus move even though the detail view has no Selection; menu
  position is a property of the dashboard, not of the mode.
- `A` disappearing frees a capital key. It is deliberately left unbound rather than
  reassigned: the verb-case rule reserves capitals for mode keys, and there is one fewer
  mode now.
