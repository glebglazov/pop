---
fragment: 11516ff8
generation: 0043
branch: master (grill: managed-drain-routing regression + retarget-to-managed)
---

~ Bind worktree
  The human act of retargeting where a **Task set** drains, via `pop tasks bind-worktree <set>`. Its default mode, run from inside the target checkout, adopts that checkout as an **adopted** **Worktree binding** (pop never deletes the checkout). `--managed` instead records a **managed intent** on the set — dropping any existing binding — so the next Queue drain provisions a pop-managed worktree forked from the **Trunk worktree**, exactly as `register --managed` would have; provisioning stays lazy, never immediate. Symmetric sibling of **Unbind worktree**; both mutate the shared binding store and run without the daemon. Refuses to re-point a set that is already bound elsewhere without `--force`, and never re-points a set holding a live **Runtime execution lock**.
  was: The human act of pointing an existing, human-owned git checkout at a **Task set** so a later drain targets that checkout — `pop tasks bind-worktree <set>`, run from inside the target checkout. It creates an **adopted** **Worktree binding** (pop never deletes the checkout). Symmetric sibling of **Unbind worktree**; both mutate the shared binding store and run without the daemon. Refuses to re-point a set that is already bound elsewhere without `--force`, and never re-points a set holding a live **Runtime execution lock**.
