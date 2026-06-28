---
status: accepted
---

# Task sets may declare a worktree directive, applied as a registration seed

> **Relates:** amends [ADR-0052](0052-drain-checkout-is-chosen-not-auto-provisioned.md) (drain-routing precedence) and respects ADR-0035 (machine-specific routing stays out of branch-riding files); builds on [ADR-0036](0036-implement-adopts-its-checkout-into-the-binding-model.md) (adopt/managed binding model). Amended by [ADR-0072](0072-worktree-directive-is-queue-only-foreground-implement-binds-the-current-checkout.md): the directive is now **Queue-only** — a foreground `pop tasks implement` ignores it and binds the current checkout — so "honoured by every drain, foreground and Queue alike" no longer holds.

## Context

ADR-0052 removed declarative auto-provisioning: `worktree_ready` — a *repo*-level capability flag — silently fanned every Ready set into managed worktrees, so it was deleted and provisioning became an explicit operator act (`--in-worktree`, the Drain target picker, or `bind-worktree`). That left no way to say "*this particular set* should always drain in its own worktree" without a human arranging it per drain. We want that intent back — but expressed per set, authored deliberately, without reintroducing the surprises 0052 fixed.

## Decision

A Task set manifest may carry an optional `worktree` key beside `auto_drain`:

- `{ "managed": true }` — pop provisions a **managed** worktree (forked from the Trunk worktree; torn down on integration/unbind).
- `{ "name": "<worktree-name>" }` — pop **adopts** the existing worktree of that name on this machine (never deleted). Absent on this machine → error, not silent fallback.
- absent — drain in the current checkout (the 0052 default; the seam is shut by default).

It is a **registration seed**, exactly like `auto_drain`: read once into the persisted `RegisteredTaskSet` at first registration and never re-read from the manifest afterward. Provisioning is **lazy** — registration records only the intent; the first *unbound* drain reads it, provisions/adopts, and records a Worktree binding; later drains resume via that binding. So Drain-routing precedence becomes: **binding → runtime-path override → registration worktree-intent → current checkout**. An operator's `bind-worktree`/override still wins because the binding is consulted first. The directive is honored by **all** drains — foreground `pop tasks implement` and the Queue alike — since both share `RouteDrainCheckout`.

An unsatisfiable directive at drain time — `managed` with no resolvable trunk, or a `name` with no such worktree on this machine — is surfaced as a **config/registration-class error** on the set: visible in `status`/dashboard, the set is not drained, no crash-backoff churn, no silent in-place fallback. The daemon never blocks and never surprises (ADR-0052's invariant holds).

## Considered options

- **Repo-level flag again (`worktree_ready` reborn).** Rejected — that's the capability-vs-intent confusion 0052 removed; it steers *every* set, not the one that needs isolation.
- **Name an existing worktree by path in the manifest.** Rejected — the manifest is branch-riding and shared across clones/machines; a path is machine-specific, which ADR-0035 forbids in branch-riding files. A worktree *name* (the operator-facing label) resolves per machine; its absence is an accepted, explicit failure.
- **Eager provisioning at registration.** Rejected — registration fires during refresh/status (read-ish ops); running `git worktree add` as a side effect of *looking* at a project is exactly 0052's surprise-provisioning. Lazy keeps reads side-effect-free.
- **Treat an unsatisfiable directive as a failed drain (crash-backoff/park), or as silent in-place fallback.** Rejected — it is a static env defect, not a runtime crash; backoff churns a set that can never succeed, and silent fallback reintroduces 0052's hidden routing.
- **Group registration seeds under a new `auto_register` key.** Rejected — migrating `auto_drain` under it is breaking churn for zero behaviour change; the seed category is documented in the glossary instead.

## Consequences

- New persisted field on `RegisteredTaskSet` carrying the seeded worktree-intent, seeded in `registeredTaskSetFromManifest` alongside `auto_drain`.
- `RouteDrainCheckout` gains a step between the runtime-path override and the current checkout that consults the registration intent and provisions/adopts on the first unbound drain.
- Manifest validation must reject a malformed `worktree` value (both arms set, unknown key, wrong types) as a contract fault → Malformed.
- Glossary: adds **Worktree directive** and **Registration seed**; redefines **Drain routing** and **Task manifest**.
