# Exploration report — 2026-09-06-detail-keymap

- Explored: 2026-10-10T15:19:46Z
- Work SHA: 801845fe685a
- Explorer: claude --model "opus"

## Where things live

- `dashboard/dashboard.go` (4553 lines): the whole Work dashboard model, `QueueDashboard`. It holds the key routing (`update`, `updateDetailView`, `updateMenu`), the menu openers, the detail view (`detailView`), the row-list search, rendering (`frameSpec`, `detailFrame`, `menuBlockLines`), hints (`mainHint`, `detailHints`) and help (`helpEntries`, with help titles in `View`).
- `dashboard/bulk_verbs.go`: the Selection (plural) side. This file holds `openSelectionStatusMenu`/`openSelectionCopyMenu`/`openSelectionMuteMenu` (:198/:226/:251), `refuseInterceptedVerb` and `singularRefusal`.
- `dashboard/kind_verbs.go`: runs a verb and reports its outcome. This file holds `performKind`, `muteRow`/`unmuteRow`, `applyKindVerb`, `reportVerbStatus` (:175) and `reportVerbError` (:192).
- `dashboard/mute_menu.go`: `newDashboardMuteMenu`, `muteMenuClearable`, `muteMenuFooter` and `taskDeps`.
- `dashboard/work_rows.go`: `workKinds` lookups. These are `statusActionsFor`, `copyActionsFor`, `muterFor`, `actionsFor`, `itemActionsFor` and `artifactActionsFor`.
- `tasks/setkind/verbs.go:162`: `Kind.ItemActions`. Here a task binds `y` to copy-name and `p` to copy-path. `routine/verbs.go:108,129` binds `p` to copy report path.
- `work/kind.go:336`: `work.Item` has the fields `ID`, `Title`, `Type`, `Status`, `StatusLabel` and `File`.
- `work/artifact.go:12`: `work.Artifact` has the fields `Type`, `Name`, `Path` and `At`.
- `docs/adr/0261-*`: the decision record for this set.

## Which seam owns what

- **Key routing order.** `update` (:1282) checks these in sequence: help, `bind`, `drainPick`, `abandon`, `bulkPrompt`, **`detail` (:1313)**, `menu` (:1316), `filter`, `searchTyping`. When the detail is open, every key goes to `updateDetailView` and never reaches `updateMenu`.
- **`updateDetailView`** (:2084):
  - It routes to `updateItemMenu` first when `m.itemMenu != nil`.
  - Next it routes to `updateMenu` when `m.menu != nil` (a container menu opened from the detail; J/K are inert there).
  - Then it handles the peek branch. This branch has `r`; `y`/`p` are inert in it.
  - Last comes the list switch. The list's `r` opens the item menu; `s`/`y`/`m` call `openStatusMenuOver`/`openCopyMenuOver`/`openMuteMenuOver(m.detail.row)` and `I` calls `runFlatI(m.detail.row)`. `dispatchItemKey`/`dispatchArtifactKey` are retired.
- **Menu openers.**
  - `openStatusMenu` (:287), `openCopyMenu` (:335) and `openMuteMenu` (:360) each branch to the `openSelection*` sibling first when `m.selection.Active()`. Each then reads `m.list.Selected()`.
  - When the kind offers no entries, each sets **`m.flash`** with a fixed refusal: "a %s has no status to write", "…has nothing to copy" or "…cannot be muted".
  - Otherwise each sets `m.menu = &dashboardMenu{row:…, status|copy|mute:…}`.
- **Menus.**
  - `dashboardMenu` (:388) holds `row`, `list`, `status`, `copy`, `mute`, `plural` and `targets`.
  - `updateMenu` (:1707) handles `J`/`K`, which move **`m.list`**, the row list. It then hands off to `updateStatusMenu`, `updateCopyMenu` or `updateMuteMenu`.
  - The invokers (`invokeStatusMenuItem` :1922, `invokeCopyMenuItem` :1814, `invokeMuteMenuItem` :1867) dispatch over `m.menu.row` through `dispatchVerb`, `muteRow` or `unmuteRow`.
- **Verb dispatch.** `dispatchVerb` (:2004) owns the Task-set modals: bind, unbind, the drain picker and launches. Its flashes go to `m.flash` and its errors to `m.actionErr`. Other verbs go to `performKindVerb`.
- **Where outcomes report.** `reportVerbStatus` sends a container verb's status (no item, no artifact) to `m.flash`. `reportVerbError` sends a container verb's error to `m.actionErr`.
- **Flat `I`.** `case "I"` is at :1430. It runs `refuseSingular`, then `dashboardShortcutItem(m.kinds, row, "I")` (:509), then `dispatchVerb`. A Map row has its own fallback to `launchWayfinderSession(row, "")`.
- **Detail rendering.**
  - `View` renders `viewDetail` whenever `m.detail != nil` and no bind, drain-picker or abandon modal is open, before it looks at any menu.
  - `detailFrame` builds a `ui.Frame` whose `Block` is `itemMenuLines(...)` (when `!m.itemMenu.inPeek`) or `dashboardMenuLines(m.menu, …)`, with the hint "j/k move · enter/letter run · esc close".
  - Its `Flash` is `d.flash`, or `m.flash` when `d.flash` is empty; its `Warnings` carry `m.actionErr`.
  - The row list's `frameSpec` (:3605) gets its menu block from `dashboardMenuLines(m.menu, …)` (:4419).
  - `menuBlockLines` (:4401) is the one renderer that every menu uses.
- **Row-list search.**
  - The fields are `searchTyping`, `searchInput` (`ui.TextField`) and `searchTerm` (:935–937).
  - `/` (:1341) starts an empty buffer and calls `applySearch("")`.
  - `updateSearchTyping` (:2428) reserves enter, esc and ctrl+c. Every other key goes to `searchInput.Update`, followed by a live `applySearch`.
  - `activeQuery` (:2456) and `applySearch` (:2471) re-anchor the cursor by key.
  - The grammar lives in `narrowBySearch` (generic over the entry type, called by `filterDashboardRows` and by the detail) and `matchesEveryTerm`: `strings.Fields(strings.ToLower(q))`, every term a substring of some field. `searchFields` is row-specific.
  - The empty-result line is `emptySearchLine` (:3755). `mainBody` (:3762) uses it.
  - In `mainHint` (:3717), the typing-phase hint is "enter apply search · esc cancel · C-h help".
- **Detail state.**
  - `detailView` (:646) holds `row`, `list *ui.List[work.Item]`, `artifactList`, `allArtifacts`, `artifacts`, `cols`, `peek`, `flash` and its own search fields `searchTyping`/`searchInput`/`searchTerm`.
  - `sync` and `syncArtifacts` narrow `row.Items` / `allArtifacts` by the detail's `activeQuery` before `ReplaceItems`; `applySearch` does the same on a keypress.
  - `syncDetailView` (:718) is called from the `dashboardRowsMsg` arm (:1526–1535), which matches on `CursorKey`.
  - That same arm re-points `m.menu.row` by `CursorKey` (:1537).

## Invariants that hold

- `r` in the detail list and in the peek opens `itemMenu` from `itemActionsFor`/`artifactActionsFor`. For items it first passes the actions through `enrichItemActions`. `inPeek` decides which flash a result lands on (`reportVerbStatus`, `dispatchItemVerb` :2368).
- Every menu renders through `menuBlockLines` as a `Frame.Block`, and `BodyHeight` shrinks the list by the block height (`detailFrame`, `frameSpec`).
- When a kind offers nothing, the opener flashes and does not open an empty menu (`openStatusMenu`/`openCopyMenu`/`openMuteMenu`).
- A menu opened over a Selection carries `plural` and `targets`. The invokers branch on `m.menu.plural` to `dispatchBulkVerb`/`openBulkPrompt`.
- Esc in each container menu sets `m.menu = nil` (`updateStatusMenu` :1761, `updateCopyMenu` :1791, `updateMuteMenu` :1843).
- Each flash belongs to one surface: `flashes()` (:1271) runs the timers for `m.flash`, `m.detail.flash` and `m.detail.peek.flash`.
- `ReplaceItems` re-anchors each list's cursor by its `Key` func: `i.ID` for items, `a.Path` for artifacts (`newDetailView` :684).
- Search typing reserves exactly enter, esc and ctrl+c (`updateSearchTyping`). The search fields never include a row's status (`searchFields`).
- `pendingG` handles `gg` in the peek, in the list and on the row list. Every other key resets it.

## The test shape

- The tests live in package `dashboard`, under `dashboard/*_test.go`.
- They drive the model with `m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})`. A returned `cmd` is run and its result is fed back through `update`. `pressKeys` (`dashboard_routine_verbs_test.go:38`) and `openPage` give a whole-page path.
- `testhelpers_test.go` gives `detailRowWithTasks`, `newTaskDetailView` and `testKinds()`, which are the real adapters.
- `dashboard_copy_test.go`:
  - Its `TestQueueDashboardCopyDetailTask` (:254), `…DetailTaskPath` (:308), `…PeekTask` (:347), `…PeekTaskPath` (:396), `…MapDetailTicket` (:419) and `…MapPeekTicket` (:441) assert today's flat `y`/`p` copies through `m.copyFunc` and `detail.flash`.
  - The `…ViaMenu` variants (:279, :373) assert the `r` route.
- `dashboard_detail_generic_test.go`: item menu, block position and rebuild (`TestDetailFollowsTheRebuiltContainer` :343).
- `dashboard_menu_block_test.go`: block position and `TestMenuRuleNamesItsTarget` (:243).
- `dashboard_kind_menu_test.go`: menus built from the kind.
- `dashboard_flat_i_test.go`: `TestFlatIDispatchesPerKind`, using `verbSpyKind`.
- `dashboard_selection_test.go`: Selection behaviour.
- `dashboard_artifact_test.go`: the artifact view and artifact copy (:165).
- To prove a change: `go build ./... && go vet ./dashboard/... && go test ./dashboard/...`.

## Read these in ranges

`dashboard/dashboard.go`:

| Lines | What is there |
| --- | --- |
| 252–425 | openers, `dashboardMenu` |
| 505–545 | shortcut, Run menu |
| 606–822 | `itemMenu`, `detailView`, `sync` |
| 891–985 | model fields |
| 1271–1470 | `flashes`, `update` key switch |
| 1496–1545 | rows poll |
| 1700–1941 | container menus |
| 2004–2082 | `dispatchVerb` |
| 2084–2420 | detail keys, item dispatch |
| 2422–2500 | search |
| 2695–2745 | grammar |
| 3324–3460 | help arms |
| 3528–3560 | help titles |
| 3605–3660 | `frameSpec` |
| 3717–3770 | hints, empty search |
| 3892–3982 | `detailFrame`, `detailHints` |
| 4115–4140 | `itemMenuLines` |
| 4390–4500 | `menuBlockLines` and its siblings |

`dashboard/bulk_verbs.go`: lines 147–275.
