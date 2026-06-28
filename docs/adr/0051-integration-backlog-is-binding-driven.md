---
status: accepted
---

# Integration backlog membership is binding-driven, not mergeability-record-driven

The **Integration backlog** is the set of non-trunk **Worktree binding**s for Done sets awaiting reconciliation. Membership is the binding's existence alone; **Mergeability** is a left-joined property of a member, never the gate for membership. A member whose mergeability has never been computed is shown as `unknown` and stays actionable (its `pop tasks integrate <set>` hint stands, and Integrate computes mergeability when run). Both the dashboard and `pop queue status` derive their "awaiting integration" view from this one binding-driven source, so a Done worktree set can never be visible to one surface and invisible to the other.

## Context

A Done worktree set was visible under `pop queue status` "Active worktrees" (which reads all bindings) but absent from `pop queue dashboard` and from status's "Awaiting integration" bucket — both of which gated on a recorded mergeability entry. A set that reached Done outside a `pop queue run` drain (e.g. foreground `pop tasks implement`, or manual completion) never gets a mergeability record written, so its managed worktree binding became an orphan only `status` could see and nothing could act on. The binding is released only by Integrate or Unbind worktree; finishing the set does not release it, so the orphan persists indefinitely.

## Considered Options

- **Binding-driven membership, lazy mergeability (chosen).** Enumerate non-trunk bindings; left-join the mergeability record; show `unknown` when absent. No mergeability computation during the read-only listing.
- **Record-driven membership (rejected).** The prior behaviour. Simpler readers, but a set must be drained by the queue to ever enter the backlog, stranding foreground/manual Done worktree sets.
- **Eager compute on listing (rejected).** Compute mergeability for record-less members during each dashboard poll / status build. Accurate immediately, but forks `git merge-tree`/`diff` per orphan per poll and tempts writing records from a surface that is contractually read-only.

## Consequences

Listing the backlog stays read-only and cheap; `unknown` is a first-class, actionable backlog state. A future reader tempted to "fix" the `unknown` rows by computing mergeability inline should not — the read-only-no-compute stance is deliberate; mergeability is populated at drain-Done outcome or at Integrate time.
