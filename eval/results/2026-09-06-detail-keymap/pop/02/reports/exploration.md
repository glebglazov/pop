# Exploration report — 2026-09-06-detail-keymap

- Explored: 2026-10-10T04:52:48Z
- Work SHA: 801845fe685a
- Explorer: claude --model "opus"

## Where things live

- `dashboard/dashboard.go` (4553 lines): the whole Work dashboard model `QueueDashboard`. It holds the row list, the menus, the detail view, the Document peek, search, help and render.
- `dashboard/bulk_verbs.go`: the plural (Selection) openers `openSelectionStatusMenu` / `openSelectionCopyMenu` / `openSelectionMuteMenu` (`:198`, `:226`, `:251`).
- `dashboard/kind_verbs.go`: `performKindVerb`, `muteRow` / `unmuteRow`, `applyKindVerb`, `reportVerbStatus`, `reportVerbError`. These carry a verb's outcome back to the surface it came from.
- `dashboard/work_rows.go`: the kind resolvers `statusActionsFor` (`:81`), `copyActionsFor` (`:92`), `itemActionsFor` (`:103`) and `muterFor` (`:161`).
- `work/kind.go:336`: `work.Item` has `ID`, `Title`, `Type`, `Status`, `StatusLabel`, `Blocked`, `BlockedBy` and `File`.
- `docs/adr/0261-the-detail-views-menu-openers-act-on-the-container-and-r-stays-the-items.md`: the parent ADR. It is accepted.

## Which seam owns what

- **Detail keys:** `updateDetailView` (`:2084`). It goes first to `updateItemMenu` when `m.itemMenu != nil`. Then it uses the peek switch (`:2089`–`:2152`), and then the list switch (`:2165`–`:2278`).
  - The flat copies are retired (task 01): `y`/`p` do nothing in the peek, and `dispatchItemKey` / `dispatchArtifactKey` are gone. The list's `case "s", "y", "m", "I"` calls the container openers over `m.detail.row`. When `m.menu` is open, `updateDetailView` sends the key to `updateMenu` before the peek switch.
  - The `r` cases are at `:2115` and `:2234`.
- **Key routing order:** `update` (`:1282`) checks `m.detail != nil` (`:1313`) before `m.menu != nil` (`:1316`) and before `m.searchTyping` (`:1324`). Thus a `m.menu` that is open over a detail never gets to `updateMenu` (`:1707`) unless `updateDetailView` sends the key there.
- **Container menu openers:** `openStatusMenu` (`:287`), `openCopyMenu` (`:335`) and `openMuteMenu` (`:360`).
  - Each one checks `m.selection.Active()` first, then reads `m.list.Selected()` and calls its `open*MenuOver(row)` sibling, which the detail calls directly with no Selection check. `I` is `runShortcutOver(row)`.
  - Each one flashes a refusal on `m.flash`: "a %s has no status to write", "a %s has nothing to copy" or "a %s cannot be muted".
  - Each one sets `m.menu = &dashboardMenu{row, status|copy|mute}`.
- **Menu keys:** `updateMenu`, then `updateStatusMenu`, `updateCopyMenu` or `updateMuteMenu`.
  - In these handlers, esc sets `m.menu = nil`.
  - `J`/`K` in `updateMenu` move `m.list`, the row list. They do nothing when `m.detail != nil`.
- **Verb dispatch:** the `invoke*MenuItem` functions call `dispatchVerb` (`:2004`) or `dispatchBulkVerb`.
- **`I` on the row list:** `update`'s `case "I"` (`:1430`). It calls `refuseSingular`, then `dashboardShortcutItem(m.kinds, row, "I")` (`:509`), then `dispatchVerb`. If the row is a Map and the kind offers no `I` item, it falls back to `launchWayfinderSession(row, "")`.
- **Where container feedback goes:**
  - `dispatchVerb` sets `m.flash`.
  - `reportVerbStatus` (`kind_verbs.go`) writes to `m.detail.flash` or `m.detail.peek.flash` only when the message carries an `item` or `artifact`. For a container verb it writes to `m.flash`.
  - `reportVerbError` sets `m.actionErr` for container verbs.
  - `detailFrame` renders `d.flash`, or `m.flash` when `d.flash` is empty, and `m.actionErr` as `Warnings` (task 01).
- **Rendering:**
  - `View` (`:3565`) returns `viewDetail` before it reads `m.menu`, unless a bind, drain or unbind modal is open (task 01).
  - `detailFrame` builds `Block` from `dashboardMenuLines(m.menu, …)` when a container menu is open, else from `itemMenuLines` (when `!itemMenu.inPeek`).
  - `frameSpec` (`:3605`) builds the row list's block with `dashboardMenuLines(m.menu, …)` (`:4419`) and the hint `"j/k move · J/K rows · enter/letter run · esc close"`.
  - The peek's chrome, hints and block come from `documentPeekChrome` (`:4141`).
- **Hints:**
  - `detailHints` names `r run ▸ · s status ▸ · y copy ▸ · m mute ▸` (task 01).
  - The peek hint names `r run ▸` and no copy keys (task 01).
  - `mainHint` (`:3717`) has the openers string `"r run ▸ · s status ▸ · y copy ▸ · m mute ▸ · "` and the typing line `"enter apply search · esc cancel · C-h help"`.
- **Help:**
  - `helpEntries` (`:3276`) checks the `m.menu` cases (`:3337`–`:3383`) before `m.detail.peek` (`:3407`) and `m.detail` (`:3427`). The detail arm lists `r`, `s`, `y`, `m` and the container's `I`; the peek arm lists no copy keys (task 01). The menu arms end with `menuNavigationHelp`, which leaves out `J/K` over a detail.
  - The help title chain in `View` (`:3533`) checks `m.menu` before `m.detail` (task 01).
- **Row-list search:**
  - Fields on `QueueDashboard` (`:935`–`:937`): `searchTyping`, `searchInput`, `searchTerm`.
  - `/` at `:1341` starts from an empty buffer and calls `applySearch("")`.
  - `updateSearchTyping` (`:2428`), `activeQuery` (`:2456`), `applySearch` (`:2471`) and `viewRows` (`:2492`).
  - Grammar: `filterByTerms` (shared with the detail search, task 02) uses `strings.Fields(strings.ToLower(query))` and `matchesEveryTerm`. `filterDashboardRows` calls it with `searchFields`.
  - Empty state: `emptySearchLine` (`:3755`), called from `mainBody`.
- **Detail data:** `detailView` (`:680`) has `row`, `list`, `artifactList`, `artifacts`, `cols`, `peek` and `flash`, plus `allArtifacts` and its own `searchTyping` / `searchInput` / `searchTerm` (task 02).
  - On each poll, the `dashboardRowsMsg` handler (`:1526`) calls `syncDetailView` (`:718`), which calls `d.sync`, which calls `d.applySearch` (both lists, narrowed by the detail's term). It also calls `syncArtifacts`, which stores `allArtifacts` and calls `d.applySearch`. The same handler updates `m.menu.row` by `CursorKey` (`:1537`).
  - `itemByID` reads `d.row.Items`. `artifactByPath` and `hasArtifacts` read `d.allArtifacts` (task 02).

## Invariants that hold

- `DashboardRow = work.Container` (`dashboard.go:62`).
- A kind that offers nothing gets a flash and not an empty menu (the openers at `:287`–`:375`).
- `ViewToggleAllowed` (`:1163`) returns false while `m.detail`, `m.menu` or `m.searchTyping` is set. `dashboardshell/shell.go:244` reads it.
- A text entry mode reserves only Enter, Esc and ctrl+c (ADR-0213, `updateSearchTyping`).
- Item and artifact feedback stays on the surface that called the verb (`reportVerbStatus`, and `dispatchItemVerb` at `:2370`). `dashboard_artifact_test.go:165` tests this.
- When the last artifact goes, `syncArtifacts` puts the detail back to the item list.
- `ReplaceItems` re-anchors the cursor by key: `Item.ID`, and `Artifact.Path` (`newDetailView`, `:684`).
- The search can only narrow the rows. It reads `m.allRows`, and Selection rows are exempt from it (`viewRows`).

## The test shape

- Tests are internal (`package dashboard`). They drive `m.update(tea.KeyPressMsg{Code, Text})` directly, or through `pressKeys` (`dashboard_routine_verbs_test.go:38`), which also runs the commands that come back. Then they read model fields (`m.menu`, `m.itemMenu`, `detail.flash.Text()`) or the `View()` text.
- Fixtures:
  - `genericDetailDashboard` / `genericDetailRow` and the fake kind `itemVerbKind` (`dashboard_detail_generic_test.go:32`–`:84`). That kind's `StatusActions` and `CopyActions` return nil.
  - `detailCopyModel` (`dashboard_copy_test.go:242`), built on `newTaskDetailView` (`testhelpers_test.go:29`).
  - `artifactDetailDashboard` (`dashboard_artifact_test.go:72`).
  - `countingKind` (`dashboard_kind_menu_test.go`).
  - `blockLines` / `menuRuleLine` (`dashboard_menu_block_test.go`).
- These tests asserted the flat `y`/`p` behaviour; task 01 rewrote them to assert the menus instead:
  - `TestQueueDashboardCopyDetailTask`, `…CopyDetailTaskPath`, `…CopyPeekTask`, `…CopyPeekTaskPath`, `…CopyMapDetailTicket` and `…CopyMapPeekTicket` (`dashboard_copy_test.go:254`–`:461`).
  - The `row y` / `row p` checks in `dashboard_artifact_test.go:200`–`:245`.
- Proof: `go build ./... && go vet ./dashboard/... && go test ./dashboard/...`.

## Read these in ranges

- `dashboard/dashboard.go`:
  - `252–375`: menus and openers
  - `388–420`: `dashboardMenu`
  - `606–830`: `itemMenu`, `detailView`, sync
  - `925–940`: search fields
  - `1282–1345` and `1388–1470`: `update` routing and row keys
  - `1520–1545`: poll sync
  - `1707–1830`: `updateMenu` and its submenus
  - `2004–2085`: `dispatchVerb`
  - `2084–2400`: detail keys, item dispatch
  - `2428–2510` and `2695–2745`: search
  - `3300–3470`: `helpEntries` detail and menu arms
  - `3533–3590`: `View`
  - `3605–3770`: `frameSpec`, `mainHint`, empty search
  - `3892–3980`: `detailFrame`, `detailHints`
  - `4115–4200`: item menu and peek chrome
  - `4401–4450`: menu blocks
- `dashboard/dashboard_test.go` (5641 lines): search it by test name and do not read it all. Detail item-verb tests are near `:3640`–`:3995`.
