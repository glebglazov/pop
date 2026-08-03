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
| `tasks/` | Task sets: manifests, runner, attempt streams, prompts, spend |
| `tasks/binding/` | Binding a Task set to a checkout or worktree |
| `tasks/setkind/` | The Task-set `work.Kind` adapter: loads containers per repo group, the ADR-0121 comparator, the set's verbs. One level down from `tasks` because it needs `tasks/binding` |
| `queue/` | `pop queue` supervisor **and** the Work dashboard TUI (`dashboard.go`, paged by `page.go`: one model, page A = Task sets + Maps, page B = Routines, each page's kinds/columns/chrome in its `dashboardPage`; `dashboardshell/` switches them with `v`). The supervisor tick is reconcile → candidates → dispatch over `work.Advancer`s (`advance.go` composes the advance half onto the Task-set kind, `routines.go` is now only the Routine advancer's wiring; `supervisor.go` sequences — the first two phases fan out per kind, dispatch is serial in kind precedence order and rules on checkout occupancy first via `occupancy.go`). Rows are `work.Container`s: cells, action menus and header phrases all come from the row's own kind through `work_rows.go`'s resolver, and verbs dispatch by verb id — the modal ones the dashboard owns have a case in `dispatchVerb`, everything else falls through to `Kind.Perform` and its outcome is carried out by `kind_verbs.go` (message/clipboard, refresh, detail, pane handoff). The detail view is generic too — kind-authored sections over one `work.Item` list, per-item menus from `Kind.ItemActions`, fed by the same periodic rebuild as the table |
| `work/` | The Work seam (ADR-0173): the `Kind` interface (including `StatusCell`, which returns tone-tagged cell segments, and `Columns`, the headers a page of that kind reads under), the plain `Container`/`Item`/`Action`/`Outcome`/`Section` structs (a container carries its detail sections and headline; an item its type, status label and absolute file; an outcome is a message, a refresh, a detail-view request, a handoff, or a caller-owned modal), the snapshot builder over a wired `[]Kind`; plus the second seam `Advancer` (`advance.go`) with `Candidate`/`Verdict`/`AdvanceEvent`, implemented only by advanceable kinds and obtained by type assertion. Imports no kind and no TUI — two guard tests. There is no row model beside `Container`: `Row`/`SetRef` are deleted and each page's cells (Task-set, Routine) are container fields |
| `work/ref/` | `WorkRef` + the closed Work-kind enum; a leaf `store` may import |
| `repogroup/` | Fork-free resolution of the repository groups every kind scans (markers, integration target, HEAD branch, ADR-0060) — below the kinds because it needs `tasks/binding` |
| `routine/` | Routines: discovery, schedules, firing, per-checkout Project routines — plus `workkind.go`, the Routine's `work.Kind` (one derivation behind every read surface, relevance tiers stamped at `Load`, unreadable Routines as `BROKEN` containers, detail sections for schedule/directory/pause/last report, ADR-0177) wrapping `advance.go`'s `work.Advancer`, where the daemon's whole relationship with routines lives (consent and drift/overlap verdicts on the pure read, their writes at dispatch). Every Routine verb is in `verbs.go` (`Actions`/`ItemActions`/`Perform`, ADR-0173) over the behaviour files beside it; the STATUS derivation is `status.go`. No TUI lives here any more — the Routine dashboard *is* the Work dashboard's page B |
| `integrate/` | Agent-CLI integration — install/remove/doctor per agent |
| `internal/tmux/` | All tmux knowledge; nothing else shells out to tmux (ADR-0142). `@pop_*` option semantics including Work-session typing (`@pop_work_kind`/`@pop_work_id`) live here |
| `store/` | `pop.db`, single connection, opened once via `tasks.Deps` (ADR-0140). Forward-only migration list in `store.go`; every Work container registers in `work_containers`, with Task-set-local registration in `task_set_registrations` beside it (`sets.go`) — the `sets` table is a tombstone nothing reads or writes (ADR-0174) |
| `monitor/` | Pane status daemon and state |
| `project/`, `wayfinder/` | Project picker; Maps — scan, `index.json` manifest, `pop map register`/archive against the Work registry, read-path folds (pre-manifest Maps, the `wayfinder/`→`maps/` rename, the retired archive side-file), frontier, `pop map next`/`claim` over `store`'s `work_item_claims`, `pop map resolve`/`out-of-scope` writing answer + manifest + map.md's `pop:generated` regions under a per-Map file lock, `pop map spawned` appending the Map's `spawned_sets` lineage (`spawn.go`) and the live status those ids resolve to on every render (`spawned.go`, shared by `pop map show` and the dashboard detail pane), `pop map arrive`/`open` writing the `active`/`arrived`/`abandoned` status line, the per-Map tmux session `pop-map-<id>` (`session.go`: create-or-attach at the Trunk, `@pop_work_*` stamp, one grilling window per ticket — shared with the Work dashboard's map row), skill invocation, and the Map `work.Kind` adapter (`workkind.go`) |
| `config/` | `config.toml` load, validation, migration |
| `ui/`, `layout/` | lipgloss styles and shared render helpers |
| `dashboardshell/` | The Work dashboard's entry layer: opens it on a page (`RunFromQueue` → A, `RunFromRoutine` → B) and gives one page the keyboard at a time |

Test doubles live next to their subject: `internal/tmux/tmuxtest`,
`store/storetest`, and the fake `CommandRunner` + git-repo template in `tasks/`
(ADR-0144). Reach for those before writing a new fake.

## Finding a symbol

Use LSP go-to-definition, not a grep ladder. Symbols in this repo are mostly
package-private and clustered by feature, so a name-pattern grep returns either
everything or nothing; two empty greps for the same symbol means the shape is
wrong, not that the symbol is absent.

These files are large enough that reading one whole costs more context than the
rest of the task combined — read ranges, and read each once:

- `queue/dashboard.go` (~3.7k lines) and `queue/dashboard_test.go` (~4.8k) —
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
