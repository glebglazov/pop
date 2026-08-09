---
status: accepted
relates: "moves the lists [ADR-0092](0092-task-config-parented-under-tasks-with-verb-named-sub-tables.md) parented under [tasks] onto the Work roots [ADR-0178](0178-pop-queue-hard-cuts-into-pop-work.md) established, and gives the attended sessions of [ADR-0187](0187-attended-agent-arguments-are-per-preset-defaults-under-an-agents-config-root.md) a list of their own"
---

# Agent lists are grouped by kind of work, and attended is one of the groups

## Context

Three ordered agent lists existed, in three roots chosen at three different
times: `[tasks.implement].agents`, `[tasks.verify].agents`, and
`[routines].agents` (which falls through to the implement list when unset). A
fourth kind of work had no list at all. Every **attended** session pop opens —
gate assistance, an **Assist session**, **Map assist**, map grilling, a
**Routine refinement session** — borrowed the *first entry* of the implement
list for its preset name, because that was the only list in reach.

That borrowing is not a small leak. A Map's grilling session is a long
interrogation that writes no code; an implement drain is an unattended coding
run whose list is ordered for quota fall-through. They want different agents,
and today choosing one chooses the other.

Two more defects sat on top. The lists are bare preset-spec strings
(`"claude --model opus"`), so nothing can render *which* agent and model a
session will use without re-parsing an argv string at display time — and pop
never showed it at all for a dashboard-launched session. And the `[tasks]` root
had accumulated keys at three different scopes: kind-scoped (`implement`,
`verify`), kind-shared (`max_tries`, `attempt_retry_delays`), and
preset-scoped (`presets.<name>.output`), with `tasks.git` — read only by the
drain's commit path — sitting at root as though it were shared.

## Decision

1. **Agent lists are kind-scoped, under the `[work]` root.**
   `[work.implement].agents`, `[work.verify].agents`, `[work.routine].agents`
   and `[work.attended].agents`. `[work]` already documents itself as a
   container "free for later non-daemon Work keys" beside `[work.daemon]`, and
   Task sets and Routines are both Work kinds, so this is the root the lists
   should always have had. The pre-cut `[tasks.*]` tree moves with them.
2. **Attended is one group, not one group per surface.** Every human-facing
   session shares `[work.attended].agents`. Splitting it per verb was rejected
   for the reason ADR-0187 rejected per-call-site permission modes: twelve
   places to forget is not a design. The **Routine refinement session** is an
   attended session and reads this group, not `[work.routine]`.
3. **No shared defaults at the `[work]` root.** `max_tries` and
   `attempt_retry_delays` are declared in each kind that retries — implement and
   verify — rather than once at the root with per-kind overrides. They are not
   universally reused: routine and attended have no retry loop, so a root
   default would be a value most groups ignore. The cost is knowing to set two
   keys when you want one cap; the gain is that every key in the tree belongs to
   exactly the thing that reads it.
4. **Keys that are not kind-scoped leave the tree.** `tasks.git` becomes
   `[work.implement].git` (only `run_plan.go` reads it). `tasks.presets.<name>
   .output` becomes `[agents.<preset>].output` — it is keyed by agent preset,
   means the same thing to every kind, and describes how pop parses that agent's
   stream. This is what the `[agents]` root becomes once
   [ADR-0195](0195-an-attended-entry-owns-its-whole-invocation.md) empties it:
   settings keyed by preset, as against settings keyed by kind.
5. **One entry type, everywhere, as a table.** An entry is
   `{ display_name, cmd }` — `cmd` is the whole invocation the entry stands for,
   `display_name` is what a picker and a log line call it. A bare string stays
   valid as sugar for `{ cmd = "<string>" }`, decoded the way
   `[pane_monitoring].topic_agents` already decodes a mixed string-or-table
   array (`config/topic_step.go`). Named entries are what make the resolved
   agent renderable *before* launch, which is the whole point of
   [ADR-0196](0196-one-agent-override-picker-and-attended-gates-become-inline-tui.md).
6. **`output = "text"` survives, at its new address.** It is not a display
   preference: it suppresses the adapter's stream-JSON flags entirely, and with
   them usage, cost, turn counts, tool timing, actual-model reporting and
   **Agent proceed verdict** detection. It stays because it is the only
   in-config workaround when a vendor changes a stream shape and pop's parser
   breaks — without it that is release-blocking.
7. **Hard cut, with one exception, recorded in `CLEANUP.md`.** The old keys are
   not read. The exception is `tasks.git.commit_config_overrides`, kept readable
   because it was added on request and its user should not lose it silently;
   `[work.implement].git` wins when both are set.

## Consequences

- A machine whose `config.toml` still says `[tasks.implement].agents` gets the
  built-in `claude` default with no message, because an unread key is an unknown
  key. That is the acknowledged risk of decision 7 and the reason a loud
  unknown-key warning at load matters more after this change than before it.
- `[work.implement].max_tries` and `[work.verify].max_tries` can now drift
  apart, where one root key kept them together. Decision 3 accepts that.
- Attended sessions stop inheriting the implement list's head. Anyone relying on
  that — which is everyone, since it was the only behaviour available — must
  name their attended agents once in `[work.attended].agents`.
- The `[routines].agents` → `[work.routine].agents` move keeps its two-level
  override intact: a Routine manifest's own `agents` still beats the group.
