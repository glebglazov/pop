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
