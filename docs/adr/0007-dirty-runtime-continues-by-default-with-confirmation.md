# Dirty runtime continues by default with confirmation

`pop workload run-issue` and `run-issues` start from a dirty runtime checkout by default. Previously a dirty checkout was rejected unless the operator passed an explicit `--allow-dirty` strategy; the absence of a strategy meant "require a clean checkout". Now `continue` is the default. Whenever the runtime is dirty — under any strategy — the command prints the full `git status`, states what the chosen strategy will do to the dirty state, and requires interactive `y` confirmation before proceeding. `--yes` auto-confirms; a non-interactive run (no TTY) without `--yes` is rejected. The `reject` sentinel that encoded the old fail-on-dirty default is removed.

## Why

In practice the runtime is dirty more often than not — local edits, an interrupted prior run, scratch files. The old default turned every such case into a hard stop that forced the operator to recall and retype an `--allow-dirty` value before any work could start, even though the overwhelmingly common intent is simply "yes, go, with the changes in place". The rejection protected against running against an unexpected dirty tree, but it paid for that protection on every invocation.

Continue-by-default keeps the convenience while preserving the safety the rejection provided: the operator still sees exactly what is dirty (`git status`) and is told what will happen to it, and nothing proceeds without an affirmative `y`. The protection moves from "refuse and make them re-invoke" to "show and confirm" — one keystroke instead of a remembered flag, with the same information in front of them at decision time.

Non-interactive safety is preserved structurally rather than by a strict mode: with no TTY and no `--yes`, the confirmation cannot be answered, so the run is rejected. An operator who wants to abort an interactive dirty run answers `N`. Between those two, a standalone `reject` strategy had no remaining role, so it was removed rather than carried as dead surface area.

## Considered Options

- **Keep require-clean as the default (status quo).** Rejected: penalizes the common case on every invocation to guard against the rare one; the guard is better served by show-and-confirm.
- **Continue by default with no prompt.** Rejected: silently running against an unexpected dirty tree is the exact hazard the old default existed to prevent; the prompt keeps that protection at negligible cost.
- **A separate dirty `[y/N]` prompt in addition to the existing run confirmation.** Rejected: two confirmations on one run is redundant friction. The `git status` and strategy line fold into the existing run confirmation, so a dirty run is still a single prompt.
- **Confirm only on the implicit default; skip when an explicit strategy is passed.** Rejected: a `commit-and-continue` or `stash-and-continue` operator still benefits from seeing the dirty state before it is committed or stashed; the prompt's message is tailored to the chosen strategy rather than suppressed.
- **Retain `reject` as an opt-in strict mode.** Rejected: no flag value selected it, the no-TTY rejection and an interactive `N` already cover the fail cases, and keeping it is unused surface.

## Consequences

The `DirtyRuntimeReject` sentinel and the "runtime checkout is dirty; commit or stash" rejection path are removed; the unset flag value now resolves to `continue`. The pre-execution dirty handling shifts from "warn then proceed (or reject)" to "show `git status`, describe the strategy effect, confirm" and shares the existing confirmation plumbing (`--yes` bypass, no-TTY rejection). Tests that asserted the fail-on-dirty default must be reframed around the confirmation; golden output for the dirty path must include `git status` and the strategy line. The CONTEXT.md glossary entry **Dirty runtime strategy** is updated to drop the clean-checkout requirement and record the always-confirm behavior. A future reviewer should not reintroduce a silent dirty run "for automation" — automation uses `--yes`, which still surfaces the state in the output.
