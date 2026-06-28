---
status: deferred
---

# `pop tasks implement` adopts its current checkout into the binding model

> ⚠️ **DEFERRED — intended design, not shipped behavior.** Same posture as ADR-0028; amends ADR-0031 (binding state home), extends ADR-0028/ADR-0030 (integration is trigger-agnostic).

## Context

Worktree provisioning, bindings, mergeability, and integration were all framed as **Queue**
machinery (ADR-0028 through ADR-0035). But ADR-0028 already noted the real unit is the **checkout**,
not the Queue — "the feature's home is Task set + Project + Runtime path, not the Queue; the Queue
is a beneficiary." When an operator runs a bare `pop tasks implement` in a repo where they keep
their own worktrees, they expect their worktree setup to matter — yet `implement` had no relationship
to the binding/integration model at all. The question "should `implement` grow worktree support?"
forced the conflation out into the open: **how a set is triggered (queue vs implement) is orthogonal
to which checkout it drains in (trunk vs worktree)**, and integration only ever cared about the
latter.

## Decision

`pop tasks implement` participates in the binding model as an **adopter, not a provisioner**.

- **Adopt-where-you-are.** `implement` runs in — and binds the set to — its **current checkout**.
  It never runs `git worktree add` itself. Running it inside an existing worktree creates a
  never-delete **adopted** **Worktree binding** for that checkout; running it in trunk leaves the set
  with no worktree binding. This needs **no project config**: the nested-worktree problem (provisioning
  a worktree from inside a worktree) cannot arise, because `implement` never provisions.

- **Integration membership follows the checkout, not the trigger.** A set drained in a worktree —
  by `implement` or by `pop queue run` — enters the **Integration backlog** (new glossary term: the
  derived, read-only view over non-trunk bindings plus their **Mergeability**; *not* a scheduler, and
  deliberately not named "merge queue" to avoid colliding with **Queue**). A set drained in trunk is
  never integrateable. Integration is one verb over the backlog, identical regardless of how the set
  was drained.

- **Binding state moves out of the daemon.** ADR-0031 recorded bindings "in Queue daemon state."
  Because `implement` runs without the daemon yet must see and write the same 1:1 (set ↔ checkout)
  association — otherwise a failed `implement` worktree run would be re-provisioned and orphaned by a
  later `queue run`, the exact failure ADR-0031 fixed — bindings move to **shared per-repository drain
  state owned by a dedicated provisioning/binding module**. `pop queue run` and `pop tasks implement`
  are both callers of that module.

- **Queue keeps auto-provisioning.** The Queue is AFK; no human is present to create a worktree, so
  it retains pop-owned `git worktree add` gated by `worktree_ready` (ADR-0028/0029). `.pop.toml`
  survives, shrunk to that one flag. We do **not** make pop a pure binder (no provisioning anywhere) —
  that would gut the Queue's hands-free fan-out.

- **Attended completion prompt.** When an interactive `implement` drains a set to Done in a worktree,
  its completion prompt offers integration alongside the final HITL completion. A trunk drain offers
  nothing to integrate (already on the working branch). `--yes` **never** auto-integrates a worktree
  drain unless the repo explicitly opted in with `auto_merge_clean = true` — the same opt-in that
  governs `pop queue run`, applied trigger-agnostically. `--yes` means "don't ask me," not "do
  something risky."

## Considered options

- **Pop never provisions (pure binder everywhere).** Rejected — reverses ADR-0029 and forces the
  AFK Queue operator to pre-create and `bind-worktree` every set's checkout by hand, gutting the
  hands-free fan-out that is ADR-0028's entire value.
- **`implement` auto-provisions when `worktree_ready`, like the Queue.** Rejected — it needs
  nested-worktree guard logic (provisioning from inside a worktree) and reintroduces the
  zero-setup/runnable-checkout constraint; adopt-where-you-are sidesteps both since the human (who is
  present) made the worktree.
- **Kill `.pop.toml` entirely.** Rejected — ADR-0035 already evicted machine-specific routing to
  global `[repo."<path>"]` overrides; what remains (`worktree_ready` as a repo-nature capability) is
  the Queue's only provisioning gate and earns its keep.
- **Inline-only integration bolted onto `implement`.** Rejected as the *model* — integration is
  trigger-agnostic and lives over the backlog. The attended completion prompt is a convenience over
  that model, not a separate integration path.
- **`implement --yes` never integrates even with `auto_merge_clean`.** Rejected — the opt-in
  expresses the operator's accepted risk for the repo and should not depend on which verb drained the
  set.

## Consequences

- **Glossary:** adds **Integration backlog**; redefines **Worktree binding** (shared module-owned,
  checkout-aware, decoupled from Queue) and **Implement** (adopts its checkout). Recorded live in a
  CONTEXT fragment.
- **State:** the binding map and set-scoped keys move from daemon-private state to a shared
  per-repository store; a provisioning/binding module owns provision/adopt/lookup/teardown, and
  `queue` + `tasks` become callers. This amends ADR-0031's "lives in Queue daemon state."
- **Safety:** `implement` adopting a checkout is always a never-delete **adopted** binding — it can
  never trigger a directory deletion, consistent with ADR-0035's adopt-default posture.
- **Deferred:** like ADR-0028, this records the decision; no code ships from it yet.
