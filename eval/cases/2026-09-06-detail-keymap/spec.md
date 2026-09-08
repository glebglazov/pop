## Parent

The decision record "The detail view's menu openers act on the container, and
`r` stays the item's" (`docs/adr/0261-*`), decisions 1, 3 and 4.

## What to build

Inside a Task set's or a Map's detail view, `s`, `y` and `m` open that
container's Status, Copy and Mute menus — the same menus, over the same
container, that the row one level up opens — and the flat `I` runs the
container's own first verb. The menus render as the detail's own reserved block,
list the same entries, dispatch the same verbs, and a container whose kind
offers nothing flashes the same refusal it flashes above.

The flat copy keys go away with it. In the detail list and inside a document
peek, `y` no longer copies a name outright and `p` no longer copies a path.
Copying an item is what the `r` menu is for, where both verbs already sit;
copying the container is `y` and a menu entry. `r` itself is untouched — it
still opens the cursored item's or artifact's own menu.

An operator who learns the keyboard on the row list finds the same keyboard one
level down, for every key but `r`.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `dashboard/dashboard.go:2084` `updateDetailView` is the detail's key switch.
  Its `case "y", "p":` arms (list at ~`:2262`, peek at ~`:2137`) are the flat
  copies to delete; `case "r":` stays as it is.
- The three openers are `openStatusMenu` (`:287`), `openCopyMenu` (`:335`) and
  `openMuteMenu` (`:360`). Each reads `m.list.Selected()` today, so each needs a
  row parameter (or a `*With` sibling) to be callable over `m.detail.row`. Each
  also branches on `m.selection.Active()` first — a Selection is a row-list
  concept and must not apply from inside a detail.
- `DashboardRow` is a type alias for `work.Container` (`dashboard.go:62`), so
  `m.detail.row` needs no conversion.
- Menus live on `m.menu` (`dashboardMenu`, `:380`-ish) and are driven by
  `updateMenu` (`:1707`), which `update` reaches *before* the detail check
  today — the ordering at `:1311` puts `m.detail != nil` ahead of `m.menu !=
  nil`, so `updateDetailView` has to route to `updateMenu` itself when a menu is
  open over a detail.
- Rendering: `detailFrame` (`:3908`) already takes a `Block`, used for
  `itemMenuLines`. The container menu renders into the same slot; see
  `frameSpec` (`:3605`) for how the row list builds its own.
- The `I` shortcut path is `case "I":` in `update` (~`:1432`), via
  `dashboardShortcutItem` and `dispatchVerb` (`:2004`).
- Hint line: `detailHints` (`:3968`). Row-list wording to mirror is `mainHint`
  (`:3717`) — `r run ▸ · s status ▸ · y copy ▸ · m mute ▸`.
- Help overlay: `helpEntries` (`:3276`) has arms per open menu; the detail's
  arms need the container menus added.
- Existing tests to extend rather than duplicate:
  `dashboard/dashboard_detail_generic_test.go`, `dashboard_copy_test.go`
  (asserts the flat `y`/`p` behaviour that is being retired — those assertions
  invert), `dashboard_kind_menu_test.go`, `dashboard_menu_block_test.go`.
- Proof: `go build ./... && go vet ./dashboard/... && go test ./dashboard/...`

## Type

AFK

## Acceptance criteria

- [x] `s` in a detail view opens the detail's container's Status menu, with the
      same entries `s` opens over that container's row.
- [x] `y` in a detail view opens that container's Copy menu; it no longer copies
      anything outright, in the list or in a peek.
- [x] `p` is inert in a detail view and in a peek.
- [x] `m` in a detail view opens that container's Mute menu.
- [x] `I` in a detail view runs the container's own flat verb, the same verb the
      row list runs.
- [x] A container whose kind offers no status, nothing to copy, or no mute
      flashes the same refusal from the detail that it flashes from the row list.
- [x] A menu opened from a detail renders as the detail's reserved block, and
      esc closes the menu and leaves the detail standing.
- [x] A Selection marked on the row list does not make a detail-opened menu
      plural.
- [x] `r` still opens the cursored item's or artifact's own menu, from the list
      and from inside a peek.
- [x] The detail's hint line and the help overlay name the four openers.
- [x] `go build ./... && go vet ./dashboard/... && go test ./dashboard/...` passes.

## Blocked by

- None - can start immediately.

## Parent

The decision record "The detail view's menu openers act on the container, and
`r` stays the item's" (`docs/adr/0261-*`), decision 5.

## What to build

`/` inside a detail view narrows its item list, with the grammar the row list's
search already uses: a committed, case-insensitive substring match, split on
spaces into terms that are AND-ed, so each term must match somewhere but
different terms may match different fields. Enter applies it and returns the
full keymap over the narrowed items; Esc abandons the edit and restores the term
in force; reopening `/` starts from an empty buffer, so applying an empty query
clears the search.

It reads an item's id, its title and its type. It does not read the item's
status: a reader narrowing by status wants a filter, and the row list keeps
status out of search for exactly that reason.

The term persists while the detail is open and across the polls that rebuild it,
and is shown where the reader can see what is hiding rows. When a query matches
nothing, the empty list says which term emptied it and how to clear it, rather
than reading as a container with no items.

The artifact list narrows the same way when it is the list on screen.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- The row list's half is the model: `applySearch` / `activeQuery` (~`:2456`),
  `updateSearchTyping` (`:2428`), `viewRows` (`:2492`) and the empty-state line
  `emptySearchLine` (`:3755`). Match its grammar rather than writing a second one.
- `searchTyping` / `searchInput` / `searchTerm` are row-list fields on
  `QueueDashboard`; the detail needs its own, on `detailView`
  (`dashboard.go:646`), so the two searches never share a buffer.
- `updateDetailView` (`:2084`) is the key switch to add `/` to, and it must
  route to the detail's own typing handler while typing — the row list's rule
  applies, that every key but Enter/Esc/ctrl+c is a character (see the ADR-0213
  note on `updateSearchTyping`).
- The item and artifact lists are `d.list` and `d.artifactList`, rebuilt by
  `syncDetailView` (`:718`) on every poll — the narrowing has to survive that
  rebuild, the way `retainSelection` + `applySearch` survive it at row level.
- Hint line `detailHints` (`:3968`) and `helpEntries` (`:3276`) both need the
  typing-phase wording; copy the row list's three-reserved-keys line.
- Proof: `go build ./... && go vet ./dashboard/... && go test ./dashboard/...`

## Type

AFK

## Acceptance criteria

- [x] `/` in a detail view opens a search over the item list, and Enter applies
      it and returns the full keymap over the narrowed items.
- [x] The query matches case-insensitively on an item's id, title and type, with
      space-separated terms AND-ed across those fields.
- [x] An item's status is not searched.
- [x] Esc while typing abandons the edit and restores the term in force;
      reopening `/` and applying an empty query clears the search.
- [x] While typing, every key but Enter, Esc and ctrl+c is a character — `j`,
      `k`, `v` and `r` type rather than act.
- [x] The term survives the poll that rebuilds the detail, and is visible to the
      reader while it is in force.
- [x] A query matching nothing shows an empty-state line naming the term and the
      way to clear it.
- [x] The artifact list narrows the same way when it is the list on screen.
- [x] The detail's search and the row list's search do not share a buffer or a
      term.
- [x] `go build ./... && go vet ./dashboard/... && go test ./dashboard/...` passes.

## Blocked by

- 01-container-openers-in-the-detail
