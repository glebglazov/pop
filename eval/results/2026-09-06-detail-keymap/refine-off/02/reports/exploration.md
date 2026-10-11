# Exploration report — 2026-09-06-detail-keymap

- Explored: 2026-10-10T05:28:46Z
- Work SHA: 801845fe685a
- Explorer: claude --model "opus"

## Code map for task set 2026-09-06-detail-keymap, at commit 801845fe

### Where things live
- `dashboard/dashboard.go` (4553 lines) holds all the code that these tasks touch: the openers, the menus, `update`, `updateDetailView`, the row-list search, both frames, the hints and the help overlay.
- `dashboard/work_rows.go` holds `workKinds.statusActionsFor` (:81), `copyActionsFor` (:92) and `muterFor` (:161). Each one asks the kind that owns the row.
- `dashboard/kind_verbs.go` holds `work.OutcomeDetail` (:124), which opens a detail from a kind verb.
- `work/kind.go:336` `work.Item` has the fields `ID`, `Title`, `Type`, `Status`, `StatusLabel`, `Blocked` and `File`. `work/artifact.go:12` `work.Artifact` has the fields `Type`, `Name`, `Path` and `At`.
- `docs/adr/0261-the-detail-views-menu-openers-act-on-the-container-and-r-stays-the-items.md` has decisions 1–7. Decision 6 keeps `v` and `f` as they are. Decision 7 keeps `tab` out of the detail.

### Which seam owns what
- **Key routing.** In `update` (:1282), `m.detail != nil` (:1313) comes before `m.menu != nil` (:1316) and before `m.searchTyping` (:1324). `updateDetailView` (:2084) first sends keys to `updateItemMenu` when `m.itemMenu != nil`, then to `updateMenu` when a container menu is open over the detail. Then it runs the peek switch, then the list switch. The flat copies (`case "y","p"`) and `dispatchItemKey` / `dispatchArtifactKey` are retired (task 01); the list switch has `s`/`y`/`m`/`I` over `m.detail.row`.
- **Openers.** `openStatusMenu` (:287), `openCopyMenu` (:335) and `openMuteMenu` (:360) each do three things (task 01 split the per-row half into `openStatusMenuOver` / `openCopyMenuOver` / `openMuteMenuOver(row)`, which the detail calls):
  - They branch to `openSelection*Menu` when `m.selection.Active()`.
  - They read `m.list.Selected()`.
  - When the kind offers nothing, they flash on **`m.flash`** with the texts "a %s has no status to write", "a %s has nothing to copy" and "a %s cannot be muted" (`detailKindNoun`, :4028).
  
  They set `m.menu = &dashboardMenu{row, status|copy|mute}`.
- **Menu driving.** `updateMenu` (:1707) handles `J`/`K` before anything else, and these keys move **`m.list`**, the row list. Then it hands off to `updateStatusMenu` (:1756), `updateCopyMenu` (:1786) or `updateMuteMenu` (:1838). Each of these sets `m.menu = nil` on esc. `invokeCopyMenuItem` (:1814) dispatches `dispatchBulkVerb` when `menu.plural`, and `dispatchVerb(item.Verb, m.menu.row)` when it is not.
- **Flat `I`.** This is `update` :1430–1452 (task 01 moved steps 2–4 into `runFlatVerb(row)`, which the detail calls too). It does these steps in order:
  1. `refuseSingular(selectionShortcutName("I"))`
  2. `dashboardShortcutItem(m.kinds, row, "I")` (:509)
  3. `dispatchVerb` (:2004)
  4. For a Map row with no `I` item, `launchWayfinderSession`
- **Container dispatch.** `dispatchVerb` writes flashes, for example `dashboardHandoffPending` and `copyRowName`, to `m.flash`. Item dispatch, `dispatchItemVerb` (:2366), writes to `m.detail.flash` or `m.detail.peek.flash`.
- **Detail render.** `View` (:3528) returns `viewDetail` (:3892) whenever `m.detail != nil`. It does this before it reads `m.menu`. `detailFrame` (:3908) sets `Flash: d.flash` and fills `Block` only from `itemMenuLines` when `!m.itemMenu.inPeek`. In that case it also sets the hints to "j/k move · enter/letter run · esc close". The peek frame is `documentPeekChrome` (:4142). Its hints are "y copy name", plus "p copy path" for an artifact, plus "r run". The row list builds its menu block in `frameSpec` (:3605) through `dashboardMenuLines(m.menu, m.width, m.liveCache())` (:4419), with the hints "j/k move · J/K rows · enter/letter run · esc close".
- **Hints and help.** `detailHints` (:3969) names "y copy name", and "p copy path" in the artifact list. `helpEntries` (:3276) has these arms:
  - `itemMenu`
  - one arm for each `m.menu` sub-menu (copy, mute, status, run)
  - peek (:3408), which lists `y` and `p`
  - detail (:3427), which lists `y`, and `p` for artifacts
  - `searchTyping`
  
  The help title order in `View` (:3533–3557) checks peek and detail before any menu.
- **Row-list search.** `/` (:1341) opens a new `ui.NewTextField()` and calls `applySearch("")`. `updateSearchTyping` (:2428) does three things:
  - On enter, it commits `searchTerm`.
  - On esc, it restores `searchTerm`.
  - On any other key, it calls `searchInput.Update` and narrows the rows live.
  
  `activeQuery` (:2456) returns the buffer while the operator types and the term at other times. `filterDashboardRows` (:2695) does `strings.Fields(strings.ToLower(query))` and then `matchesEveryTerm` (:2730), which checks every term against any field. `searchFields` (:2712) is specific to rows. `pageHeader` shows "search: <term>". `emptySearchLine` (:3755) and `mainBody` give the empty-state text. `frameSpec` sets `InputBox: m.searchInput.View()`. The fields are `QueueDashboard.searchTyping`, `searchInput` and `searchTerm` (:935–937). `detailView` (:646) has its own `searchTyping` / `searchInput` / `searchTerm` (task 02), and both levels narrow through the shared `narrowBySearch`.

### Invariants that hold
- The rows poll (`dashboardRowsMsg`, :1522–1543) runs `retainSelection` and `applySearch(activeQuery())`. After that it calls `syncDetailView` and refreshes `m.menu.row`. It finds both of them by `CursorKey` in **`m.snap.Containers`**, which is the list after the row-list search narrows it.
- `detailView.sync` (:731) calls `list.ReplaceItems(row.Items)` and re-measures `cols.idW` over all items. `syncArtifacts` (:740) clears `d.artifacts` when the list becomes empty. `ReplaceItems` keeps the cursor on the same item key (`Item.ID` / `Artifact.Path`).
- Only one menu or modal can be open at a time. `ViewToggleAllowed` (:1163) refuses `v` while `detail`, `menu`, `itemMenu` or `searchTyping` is set.
- `flashes()` (:1268) expires `m.flash`, `detail.flash` and `peek.flash`. A detail shows only `d.flash`.
- In the typing phase, only enter, esc and ctrl+c are reserved keys (ADR-0213, `updateSearchTyping`).
- `r` in the list and in the peek opens `newItemMenu` / `newArtifactMenu` from `itemActionsFor` / `artifactActionsFor` (:2115, :2234). The task-set item actions include `y` copy name and `p` copy path, as `dashboard_copy_test.go` shows with `menuHasItemKey`.

### The test shape
- The tests are in package `dashboard`. They build models with `newQueueDashboard(d, &config.Config{}, DashboardSnapshot{...})`, set `m.width` and `m.height`, and drive keys with `m.Update` / `m.update(tea.KeyPressMsg{Code:'y', Text:"y"})`. They give polls as `dashboardRowsMsg{snap: ...}` and check `View().Content`. They capture copies with `m.copyFunc`.
- `dashboard_detail_generic_test.go` has `itemVerbKind`, `genericDetailRow` and `genericDetailDashboard`. `itemVerbKind` returns nil for `StatusActions` and `CopyActions`. `TestDetailFollowsTheRebuiltContainer` covers the poll that rebuilds the detail.
- `dashboard_copy_test.go` has `detailCopyModel`. `TestQueueDashboardCopyDetailTask`, `…DetailTaskPath`, `…PeekTask` and `…PeekTaskPath` (:254–:418), plus `…CopyMapDetailTicket` and `…CopyMapPeekTicket` (:419, :441), check the flat `y`/`p` copies that task 01 removes.
- Other test files to extend: `dashboard_menu_block_test.go` (block position and rule target), `dashboard_kind_menu_test.go` and `dashboard_flat_i_test.go` (`TestFlatIDispatchesPerKind`). `dashboard_test.go` :3029–3580 holds `TestDashboardSearch_*` and `TestSearch*`, which cover the row-list grammar.
- To prove a change: `go build ./... && go vet ./dashboard/... && go test ./dashboard/...`

### Read these in ranges
- `dashboard/dashboard.go`: 280–425 (openers, `dashboardMenu`), 505–535, 612–820 (`itemMenu`, `detailView`), 925–937, 1282–1470, 1515–1545, 1700–1860, 2004–2380, 2422–2500, 2600–2740, 3276–3470, 3528–3590, 3605–3790, 3889–3980, 4136–4190, 4419–4460.
- `dashboard/dashboard_test.go`: 3029–3580.
