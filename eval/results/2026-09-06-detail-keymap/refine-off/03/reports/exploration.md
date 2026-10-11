# Exploration report — 2026-09-06-detail-keymap

- Explored: 2026-10-10T15:56:01Z
- Work SHA: 801845fe685a
- Explorer: claude --model "opus"

## The detail view's keymap and search, as found at 801845fe

### Where things live
- `dashboard/dashboard.go` (4553 lines) holds the whole area: the model `QueueDashboard` (`:891`), the menus (`:252`–`:545`), `detailView` (`:646`), key routing `update` (`:1282`), the menu drivers (`:1707`–`:1941`), `dispatchVerb` (`:2004`), `updateDetailView` (`:2084`), the row search (`:2428`–`:2499`, `:2695`–`:2744`), `helpEntries` (`:3276`), `View` (`:3528`), `frameSpec` / `mainHint` / `emptySearchLine` (`:3605`, `:3717`, `:3755`), `detailFrame` / `detailHints` (`:3908`, `:3969`), the peek chrome (`:4142`) and the menu renderers (`:4390`–`:4498`).
- `dashboard/bulk_verbs.go`: `openSelectionMenu`, `openSelectionStatusMenu`, `openSelectionCopyMenu`, `openSelectionMuteMenu` (`:178`–`:251`), the plural branches of the openers.
- `dashboard/mute_menu.go`: `dashboardMuteMenu` (`:157`), `taskDeps` (`:177`), `newDashboardMuteMenu` (`:188`), `muteMenuClearable` (`:208`).
- `dashboard/kind_verbs.go`: `dashboardKindVerbMsg`, `applyKindVerb`, `reportVerbStatus`, `reportVerbError`. These send each verb's result to the surface that ran the verb.
- `dashboard/work_rows.go`: `workKinds` resolvers `actionsFor`, `statusActionsFor`, `copyActionsFor`, `itemActionsFor`, `artifactActionsFor`, `muterFor`, `kindFor`.
- `dashboard/page.go`: `dashboardPage.searchNoun` and `empty` (`:78`–`:142`).
- `tasks/setkind/verbs.go` and `artifacts.go`: the Task-set kind's keys. The row-level `I` is drain (`:84`). The status keys are `c o s x u` (`:124`–`:128`). The copy keys are `n y w` (`:146`–`:150`). The item keys are `c o s y p` (`:166`–`:176`). The artifact keys are `y p` (`artifacts.go:47`). The Map kind has `y` for "copy map folder" and `y` for copy-name (`wayfinder/workkind.go:281`, `:335`). The Routine kind has `p` and `y` (`routine/verbs.go:108`–`:131`).
- `docs/adr/0261-the-detail-views-menu-openers-act-on-the-container-and-r-stays-the-items.md` is the parent record. `docs/adr/0213-*` is the text-entry rule.

### Which seam owns what
- **Key priority:** `update` (`:1282`). Help comes first, then the bind, drain, abandon and bulkPrompt modals. After them, `m.detail != nil` → `updateDetailView` (`:1313`) runs *before* `m.menu != nil` → `updateMenu` (`:1316`), `m.filter` and `m.searchTyping`.
- **Detail keys:** `updateDetailView`. Its order is `m.itemMenu` → `updateItemMenu`, then `m.menu` → `updateMenu`, then the peek switch, then the list switch. The list switch has `s` / `y` / `m` → `openStatusMenuOver` / `openCopyMenuOver` / `openMuteMenuOver(m.detail.row)` and `I` → `runFlatVerb(m.detail.row)`. Its `/` arm opens the detail's own search (`detailView.searchTyping` / `searchInput` / `searchTerm`), and while it types `updateDetailView` routes to `updateDetailSearchTyping`. `y` and `p` copy nothing in the list or the peek, and `dispatchItemKey` / `dispatchArtifactKey` are gone.
- **Container menus:** `openStatusMenu` / `openCopyMenu` / `openMuteMenu`. Each one first checks `m.selection.Active()`, then reads `m.list.Selected()` and calls its `open…MenuOver(row)` sibling. That sibling either flashes `a %s has no status to write`, `has nothing to copy` or `cannot be muted` (each with `detailKindNoun`), or sets `m.menu = &dashboardMenu{row, status|copy|mute}`.
- **Menu driving:** `updateMenu`. `J`/`K` move `m.list`, the row list, only when no detail is open. Then it routes to `updateStatusMenu` / `updateCopyMenu` / `updateMuteMenu`. Each of these sets `m.menu = nil` on esc. Each invoke goes to `dispatchVerb(verb, m.menu.row)`, or to `dispatchBulkVerb` when the menu is `plural`.
- **Flat `I`:** `update` `case "I"`. It calls `refuseSingular`, then `runFlatVerb(row)`: `dashboardShortcutItem(m.kinds, row, "I")` → `dispatchVerb`. `refuseInterceptedVerb` (`bulk_verbs.go`) refuses nothing while a detail is open. For a Map, the fallback is `mapRow(row)` → `launchWayfinderSession(row, "")`.
- **Item menu:** `itemMenu` (`:612`) and `newItemMenu` / `newArtifactMenu` (`:621` / `:630`). `itemMenu.inPeek` chooses the frame. `dispatchItemVerb` (`:2368`) sends `VerbCopyName` to `detail.flash` or `peek.flash`.
- **Rendering:** `View` renders `viewDetail` whenever `m.detail != nil` and no bind, drain or abandon modal is open (those render through `viewWithModal`). It never reaches `viewWithMenu`. `detailFrame` fills `Block` from `m.itemMenu` when `!inPeek`, else from `m.menu` through `dashboardMenuLines`. `documentPeekChrome` fills it from `m.itemMenu` when `inPeek` (`:4150`). `dashboardMenuLines(menu, width, live)` (`:4419`) is the renderer for container menus, through `menuBlockLines`. `frameSpec` sets the menu hint `"j/k move · J/K rows · enter/letter run · esc close"` (`:3641`).
- **Feedback:** `detailFrame` gives its Frame `d.flash`, or `m.flash` when `d.flash` is empty, and `m.actionErr` as its `Warnings`. `documentPeekChrome` gives only `p.flash`. A container verb sets `m.flash` (for example `dispatchVerb`'s `dashboardHandoffPending` and `VerbCopyName`), or `reportVerbStatus`'s default arm (`kind_verbs.go`), or `m.actionErr`.
- **Help:** `helpEntries` checks `m.itemMenu`, then the `m.menu` arms (copy, mute, status, run; `menuRowsHelp` drops `J/K` under a detail), then `m.filter`, then the peek, then the detail. The help *title* in `View` checks the peek, then the detail with no `m.menu`, then `m.menu`.
- **Row search:** `updateSearchTyping` (`:2428`) has Enter, Esc and ctrl+c, and gives every other key to `ui.TextField.Update`. `activeQuery` (`:2456`), `applySearch` (`:2471`) and `viewRows` (`:2492`) narrow the rows with `filterDashboardRows` (`:2695`) and `matchesEveryTerm(fields, terms)` (`:2730`). `matchesEveryTerm` is a free function, and the field list belongs to `searchFields` (`:2712`).

### Invariants that hold
- A Selection is a row-list concept. `selection.Active()` gates the openers, `r` and `refuseSingular`. Item-level bulk does not exist (ADR-0261 decision 7).
- A key resolves only through the kind's advertised actions: `dashboardShortcutItem`, `dispatchItemKey` and `dispatchArtifactKey`. No verb is registered by key in the dashboard.
- One open overlay renders as one `Frame.Block` above the hint line, through `menuBlockLines` (ADR-0224 decision 4). `BodyHeight` shrinks the list by exactly the block height (`detailFrame` `:3938`).
- The poll path `dashboardRowsMsg` (`:1496`) calls `retainSelection` + `applySearch(activeQuery())`. It then calls `syncDetailView` for the container with the same `CursorKey`, and refreshes `m.menu.row` the same way (`:1537`). `detailView.sync` calls `d.list.ReplaceItems` with the items `filterDetailItems` keeps for `d.activeQuery()`, and `syncArtifacts` keeps the whole list in `d.allArtifacts` and gives `artifactList` the part `filterDetailArtifacts` keeps. Both lists use `Key` (`i.ID` / `a.Path`) to re-anchor the cursor.
- `detailView.itemByID` (`:805`) searches `d.row.Items`, not `d.list`. `artifactByPath` searches `d.allArtifacts`, and `hasArtifacts` counts it.
- The row search never reads status (`searchFields` comment `:2681`). Applying `""` clears the search, and opening `/` resets the buffer and calls `applySearch("")` (`:1341`).
- `work.Item` has `ID`, `Title`, `Type`, `Status` and `StatusLabel` (`work/kind.go:336`). `work.Artifact` has `Type`, `Name` and `Path` (`work/artifact.go:12`).
- `DashboardRow` is an alias of `work.Container` (`dashboard.go:61`–`:70`).

### The test shape
- All tests are in `package dashboard` and drive `m.update(tea.KeyPressMsg{Code, Text})`. `pressKeys` (`dashboard_routine_verbs_test.go:38`) also runs each returned `tea.Cmd`.
- Fixtures:
  - `genericDetailDashboard(kind)` + `genericDetailRow()` over the stub `itemVerbKind` (`dashboard_detail_generic_test.go:25`–`:80`). Its `StatusActions` and `CopyActions` return nil.
  - `detailCopyModel(setID, task)` builds `newTaskDetailView` (`dashboard_copy_test.go:242`). `m.copyFunc` captures the clipboard.
- The detail's container openers, `I`, refusals and hints are pinned in `dashboard_detail_keymap_test.go`. The `…ViaMenu` tests in `dashboard_copy_test.go` cover `r`.
- Menu rendering is in `dashboard_menu_block_test.go`. Kind menus are in `dashboard_kind_menu_test.go`. Selection is in `dashboard_selection_test.go`.
- Proof: `go build ./... && go vet ./dashboard/... && go test ./dashboard/...`.

### Read these in ranges
- `dashboard/dashboard.go`: `252–545` (menus), `606–822` (itemMenu, detailView), `1282–1470` (update keys), `1496–1545` (poll), `1707–1941` (menu drivers), `2004–2082`, `2084–2420` (detail keys and item dispatch), `2428–2499`, `2681–2744` (search), `3276–3526` (help), `3528–3580` (View), `3605–3757` (frameSpec, hints, empty line), `3892–3978`, `4142–4168`, `4390–4498`.
- `dashboard/dashboard_test.go` (5641 lines): open it only for a symbol found by `rg`.
