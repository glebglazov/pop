---
fragment: C1F34703
generation: 0032
branch: master
---

+ Drain header
  The unconditional opening lines of a whole-set **Implement**: printed before the status table, naming the **Task set identifier**, the resolved **Runtime path**, and the **Worktree binding** kind (managed or adopted). It adds "invoked from" only when the invocation directory differs from the Runtime path, and announces a just-recorded **Default binding** at the moment it happens. It replaces the old conditional outside-the-binding report — where a drain runs is always stated, not only when it surprises. A single-task file run prints a sibling current-checkout line instead, since it has no drain.
  avoid: drain banner, location line, worktree line
  under: Language

+ Task result line
  The one line the drain prints at every per-task ending, at the single chokepoint that sees them all: a glyph, the `<set>/<task>` reference, and the outcome word — green for done, red for failed or out of agents, yellow for quota-paused or interrupted. It absorbs the former success-only completion line; the implementation-commit detail stays beneath a green one. Lines always print; color follows the drain output layer's TTY and NO_COLOR handling. Blocked and Deferred are set-level terminals and never appear here. Stdout-only by design: the **Work journal** stays drain-grain, whose severe rows already carry every per-task outcome a returning human must see, and a daemon drain shows these lines in its own pane.
  avoid: per-task summary, task footer, journal task row
  under: Language
