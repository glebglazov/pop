---
status: accepted
---

# Managed worktrees can be created ahead of a Task set, from the Worktree picker

> **Relates:** amends [ADR-0147](0147-managed-worktrees-are-provisioned-eagerly-at-the-operator-s-request.md) (managed provisioning is no longer exclusively set-triggered), [ADR-0076](0076-worktree-picker-owns-interactive-creation.md) (the picker gains a second create action), [ADR-0110](0110-managed-worktrees-surface-in-the-project-picker-via-a-filesystem-walk.md) (a managed dir may now legitimately have no store row), and [ADR-0116](0116-managed-worktree-teardown-is-reference-counted.md) (the refcount predicate is unchanged; only its trigger set grows).

Design sessions produce a docs commit — the glossary fragment and any ADR — and the implementation that follows should be built on top of it. Doing the design in the Trunk worktree and the implementation in a managed one splits those apart: the managed worktree is forked at `register --managed`, so any docs commit made on trunk *after* that fork is absent from the checkout the drain runs in. `RefreshCommitlessManagedBranch` heals the case where the managed branch is still empty, but not the case where the set is already part-drained. The reliable arrangement is to grill *inside* the worktree the implementation will use, which requires the worktree to exist before the set does.

Managed worktrees could not exist before a set, for one reason: the directory name **is** the Task set identifier (`ProvisionWorktree`, `<root>/<repoKey>/<safe-set-id>`).

Decision: **the Worktree picker can create a managed worktree ahead of any Task set.** A second create key alongside `ctrl+a` picks a base ref — trunk preselected, Enter accepts — generates the name (`scratch-<YYYYMMDD>-<n>`, branch `pop/scratch-<YYYYMMDD>-<n>/<stamp>`), forks under the managed-worktree root, and attaches, with no name prompt. `ProvisionWorktree` takes a start-point instead of assuming the trunk's `HEAD`; `register --managed` and `bind-worktree --managed` keep passing it.

Three rules make that safe without new machinery:

- **Location stays the teardown marker.** `shouldOfferManagedCheckoutTeardown` is unchanged — under the managed root, zero non-archived sets bound. A worktree created here satisfies it from birth, and becomes an ordinary managed worktree the moment `to-tasks` binds a set to it. **Fold** then tears it down through the existing path; no trigger is added.
- **`Provisioned` means "pop created this directory", derived from location.** A set that merely adopts a managed-root checkout records `Provisioned: true`, so the flag and `TeardownWorktree`'s "provisioned bindings only" contract stop disagreeing.
- **The directory name is a label, never a key.** Resolution reads `RuntimePath` from the binding row, so the generated name is never reconciled with the set identifier that later binds it. Nothing renames, and `fold` says nothing about the mismatch.

The new state — a managed worktree with zero live referents — is named an **Unbound managed worktree**. It is surfaced in the Worktree picker with a marker, which costs that surface a binding-store read it did not previously make. Deleting it is the picker's existing delete action.

## Considered Options

- **Go back to lazy managed provisioning.** Rejected — it does not solve this (the fork base would simply move to first drain, still after the docs commit only by luck) and it reinstates the ADR-0147 deadlock: an unprovisioned managed set resolves to no checkout, and every consumer of that resolution has to invent one.
- **Record "pop created this checkout" in a new store table or a marker file, and key teardown on that instead of location.** Rejected — creation predates the binding row that could otherwise hold the fact, so it needs durable state of its own. The managed root already *is* that record, for free.
- **A sweep verb (`pop worktree prune`) for Unbound managed worktrees.** Rejected for now — the picker lists them (they are real git worktrees) and already has a delete key. A reaper for a state you can see and remove in one keypress is machinery for its own sake.
- **Auto-delete an Unbound managed worktree once it is clean and carries no commits absent from trunk.** Rejected — the predicate is sound (it is `branchHasCommitsNotInTrunk`), but an abandoned grill worktree holds exactly the docs commit that motivated this ADR. It gets flagged, never swept.
- **Let `ctrl+a`'s empty-submit mean "pop-managed".** Rejected — empty-submit already means "derive the name from the base ref"; one keystroke would pick a different location *and* a different naming scheme.

## Consequences

- Managed provisioning is no longer exclusively set-triggered, so ADR-0110's "orphaned managed dir the store no longer tracks" stops being only residue: it is now also a legitimate birth state.
- The picker's managed-create path is a human, attended surface. ADR-0029's trust boundary for the unattended daemon is untouched.
- The key binding (`ctrl+t`; `ctrl+g` is free in the picker but is *abort* in readline and emacs muscle memory) is the one detail here chosen on taste rather than constraint, and is cheap to change.
