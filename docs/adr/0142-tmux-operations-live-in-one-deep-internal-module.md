---
status: accepted
---

# Tmux operations live in one deep internal module

## Context

`deps.Tmux` is an interface whose escape hatch is its real surface: six typed methods with almost no callers (`KillSession`: zero production callers, while two raw `Command("kill-session")` helpers duplicate it), and a generic `Command(args ...string)` used ~70 times across `cmd/`, `monitor/`, `routine/`, `queue/`, `session/`, and `history/`. Every call site re-encodes tmux CLI knowledge — subcommands, `#{...}` format strings, `@pop_*` option names — and four packages (`cmd/pane.go`, `routine/pane.go` + `refine_spawn.go`, `queue/queue.go` + `wayfinder_spawn.go`) hand-roll the same composite flow: find window by name → create if missing → split pane → tag with a `@pop_*` option → send-keys → retile. `ui/error.go` bypasses the interface entirely with a raw `os/exec` tmux call. Tests couple to exact argument vectors: ~130 arg-recording assertions across ~18 test files, each `CommandFunc` closure reimplementing tmux dispatch stringly-typed. One correction against the architecture review that prompted this: `ListSessions` is not vestigial — `history/history.go` uses it for activity sorting.

## Decision

- **One deep module, `internal/tmux/`, owns all tmux knowledge**: subcommand and format-string construction, output parsing into typed values (verbs return structs like session/pane info, never `"name\tactivity"` strings callers re-split), error mapping, and pop's own `@pop_*` option semantics (`topic`, `topic_kind`, `wb_window`, `pane`, `routine`, `set`). It is one module, not a generic tmux client with a pop layer on top — pop is the sole consumer, and the `@pop_*` options are this module's domain.
- **The escape hatch dies from the public surface.** No `Command(args...)` in the interface; any operation that needs tmux gets a named verb. The subprocess seam moves inside the module as an unexported runner with a real exec adapter and a recording fake — arg-array assertions survive only in the module's own tests, once per verb.
- **Composite domain verbs are first-class.** The ensure-tagged-pane-in-window flow becomes one verb; the four package-local copies die.
- **Full cut-over, no survivors.** The module absorbs tmux calls from every package, swallows the `session/` package (attach/switch, the only consistent typed-method consumer today), and routes the `ui/error.go` clipboard call. `deps.Tmux` and `deps.MockTmux` are deleted — no transitional alias.
- **One interface, one shared stateful fake** in an `internal/tmux/tmuxtest` package: in-memory sessions/windows/panes/options that tests arrange and assert on as state, with func-field overrides only for failure injection. Consumer tests stop asserting on argument arrays.
- **The Workbench layout engine stays out.** `cmd/template.go`'s merge/realize logic is Workbench domain knowledge; it calls the module's typed verbs. The module never learns pop config shapes.
- **Migration is per consumer package**: land `internal/tmux` + fake first, migrate one package per commit, delete `deps.Tmux`/`MockTmux` last. Old and new coexist only inside the sequence, never as a compatibility promise.

## Considered Options

- **Generic tmux client + thin pop-semantics layer** — rejected: speculative generality for a client with exactly one consumer; splits one concern across two layers.
- **Narrow per-consumer interfaces (segregation purism)** — rejected: forces N fakes; the single stateful fake only pays for itself against one interface.
- **Move the Workbench engine into the module** — rejected: inverts the dependency, making tmux plumbing aware of pop configuration.
- **Big-bang cut-over** — rejected on review size alone (~18 test files, 130 assertions); the endpoint is identical.

## Consequences

- Tmux CLI knowledge (arg order, format strings, option names) has one home; a tmux syntax change lands in one module and its tests.
- The fake grows with every new verb — accepted maintenance cost, paid once instead of per-test.
- Pane *status* (Working/Unread/Read) is untouched: it lives in the monitor state file, not in tmux options, and stays there.
