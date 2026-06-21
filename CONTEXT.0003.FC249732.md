---
fragment: FC249732
generation: 0003
branch: master
---

+ Drain routing
  Resolving which checkout a whole-set drain runs in, following fixed precedence: an existing **Worktree binding** wins; then an explicit Runtime-path override; then a managed worktree forked from the **Execution base** when the project is **Worktree-ready** and `--inline` is not set; otherwise the current checkout (trunk drains inline). **Implement** and the **Queue** share one routing policy; only error handling on provision failure differs by trigger (foreground Implement fails; Queue spawn may fall back to in-place). Single-task file runs are out of scope — they stay current-checkout operations.
  avoid: checkout picker, runtime resolver, workspace routing
  under: Tasks

~ Worktree binding
  A durable association between one **Task set** and one git checkout for that set's execution, recorded in shared per-repository drain state and owned by the provisioning module that both **`pop queue run`** and **`pop tasks implement`** call — not a **Queue**-private structure. It is the universal per-set drain router: **Drain routing** consults bindings first, then applies the remaining precedence rules. The bound checkout is either the project's trunk checkout or a worktree, and only worktree (non-trunk) bindings enter the **Integration backlog**. A binding carries a `Provisioned` bit recording who owns the checkout's teardown: **managed** (pop ran `git worktree add` under its data dir; pop tears the checkout and branch down on integration or **Unbind worktree**) versus **adopted** (a human pointed an existing, owned checkout at the set via **Bind worktree**, or a foreground **`pop tasks implement`** ran in and thereby adopted its current checkout; pop drains into it but never deletes it — unbind only forgets the association). Bindings default to adopted/never-delete, so a hand-written or unrecognized binding can never trigger a directory deletion; pop deletes only what it demonstrably created. A managed binding's checkout lives at a stable path derived from the set identifier and persists across drain exits, failures, and supervisor restarts so re-spawns resume the same branch rather than forking afresh. If a binding's checkout is missing or no longer registered with git, pop refuses to spawn and directs the human to repair git state or **Unbind worktree** — it never silently re-provisions. **Archive** does not release a binding.
  was: (see base CONTEXT.md Worktree binding — provisioning module as drain router, Provisioned/adopted bits, stable paths, refuse-on-missing, Archive does not release)

~ Integration backlog
  The derived set of **Task set**s whose drain landed in a worktree checkout (not trunk) and now await reconciliation into the working branch they forked from. Membership is determined by which checkout a set drained in, not by how it was triggered: a set that a bare **`pop tasks implement`** ran in a worktree is in the backlog exactly as one a **`pop queue run`** drained there is — and a set drained in trunk never enters it. It is a read-only view over non-trunk **Worktree binding**s plus their **Mergeability**, not a scheduler and not owned by the **Queue** command family. **Integrate** operates on the backlog regardless of trigger. Distinct from **Queue** (the per-project drain supervisor): the backlog routes integration, the Queue routes execution.
  was: (see base CONTEXT.md Integration backlog — same trigger-agnostic membership, ends with "Distinct from Queue")
