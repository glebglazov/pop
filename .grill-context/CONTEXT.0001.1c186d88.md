---
fragment: 1c186d88
generation: 0001
branch: master
---

- Worktree directive

- Manifest auto-drain seed

- Registration seed

~ Task manifest
  The `index.json` within a Task set. It remains the source of truth for task eligibility and completion, and carries **only** the `tasks` array — it no longer holds set-level `worktree` or `auto_drain` keys (ADR-0115: binding and auto-drain are register/CLI/dashboard concerns, not manifest fields). A legacy manifest still carrying those keys is **not** Malformed; the keys are ignored with a deprecation warning. Task-level fields inside the array still follow their declared types.
  was: The `index.json` within a Task set. It remains the source of truth for task eligibility and completion. It may optionally carry set-level keys beside the `tasks` array — today `auto_drain` and the Worktree directive (`worktree`) — that express authoring intent consumed once at first registration into Task state as Registration seeds; those keys are not re-applied on refresh. A malformed `worktree` or non-boolean `auto_drain` is a contract fault that makes the set Malformed. to-tasks always writes the Worktree directive.

~ Worktree binding
  A durable association between one Task set and one git checkout for that set's execution, recorded in shared per-repository drain state and consulted first by Drain routing. It is created **eagerly at first `pop tasks register`**, which infers the current checkout (`basename $(git rev-parse --show-toplevel)`) and **adopts** it — the same adopt path as Bind worktree, run automatically — so a set is bound and visible the moment it registers (no lazy manifest seed, no manual bind step). `register --managed` instead records a managed intent whose worktree is provisioned lazily at first drain. Re-running register keeps the first binding; rebinding is the explicit `pop tasks bind-worktree <set> --force`. Bindings are per-set, and a checkout may back several sets (N-sets-to-one-checkout), with managed-worktree teardown reference-counted (ADR-0116). A binding carries a `Provisioned` bit: managed (pop ran `git worktree add` under its data dir) versus adopted (register inferred the current checkout, a human ran Bind worktree, or a foreground Implement adopted its current checkout). A foreground `pop tasks implement` still rebinds the current checkout (ADR-0072).
  was: A durable association between one Task set and one git checkout, recorded in shared per-repository drain state and owned by a provisioning module both `pop queue run` and `pop tasks implement` call. The universal per-set drain router: Drain routing consults bindings first. Created lazily — the first unbound Queue drain provisioned (from a managed directive) or adopted (from a named directive) and recorded the binding; register recorded only the intent. Bindings per-set; checkout shareable across sets (ADR-0115/0116); Provisioned bit distinguishes managed vs adopted.

~ Drain routing
  Resolving which checkout a whole-set drain runs in, split by trigger. A foreground `pop tasks implement` always targets the current checkout: a live Runtime execution lock elsewhere refuses; otherwise it rebinds to the current checkout (recording a Default binding, prompting to delete an idle managed worktree it rebinds away from). The Queue honours the set's Worktree binding — created eagerly at register (the adopted current checkout) or, for a `--managed` set, provisioned from the Trunk worktree on the first drain; a set with neither is not drainable and surfaces as a needs-bind fault, never an invented checkout. There is no manifest Worktree directive any longer (ADR-0115) and no integration-target fallback. Single-task file runs stay current-checkout.
  was: Resolving which checkout a whole-set drain runs in, split by trigger. Foreground implement targets current checkout (rebinds, records Default binding). The Queue honours bindings and directives only: an existing binding wins; else the set's Worktree directive provisions a managed worktree (from the Trunk worktree) or adopts a named one; else needs-bind fault. No integration-target fallback.
