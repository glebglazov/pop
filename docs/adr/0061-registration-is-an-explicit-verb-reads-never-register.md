# Task-set registration is an explicit verb; reads never register

Status: accepted — amends [ADR-0047](0047-manifest-auto-drain-seeds-at-registration.md) (auto-drain seeds at registration) and removes the auto-registration side effect from the read path that [ADR-0060](0060-queue-resolves-from-markers-not-live-git.md) makes a pure read.

## Context

Registration — entering a discovered Task set into **Task state** with a priority/order, and seeding its `auto_drain` (ADR-0047) and worktree directive (ADR-0059) — happens today as a side effect of `refresh` (`mergeNewRegistrations`). Every caller that *looks* at a repo's tasks registers: `pop tasks status`/`list`, and the `pop queue dashboard` poll. That is a write on the read path, and it is doubly wrong now:

- **Racy cwd.** The dashboard poll runs from wherever it was launched — usually a different repo entirely — yet it registers *another* repo's sets. There is no valid checkout of the target repo in hand at that moment.
- **Surprise activation.** Because `auto_drain` is seeded at registration, merely opening the dashboard can arm an auto-drain — the "no surprises from a read" line ADR-0052/0059 fought for, violated by the act of viewing.

Registration's only write goes through `refreshWith` (the `mergeNewRegistrations` block), so there is a single chokepoint to gate.

## Decision

Registration becomes an explicit, deliberate verb; all reads are pure.

- `refreshWith` gains a `register bool`, defaulting **false**. Every read — `pop queue dashboard`, `pop tasks status`/`list`, completion (already non-registering) — passes false and never mutates state.
- A single command, **`pop tasks register`**, is the sole writer: it discovers on-disk sets, registers the new ones (assigning order, seeding `auto_drain` and the worktree directive), and prints status. It is run from inside the repo, so its cwd is always a valid checkout.
- A Task set that has been authored on disk but not yet registered is **inert**: invisible to the dashboard (`buildRows` renders only registered sets), unscheduled by the queue, never auto-drained. Writing set files is *drafting*; `register` is the deliberate *activate*.

This makes `auto_drain` (ADR-0047) seed at the explicit registration rather than at first sight, with no behavioral change to *what* is seeded — only *when*, and by whose deliberate act.

## Considered options

- **Name the command `auto-register`.** Rejected: `Auto-registration` is already a glossary term for *panes* registering on first monitoring report — an unrelated domain. Overloading it would confuse two systems. `register` (or `sync`) keeps the verb unambiguous.
- **Let `pop tasks status` register but not the dashboard.** Rejected: it still couples a write to a read, just a less-racy one. A pure-reads invariant with one explicit writer is cleaner and easier to reason about than "these reads write, those don't."
- **Render unregistered discovered sets on the dashboard (decouple visibility from registration).** Rejected for now: it keeps creation→visibility automatic but reintroduces on-disk-vs-state divergence in the view and a default-ordering question for unregistered sets. Treating an unregistered set as inert is the simpler contract; `register` is one command away.
- **Keep auto-registration, just make it side-effect-safe.** Rejected: it leaves the write on the read path, so the racy-cwd and surprise-activation problems remain in principle.

## Consequences

- A freshly authored set does not appear in the dashboard or get scheduled until `pop tasks register` is run in its repo. This is the intended "draft → activate" gate, and it removes auto-drain-by-looking.
- `refreshWith`'s register flag is the whole mechanism; no scattered conditionals.
- **Embedded planning skills must call `register`, not `status`.** `cmd/skills/pop/to-tasks/SKILL.md` (lines ~152, ~159) currently runs `pop tasks status <set>` precisely to "trigger lazy discovery and confirm the set registered" — the side effect this ADR removes. Those steps must switch to `pop tasks register <set>`, which both activates the set and reports its status. Any other embedded skill that relies on a read to register must do the same (only `to-tasks` does today).
- Glossary: **Registration** is now an explicit act, not a discovery side effect; the **`Auto-registration`** term remains reserved for the pane-monitoring domain.
