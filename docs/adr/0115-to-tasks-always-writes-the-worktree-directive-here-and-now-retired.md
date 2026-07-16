---
status: accepted
---

# to-tasks auto-binds the current worktree at register; worktree & auto-drain leave the manifest

> **Relates:** supersedes [ADR-0113](0113-to-tasks-here-and-now-binds-current-worktree-at-the-skill-layer.md) (the here-and-now wrapper is deleted), amends [ADR-0059](0059-task-set-may-declare-a-worktree-directive.md) (the `worktree` manifest key is retired), keeps [ADR-0072](0072-worktree-directive-is-queue-only-foreground-implement-binds-the-current-checkout.md) (foreground implement still binds the current checkout), relates [ADR-0104](0104-out-of-band-mutators-require-checkout-quiescence.md) (auto-drain is a runtime consent bit) and [ADR-0116](0116-managed-worktree-teardown-is-reference-counted.md) (N-sets-to-one-checkout teardown stays reference-counted).

## Context

ADR-0059 made the **Worktree directive** an opt-in manifest key, so the default `to-tasks` output left every set unbound and Queue-undrainable until a human bound a checkout by hand. ADR-0113 bolted on a second skill, `to-tasks-here-and-now`, that force-bound the current worktree behind a guard refusing the trunk, pop-managed, and already-bound checkouts.

The first cut of this ADR replaced both with one rule: `to-tasks` **always** wrote a `worktree` directive into the manifest (defaulting to the current checkout's name), killing the here-and-now split and the unbound default. That removed the two-skill ceremony, but left binding as a **lazy manifest seed**: `register` recorded the intent, the *first Queue drain* provisioned or adopted, and nothing was visible in between. Three problems survived:

- **No visible binding until first drain.** Checking the store or dashboard right after `register` showed no binding — it looked broken even when it wasn't.
- **Dual source of truth for auto-drain.** `auto_drain` lived in both the manifest (a seed) and the store (dashboard-authoritative), a confusing split.
- **Machine-specific data in a portable artifact.** A worktree *name* baked into the manifest is awkward for `pop tasks transfer` across machines.

And the manual `bind-worktree` step was still needed whenever the manifest name didn't match a checkout on the machine.

## Decision

The **intent is unchanged** from the first cut of this ADR: a set auto-binds the current worktree with no ceremony, and the trunk is not special. Only the **mechanism** moves — off the manifest and onto `register`/CLI/dashboard.

- **The manifest (`index.json`) no longer carries `worktree` or `auto_drain`.** It holds only the task list. The registration-seed read of these keys is removed.
- **`pop tasks register` infers the current checkout** — `basename $(git rev-parse --show-toplevel)` — and **eagerly adopts** it as the set's **Worktree binding** on first registration, the same adopt path as **Bind worktree**, run automatically. The binding exists and is visible the moment the set is registered.
- **`register --managed`** records a managed intent instead; its worktree is still provisioned **lazily** at first drain (forking a worktree + branch for a set that may never drain is too heavy to do at register).
- **`register --auto-drain`** (default **off**) sets the auto-drain bit. `pop tasks auto-drain` and the **Queue dashboard** toggle remain authoritative afterward.
- **`to-tasks` stops writing manifest keys.** Its `managed`/`isolated` and `auto-drain`/`drain` arguments plumb through to `register --managed` / `register --auto-drain`.
- **Re-register keeps the first binding.** Rebinding is the explicit `pop tasks bind-worktree <set> --force`, run from inside the target checkout.
- **Foreground `pop tasks implement` is unchanged** (ADR-0072): it binds/rebinds the current checkout regardless.
- **Legacy manifests** still carrying `worktree`/`auto_drain` are **ignored with a deprecation warning**, not made Malformed; already-registered sets keep their stored binding — no forced migration.

The shape it converges on: **binding and auto-drain are a register/CLI/dashboard concern, not a manifest concern.**

## Considered options

- **Keep binding in the manifest as an always-on lazy seed (the first cut of this ADR).** Rejected — invisible binding until first drain, a dual source of truth for auto-drain, and machine-specific names in portable archives.
- **Mint a new ADR to supersede the first cut.** Rejected — the *decision* (auto-bind the current worktree, no ceremony, trunk not special) did not change; only the mechanism did. A fresh ADR on the same micro-topic days later is churn, so this record is rewritten in place, preserving the prior manifest-seed mechanism above so the "why isn't this in the manifest?" question stays answered.
- **Provision managed worktrees eagerly at register too.** Rejected — creates real git state for sets that may never drain; managed stays lazy.
- **Default auto-drain on at register.** Rejected — unattended commits onto the current branch by default is too aggressive; it stays opt-in per invocation via the skill argument.
- **Foreground implement refuses to drain outside the bound checkout.** Rejected — that supersedes ADR-0072's author-then-implement-elsewhere flow; foreground keeps rebinding the current checkout.

## Consequences

- The binding is materialized and visible at `register`, eliminating both the manual-bind step and the invisible-until-drain state that motivated this rework.
- The manifest is purely tasks; a `transfer` archive carries no machine-specific worktree name, and `register` re-infers per machine.
- Single source of truth: the store — written via `register`/CLI/dashboard — owns binding and auto-drain.
- The Registration-seed machinery for `worktree`/`auto_drain` is removed from the register/refresh path.
- N-sets-to-one-checkout sharing still arises when `register` runs in a checkout another set already uses; ADR-0116 reference-counted teardown still governs it.
- Code: manifest parsing drops the `worktree`/`auto_drain` keys; `pop tasks register` gains `--managed`/`--auto-drain` and the eager-adopt step; the `to-tasks` skill stops writing the keys and plumbs its arguments to `register`. `to-tasks-here-and-now` remains deleted.
