# `implement` is one verb dispatching by target shape

Status: accepted (supersedes the `run`/`drain` verb split in ADR 0013)

ADR 0013 split task execution into two sibling verbs — `run` (one task) and `drain` (whole set) — partly to avoid the `run-issue`/`run-issues` tab-completion collision. In practice the operator almost always drains a whole set and rarely targets a single task, and the singular-vs-plural verb pair became a recurring "which one was the single one again?" friction with no upside.

Decision: collapse `pop tasks run` and `pop tasks drain` into a single command, **`pop tasks implement`**, that dispatches on the **Task target reference** shape rather than on a verb the human has to remember. A `<task-set>/<file>.md` reference runs exactly that one task; a bare `<task-set>` identifier — or no argument, in which case pop auto-selects the highest-priority Ready set — drains the set. The name matches the **Task executor**'s job and the implementation commits it produces; it does not collide with any other `pop tasks` sibling (the one `i`-prefixed subcommand), so the 0013 tab-completion concern dissolves. All execution mechanics are preserved unchanged: HITL gate prompt, agent-quota pause, per-session AFK-execution consent ("Run AFK tasks in this Task set?"), single-task confirmation ("Run task?"), dirty-runtime handling (ADR 0007), and the no-Ready-set HITL fallback to one unambiguous Human-blocked set.

## Considered options

- **Keep `run`/`drain`, accept the friction** — rejected; the verb pair earned its keep only as two tab-completion-safe names, and a single dispatching verb is both safer and lower-friction.
- **`implement <set>` + `--all`/`--once` flags** — rejected; a flag to choose one-vs-many is just two verbs wearing one coat, and it keeps the very distinction the merge is trying to erase.
- **Umbrella verb `run`** — rejected; `run` was glossary-pinned to "exactly one task," so reusing it would overload an existing term, whereas `implement` names the shared execution mechanism cleanly.

## Consequences

- Two capabilities are deliberately removed, both judged vestigial: running a *single auto-picked* task (no-arg now always drains) and running the *next one task from a named set* (`run <set>`). Exactly one task now runs only when a file reference names it.
- The glossary dissolves the separate **Run** and **Drain** entries into one **Implement** entry; the divergent mechanics (HITL gate, quota pause, Human-blocked attendance) keep their own existing entries, so nothing is lost, only re-homed.
- The embedded/dotfiles planning skill `run-task` and any `pop tasks run|drain` references must move to `pop tasks implement` in the same wave, per the 0013 same-wave rule.
