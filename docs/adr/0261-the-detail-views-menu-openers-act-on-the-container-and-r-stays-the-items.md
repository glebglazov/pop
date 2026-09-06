---
status: accepted
relates: "extends [ADR-0236](0236-every-menu-opens-from-the-top-level-and-the-action-menu-becomes-the-run-menu.md)'s four top-level openers down one level into the detail view, keeps [ADR-0254](0254-a-selection-is-a-human-mark-that-outranks-a-preset-and-turns-verbs-plural.md) decision 5's exclusion of item-level bulk, and leaves [ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md)'s kind seams untouched"
---

# The detail view's menu openers act on the container, and `r` stays the item's

## Context

The **Work dashboard**'s row list and its **Task set detail view** are one level
apart and speak two different keyboards. An audit of the two `Update` handlers
(`dashboard/dashboard.go:1282` for the row list, `:2084` for the detail) put
numbers on it: of the fourteen keys the row list binds, six mean something else
one level down or nothing at all.

Three of the six are the same failure — **the detail view is still running the
design ADR-0236 retired.**

- **`y` copies outright.** The detail dispatches `y` and `p` straight through
  `dispatchItemKey` to copy-name and copy-path. That is precisely the flat-key
  shape decision 6 replaced with an opener at row level. `p` is worse: a direct
  key with no counterpart above it at all.
- **`s` is booby-trapped.** At row level `s` opens the **Status menu**. In the
  detail's list it does nothing, and one keypress deeper — inside the menu `r`
  opens — `s` is *skip*. A finger trained on the row list reaches for the status
  menu and writes a status.
- **`r` is the undifferentiated menu.** At row level `r` is what remained of the
  action menu *after* status, copy and mute were lifted out of it. At item level
  it is the pre-split everything-menu those three came out of.

Two more are plain absence: `/` search and `m` mute do not answer in the detail.
And one is a collision: `v` is the shell's **Work page** toggle above and the
**Artifact view** toggle below.

Underneath all of it sits the real gap. **The container is unreachable from its
own detail.** Apart from `ctrl+g`, there is no way to drain, verify, assist,
archive or mute the set whose name is in the header without dismissing the view
and finding its row again — even though that container is exactly what the
reader is looking at.

## Decision

**In a detail view, the four top-level menu openers act on the container the
detail is open over — with `r` as the single, reasoned exception.**

1. **`s`, `y` and `m` mean over the container what they mean over the row.**
   They open that container's **Status menu**, **Copy menu** and **Mute menu**,
   built from the same `Kind.StatusActions` / `Kind.CopyActions` / `Muter` the
   row list asks, rendered as the detail's own reserved block. A kind that
   offers nothing flashes, exactly as it does above.

2. **`r` stays the item's menu.** A Task-set task's `ItemActions` are *entirely*
   status writes and copies — complete, open, skip, copy-name, copy-path. Split
   them the way the container's list was split and `r` would open an empty menu
   on the one kind the view was built for. The item's verbs therefore stay whole
   behind `r`, and the container's Run menu stays one dismissal away.

3. **The flat copy keys retire.** `y` and `p` no longer copy outright, in the
   detail or in the **Document peek**. Copying a *container* is `y` and a menu
   entry; copying an *item* is `r` and a menu entry, where both already live.
   Copy-name is now a menu entry on every surface and a direct keypress on none.

4. **`I` comes down.** The row's own flat verb answers in the detail over the
   container, the same verb through the same dispatch.

5. **`/` narrows the item list**, with the row list's grammar — committed,
   case-insensitive, space-split into AND-ed substring terms — over an item's
   id, title and type. Its status stays out, for the reason the row list keeps
   status out of search: a reader narrowing by status wants a filter, and one
   question asked in two grammars invites two answers.

6. **`v` and `f` do not move.** `v` stays the artifact toggle: the detail cannot
   page, the letter reads as "view" in both places, and the hint line names it.
   `f` does not come down at all — a **Work view preset** selects containers, and
   a detail has exactly one.

7. **`tab` stays absent.** Item-level bulk was settled as out of scope and this
   decision does not reopen it.

No **Work kind** seam changes. Everything above is the surface routing keys it
already owns to the container it is already holding.

## Consequences

The keyboard an operator learns on the row list is the keyboard that answers in
the detail, for every key but one. The exception is not arbitrary and it is not
novel: `r` at row level opens the Run menu while `r` *inside* that menu means
unpark. A key means one thing per level, and that was already this surface's
rule — decision 2 applies it rather than bending it.

The residue is that `s` inside `r`'s item menu still means skip while `s` at
detail level opens the container's status menu. The layering rule covers it,
and the alternative — reordering a kind's item verbs to dodge a letter — would
put the surface's keyboard inside the kinds.

Rejected alternatives:

- **Item-only, cleaned up.** Split the item's verbs three ways and leave the
  container unreachable. Rejected on decision 2's own evidence: for a Task set
  it produces an empty `r`, and it fixes none of the gap.
- **Split by case** — lowercase for the item, uppercase for the container. This
  invents a second meaning for case on a surface where uppercase already means
  *hands off to a pane*. Two case conventions on one keyboard is worse than the
  one exception decision 2 takes.
- **Container's verbs on Enter from the header.** A second navigation gesture to
  learn, to reach menus that already have keys.
