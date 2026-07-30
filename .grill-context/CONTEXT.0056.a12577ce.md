---
fragment: a12577ce
generation: 0056
branch: master
---

+ Cell budget
  A container node's cell extent along its split axis minus the N-1 border cells its N children consume — the amount actually apportionable among those children. Distinct from the container's own size: tmux charges one cell per split to the surviving pane, so a Layout that apportions the raw extent overruns by exactly the border count and the tail child absorbs the deficit.
  avoid: pane size, container size, available space, total size
  under: Workbench

+ Apportionment
  The single derivation of a Layout container's child weights into exact cell counts against its Cell budget, using largest-remainder so the counts sum to the budget and leftovers land deterministically rather than on the last child. Both realization phases consume one Apportionment: the splits pass each child's count as tmux `-l`, and the correction pass resizes to the same counts. Weights are never turned into geometry twice.
  avoid: sizing, weight math, percentage, split ratio
  under: Workbench

+ Layout realization
  The two-phase construction of a live window from a Layout: N-1 successive tmux splits that subdivide the container's own pane (child 0 reuses it), followed by a correction pass that resizes every child to its apportioned size. The splits are load-bearing, not provisional — each cuts the last pane made, so unsized splits halve the tail geometrically and hit tmux's minimum-pane floor at four children in an 80x24 window.
  avoid: apply, instantiation, rendering, pane building
  under: Workbench
