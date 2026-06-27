---
fragment: 3545959b
generation: 0010
branch: master
---

+ Integration target
  The checkout a Task set's work merges into, and the checkout the Queue drains an unbound set into. Derived per repository with no git: a **non-bare** repo's target is its main worktree (the parent of its git common directory); a **bare** repo's target is its **Trunk worktree** declared in config. Distinct from the **execution checkout** (where a drain runs): a set executes on its bound worktree but integrates into this target. A bare repo with no configured trunk has no integration target and its sets surface a config-class error.
  under: Tasks

+ Default binding
  The **Worktree binding** a no-directive drain records to the checkout it resolved on its first run — the current checkout for a foreground **Implement**, the **Integration target** for a headless **Queue** drain. It makes an otherwise-unbound set sticky to where it first ran, so later drains resume the same checkout and branch. An operator **Bind worktree** or runtime override still takes precedence.
  under: Tasks

~ Drain routing
  Resolving which checkout a whole-set drain runs in. Precedence: an existing **Worktree binding** wins (resume in the bound checkout); otherwise an explicit Runtime-path override; otherwise the set's **Worktree directive** seeded at registration (provision a managed worktree or adopt a named one, recording a binding on the first unbound drain); otherwise a **Default binding** to the chosen checkout (the current checkout for **Implement**, the **Integration target** for the **Queue**), recorded so later drains resume there. Pop still never auto-provisions from a *repo* capability while routing — provisioning happens only from an explicit operator act or a per-set authored directive, never inferred. **Implement** and the **Queue** share this policy (one `RouteDrainCheckout`). An unsatisfiable directive (managed with no **Trunk worktree**, or a named worktree absent on this machine) is a config/registration-class error: the set is not drained, surfaced like a registration fault, with no backoff and no silent in-place fallback. Single-task file runs stay current-checkout.
  was: Resolving which checkout a whole-set drain runs in. Precedence: an existing **Worktree binding** wins (resume in the bound checkout); otherwise an explicit Runtime-path override; otherwise the set's **Worktree directive** seeded at registration (provision a managed worktree or adopt a named one, recording a binding on the first unbound drain); otherwise the **current checkout**. Pop still never auto-provisions from a *repo* capability while routing — provisioning happens only from an explicit operator act or a per-set authored directive, never inferred. **Implement** and the **Queue** share this policy (one `RouteDrainCheckout`), so the directive is honoured by foreground and unattended drains alike. An unsatisfiable directive (managed with no **Trunk worktree**, or a named worktree absent on this machine) is a config/registration-class error: the set is not drained, surfaced like a registration fault, with no backoff and no silent in-place fallback. Single-task file runs stay current-checkout.
