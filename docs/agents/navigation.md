# Navigating this repo

Written from an audit of unattended drain spend: the sessions that cost the most
were the ones that rediscovered the facts below by grepping. Read this instead.

## Verify in one call

```sh
go build ./... && go vet ./<changed-pkg>/... && go test ./<changed-pkg>/...
```

`make test` (`go test ./...`) is the whole-tree gate — worth one run before you
finish, not per edit. `go build ./...` alone catches the mistakes that a
per-package `go test` misses, so chain the three rather than laddering
build → vet → test → whole-tree across four calls. `make build` / `make install`
carry the version ldflags; plain `go build` is fine for checking compilation.

Live-tmux and live-agent checks (`make live-tmux-layout`, `make live-agent-smoke
AGENTS="…"`) are opt-in and not part of ordinary verification.

## Package map

| Package | Owns |
| --- | --- |
| `cmd/` | Cobra commands, one file per family; thin over the packages below |
| `tasks/` | Task sets: manifests, runner, attempt streams, prompts, spend. `authoring_guide.go` is the `pop tasks authoring-guide` text — layout, templates, manifest fields and the judgment rules, generated from the constants `validateManifest` reads (ADR-0183); `validateManifest` also owns the orphan-markdown check — every `.md` in a set folder but `spec.md` (and the retired `prd.md`) needs a manifest entry, or the set reads MALFORMED. `verified_status.go` is the one read-side resolution answering both a set's status and its **Verification mark** (`verify_mark.go`), which is what keeps a **Human completion** (`human_completed` in the manifest, written at the `transition.go` chokepoint) from being demoted by a verdict — ADR-0179. A generated prompt never rides in argv: `agent_prompt_spill.go` spills it to a file at the `runAgentAttempt` seam (`attempts.go`) for the length of one attempt, and `verify.go`'s `workDiffView` hands the **Verifier** a commit range plus a complete `git diff --stat` instead of the diff bodies. `review.go` is **Code review** (`pop tasks review`, ADR-0214): the same range and stat handed to a **Reviewer** that is told to open the files itself, the resolved `code-review` convention reaching it through the `ReviewConvention` seam (the `conventions` package sits *above* `tasks`, so `cmd` wires it), and the answer written as a **Review artifact** under `<set>/reviews/`, latest by timestamp. `review_phase.go` is the drain's own review step, the fourth directive of the `run_tasks.go` loop, sitting after `verify_phase.go` and before `terminal_switch.go` and gating nothing — the only directive it hands back besides falling through is the human's interrupt. Its **Review episode** (`review_episode.go` over the `review_episodes` store table) is keyed on the fingerprint of the set's done-AFK task ids, so a commit that only moves the work SHA does not re-review and a finished task — a Remediation task included — does; every review records one, the drain's and `pop tasks review`'s alike. `review_pointer.go` is how the document reaches a human: one `ReviewPointer` (path plus the commit the artifact's own header records) that the HITL sign-off gate's preamble and paging entry, `render.go`'s detail-view section and the Assist prompt all render — a pointer and a verb on every surface, never the body. Both roles walk their agent list through one shared `agent_fallback_walk.go`, which differs between them only in an `agentRole`, and both file Captured runs — phase `verify` and phase `review`, which `spend.go`, `stream.go` and `stream_tracer.go` each give a set-level row of its own |
| `tasks/binding/` | Binding a Task set to a checkout or worktree, plus the managed-worktree root: `binding.go` owns the root list (current + pre-cut) and `worktree_root_move.go` the gated read-path fold that relocates the pre-cut root, repoints every recorded `runtime_path` in one transaction and repairs each repository (ADR-0174 decision 8) |
| `tasks/setkind/` | The Task-set `work.Kind` adapter: loads containers per repo group, the ADR-0121 comparator, the set's verbs. One level down from `tasks` because it needs `tasks/binding` |
| `tasks/drain/` | Task-set drain control and the supervisor's dependency bag (`queue.go`: `Deps`, the project scan, the `Decision` a scan produces, the tmux spawn). Everything a drain does to a Task set is here — routing to a checkout (`worktree.go`), bind/unbind/abandon/fold, deferrals, the status snapshot (`status.go`) and the run view (`run_output.go`) both read surfaces render. It also holds the kind wiring (`kinds.go`: `WorkKinds` and the unmemoized `AdvanceKinds` the supervisor drives, `WithGitMemo` scoping the **Git fact memo** and `project.ShapeMemo` (the per-load repository-shape probe memo) to one load, `SetKindDeps`, `MapKindDeps`, `RoutinePageKinds`; `advance.go` composes the advance half onto the Task-set kind; `routines.go` projects the bag onto the Routine kind), because that wiring needs `tasks/setkind`, `wayfinder` and `routine`, all of which sit above `tasks` |
| `dashboard/` | The Work dashboard TUI (`dashboard.go`, paged by `page.go`: one model, page A = Task sets + Maps, page B = Routines, each page's kinds/columns/chrome in its `dashboardPage`; `dashboardshell/` switches them with `v`). Rows are `work.Container`s: cells, action menus and header phrases all come from the row's own kind through `work_rows.go`'s resolver, and verbs dispatch by verb id — the modal ones the dashboard owns have a case in `dispatchVerb`, everything else falls through to `Kind.Perform` and its outcome is carried out by `kind_verbs.go` (message/clipboard, refresh, detail, pane handoff). Over a Selection (`tab`) the plural half in `bulk_verbs.go` takes over: the menus list only the verbs every marked row offers *and* declares plural on `work.Action.Modes`, a write is confirmed with an inline `y/N` on the hint line, and the run is a loop over the same `Perform` (ADR-0215). The nested status submenu behind the shared `work.VerbStatus` opener is the same story: its items are `Kind.StatusActions` and they dispatch down that one path (ADR-0186), so no status vocabulary lives here. The `f` filter menu is a single-select numbered list of Work view presets (session-only on `drain.Deps.ViewPreset`; page B offers none). The detail view is generic too — kind-authored sections over one `work.Item` list, per-item menus from `Kind.ItemActions`, fed by the same periodic rebuild as the table. `status_render.go` prints the same rows as static plain text for `pop work status` |
| `supervisor/` | The daemon loop and nothing kind-specific: `Run` acquires the single-instance lock at `<data>/pop/work/supervisor.lock` (`supervisor_lock.go`, which also reads the pre-cut `pop/queue` path so a pre-rename daemon still blocks startup) and ticks reconcile → candidates → dispatch over `work.Advancer`s (`supervisor.go` sequences — the first two phases fan out per kind, dispatch is serial in kind precedence order and rules on checkout occupancy first via `occupancy.go`). `run_output.go` is the emit-on-change diff over the run view, `log.go` the journal `pop work log` prints. It imports `tasks/drain`; nothing imports it back |
| `work/` | The Work seam (ADR-0173): the `Kind` interface (including `StatusCell`, which returns tone-tagged cell segments, and `Columns`, the headers a page of that kind reads under), the plain `Container`/`Item`/`Action`/`Outcome`/`Section` structs (a container carries its detail sections and headline; an item its type, status label and absolute file; an outcome is a message, a refresh, a detail-view request, a handoff, or a caller-owned modal), the snapshot builder over a wired `[]Kind`; plus the second seam `Advancer` (`advance.go`) with `Candidate`/`Verdict`/`AdvanceEvent`, implemented only by advanceable kinds and obtained by type assertion. Imports no kind and no TUI — two guard tests. There is no row model beside `Container`: `Row`/`SetRef` are deleted and each page's cells (Task-set, Routine) are container fields |
| `work/ref/` | `WorkRef` + the closed Work-kind enum; a leaf `store` may import |
| `repogroup/` | Fork-free resolution of the repository groups every kind scans (markers, integration target, HEAD branch, ADR-0060) — below the kinds because it needs `tasks/binding` |
| `routine/` | Routines: discovery, schedules, firing, per-checkout Project routines — plus `workkind.go`, the Routine's `work.Kind` (one derivation behind every read surface, relevance tiers stamped at `Load`, unreadable Routines as `BROKEN` containers, detail sections for schedule/directory/pause/last report, ADR-0177) wrapping `advance.go`'s `work.Advancer`, where the daemon's whole relationship with routines lives (consent and drift/overlap verdicts on the pure read, their writes at dispatch). Every Routine verb is in `verbs.go` (`Actions`/`ItemActions`/`Perform`, ADR-0173) over the behaviour files beside it; the STATUS derivation is `status.go`. No TUI lives here any more — the Routine dashboard *is* the Work dashboard's page B |
| `integrate/` | Agent-CLI integration — install/remove/doctor per agent |
| `internal/deps/` | The process seams every package injects: filesystem, git (`git.go`), and the per-load **Git fact memo** (`memogit.go`: `MemoGit`, memoizing `rev-parse`/`worktree list` per cleaned path so a Work load forks git once per question) |
| `internal/fanout/` | `Map`: one pure read over many independent inputs at once, bounded at `GOMAXPROCS`, results in input order and the first error in input order. It is what makes a Work read surface's per-repository-group load cost the slowest group rather than the sum of them (ADR-0189); the store-side and write-side halves are hoisted out of it by the caller (`tasks.PrepareRefreshes`, `wayfinder.PrepareScans`) |
| `internal/tmux/` | All tmux knowledge; nothing else shells out to tmux (ADR-0142). `@pop_*` option semantics including Work-session typing (`@pop_work_kind`/`@pop_work_id`) live here; `WorkSessions` also reports each session's tmux start directory, which is how the project picker attributes a Map session to a project (ADR-0185) |
| `internal/tty/` | Terminal job control and nothing else touches `tcsetpgrp`: `foreground.go` (`ForegroundPgrp`/`SetForeground`/`ClaimForeground`/`GuardRead` — the **Terminal foreground handover**), the per-thread SIGTTIN/SIGTTOU masking behind them (`sigmask_darwin.go`/`sigmask_linux.go`), and `OpenPTY` so a test can own a real terminal |
| `store/` | `pop.db`, single connection, opened once via `tasks.Deps` (ADR-0140). Forward-only migration list in `store.go`; every Work container registers in `work_containers`, with Task-set-local registration in `task_set_registrations` beside it (`sets.go`) — the `sets` table is a tombstone nothing reads or writes (ADR-0174). `history.go` holds the recency rows (`history_entries`) the pickers and the monitor dashboard order by, plus the `history_folds` marker that makes the legacy-file fold once-only (ADR-0188) |
| `monitor/` | Pane status daemon and state |
| `history/` | Where the human has been: one row per path, its last-landing instant, and the recency sorts the pickers read. Rows live in `store` (`history_entries`), borrowed through `tasks.Deps`, so this package sits above `tasks`; `cmd/` and `dashboard/` import it — the dashboard's one chokepoint `handoffAfterLaunch` records every handoff verb's landing (ADR-0188) |
| `project/`, `wayfinder/` | Project picker; Maps — scan, `index.json` manifest, `pop map register`/archive against the Work registry, read-path folds (pre-manifest Maps, the `wayfinder/`→`maps/` rename, the retired archive side-file), two-severity manifest validation on the load path (`manifest.go`: errors render the Map BROKEN; unreferenced files under `adrs/`/`context/` are warnings carried on `Map.Warnings` through status, resolve, arrive, register and the dashboard detail pane, refusing nothing), frontier, `pop map next`/`fan-out`/`claim` over `store`'s `work_item_claims`, `pop map resolve`/`out-of-scope` writing answer + manifest + map.md's `pop:generated` regions under a per-Map file lock, `pop map spawned` appending the Map's `spawned_sets` lineage (`spawn.go`) and the live status those ids resolve to on every render (`spawned.go`, shared by `pop map status <map-id>` and the dashboard detail pane), `pop map arrive`/`abandon`/`open` writing the `active`/`arrived`/`abandoned` status line (`arrival.go`: `AbandonMap` and `ReopenMap` are the status halves the Map kind's own status verbs perform in-process — ADR-0186), the per-Map tmux session `pop-map-<id>` (`session.go`: create-or-attach at the Trunk, `@pop_work_*` stamp, one `@pop_ticket`-tagged pane per ticket in its single `map` window, spawned and claimed by `fanout.go` — shared with the Work dashboard's map row), `assist.go` (`pop map assist`: the `@pop_assist`-tagged Map-scoped pane in that same window — one per Map, reused, claiming nothing and resolving nothing, ungated by the frontier — ADR-0184), skill invocation, `authoring_guide.go` (the `pop map authoring-guide` text, generated from the manifest/parse constants so it cannot drift from what `register` enforces — ADR-0183), and the Map `work.Kind` adapter (`workkind.go`) |
| `config/` | `config.toml` load, validation, migration. `DeclaredTrunkPathsWith` (`trunk_paths.go`) is the fork-free trunk read — every stated trunk path as a set, for callers that hold the checkout and cannot afford `binding.ResolveTrunkPathWith`'s git fork; the project picker promotes a bare repo's trunk row with it (ADR-0185). Pop writes one config file, `config.override.toml` (`override_write.go` over `popwritten.go`); `runtime_fold.go` is the only code that still names the retired `config.runtime.toml`, folding it into that layer on the first load and renaming it aside (ADR-0212 decision 5) |
| `ui/`, `layout/` | lipgloss styles and shared render helpers |
| `conventions/` | Repo conventions: the closed kind enum, the four-layer stack (`stack.go` — `ResolveAll` does one repository's git work once for every kind), every rendering of one (`render.go`, including `StackPreview` for the Config dashboard's pane), the pop-written memory layer (`memory.go`) and the human's overlay (`overlay.go`), which is the layer an editing surface writes. Two consumers: `pop conventions` and the Config dashboard's `conventions.<kind>` rows |
| `confighost/` | The Config dashboard's write side over the real override layer and the Convention overlay, the adapter from resolved override views to the component's rows, and the **Config dashboard host** contract every embedding program owes it (ADR-0202 decision 11) — read its package doc before adding a host. `cmd`, `dashboardshell` and `ui.Picker` (`ui/picker_config.go`, for the project and worktree pickers) are the hosts today |
| `dashboardshell/` | The Work dashboard's entry layer: opens it on a page (`RunFromQueue` → A, `RunFromRoutine` → B) and gives one page the keyboard at a time. It also hosts the Config dashboard as a modal on `alt+c` (`config_modal.go`) — both the page toggle it suspends and the one config load it re-reads after a write are the shell's, not a page's |

Test doubles live next to their subject: `internal/tmux/tmuxtest`,
`store/storetest`, the recording tmux / advancer and spawn-repo fixtures the
drain, dashboard and supervisor tests share in `internal/queuetest`, and the fake
`CommandRunner` + git-repo template in `tasks/` (ADR-0144). Reach for those before writing a new fake.

## Finding a symbol

Use LSP go-to-definition, not a grep ladder. Symbols in this repo are mostly
package-private and clustered by feature, so a name-pattern grep returns either
everything or nothing; two empty greps for the same symbol means the shape is
wrong, not that the symbol is absent.

These files are large enough that reading one whole costs more context than the
rest of the task combined — read ranges, and read each once:

- `dashboard/dashboard.go` (~3.4k lines) and `dashboard/dashboard_test.go` (~4.8k) —
  dashboard rendering, status cells, sort tiers, dest kinds
- `tasks/run_tasks_test.go` (~3.4k) — the drain family's table tests
- `config/config.go` + `config/config_test.go` (~7.5k together)
- `integrate/integrate_test.go` (~3.3k)

## Agent-CLI wire facts are in the code

How each agent CLI is configured — kimi's `[[hooks]]` array-of-tables in
`~/.kimi-code/config.toml`, `KIMI_CODE_HOME` resolution, which hook event carries
"working", stream-json shapes — is documented in comments in `integrate/`
(`hooks_toml.go`, `hooks.go`) and in `docs/adr/0164-*`. Read those. Do not
`strings(1)` the agent binaries to re-derive it; one audited task spent 21 shell
calls and ~68k tokens doing that for facts already written down here.

## Domain language

`CONTEXT.md` is a 1100-line glossary — grep it for the term you need rather than
reading it whole. See `docs/agents/domain.md` for how it and `docs/adr/` fit
together.
