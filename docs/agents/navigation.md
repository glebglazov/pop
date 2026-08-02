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
| `queue/` | `pop queue` supervisor **and** the Work dashboard TUI (`dashboard.go`) |
| `work/` | Work-dashboard data core — rows, derivation, snapshot (ADR-0143) |
| `work/ref/` | `WorkRef` + the closed Work-kind enum; a leaf `store` may import |
| `routine/` | Project routines: discovery, firing, per-checkout state |
| `integrate/` | Agent-CLI integration — install/remove/doctor per agent |
| `internal/tmux/` | All tmux knowledge; nothing else shells out to tmux (ADR-0142) |
| `store/` | `pop.db`, single connection, opened once via `tasks.Deps` (ADR-0140) |
| `monitor/` | Pane status daemon and state |
| `project/`, `wayfinder/` | Project picker; Maps — scan, `index.json` manifest, frontier, skill invocation |
| `config/` | `config.toml` load, validation, migration |
| `ui/`, `layout/`, `dashboardshell/` | lipgloss styles and shared render helpers |

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
