# Layout sizing is apportioned once in exact cells, and splits carry `-l`

A **Layout** container derives its children's geometry exactly once. An
**Apportionment** turns the child weights into exact cell counts against the
container's **Cell budget** — its extent along the split axis minus the `N-1`
border cells — using largest-remainder so the counts sum to the budget.
**Layout realization** then consumes that one result twice over: each tmux
split passes its child's count as `-l <cells>`, and the correction pass resizes
to the same counts. Weights are never independently turned into geometry a
second time.

## Context

`realizeContainer` (`cmd/template.go`) derived geometry twice from the same
weights, by two different formulas. The splits used a percentage of *what
remained* after earlier children were carved off
(`remainingWeight * 100 / previousRemaining`); the following
`resizePanesByWeight` pass ignored that and computed absolute cells
(`totalSize * weight / totalWeight`). Two derivations of one truth can
disagree, and all three known defects lived in the gap between them:

- The percentage is integer division, so a lopsided tree (previous weight 200,
  remainder 1) yields `0`, the `-p` flag is dropped entirely, and tmux falls
  back to 50/50.
- The correction pass read `WindowSize`, but recursion realizes nested
  containers *after* resizing, so an inner container re-derived its children
  against the whole window. Alternating directions survive by accident — a
  full-height column has the window's height — while same-direction nesting
  overruns and tmux clamps it.
- Neither formula accounted for borders. `N` panes eat `N-1` separator cells,
  so the sizes summed past the real extent and the tail pane absorbed the
  deficit plus every rounding remainder.

The obvious simplification — drop the split sizing entirely, since the
correction pass overwrites it — was measured against real tmux and rejected.
Because each split cuts the *last* pane made, unsized splits halve the tail
every time: in an 80x24 window, six equal-weight children die at the fourth
split with `size or position no space for a new pane` (heights 12/5/2/2), while
the weighted splits place all six at 4/3/3/3/3/3. The provisional sizing is
what makes ordinary multi-pane layouts possible at all, so a rounding defect in
it is one step from a hard failure, not a cosmetic flash.

`-l` replaces `-p` because it is what the code actually means. `-l N` sets the
new pane's size to exactly `N` cells and charges the border to the surviving
pane (`-l 8` on a 24-row pane gives 8/15, not 8/16) — the same cells the
correction pass already computes, so both phases can share one number. It is
also the non-deprecated spelling; the deprecated `-p` is still accepted on
current tmux (verified on 3.7b, well past the 3.4 where a bug report claimed it
had been removed), but there is no reason to keep a second unit in play.

## What follows from it

- **`PaneSize` replaces `WindowSize` on the `Tmux` interface.** A container
  reads its own pane's extent before splitting, which is correct at any depth
  and identical to the old value at the top level, where the container is the
  window's only pane. `WindowSize` had exactly one caller and retires with it.
  The lesson recorded in its doc comment moves across: the query must be
  targeted with `-t`, because an untargeted `display-message` returns the
  *client's* window size, which for a detached session born from a Workbench
  (80x24) is not the window being built.
- **Apportionment is a pure function**, taking a budget and weights and
  returning cell counts, with no tmux dependency. That is where the rounding,
  border, and lopsided-weight cases get their unit tests; nothing about
  geometry needs a tmux server to verify.
- **A layout that cannot fit fails loudly**, naming the window and the Pane
  spec, rather than letting tmux emit `no space for a new pane` partway down
  the tree and leaving a half-built window standing.
- **The correction pass stays**, but demotes to a correction: it repairs
  whatever tmux clamped, instead of offering a second opinion on the geometry.
- **A real-tmux layout test lives behind a `live` build tag**, run from its own
  make target beside `live-agent-smoke` and never from `make test`. Same-
  direction nesting and the minimum-size floor are exactly the behaviours the
  argv-asserting fake cannot reach, and the repo's CI runs no tests at all, so
  the tag is the only thing keeping a tmux dependency out of the default suite.
