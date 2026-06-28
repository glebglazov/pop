---
status: accepted
---

# Worktree directive is Queue-only; foreground implement binds the current checkout

> **Relates:** amends [ADR-0059](0059-task-set-may-declare-a-worktree-directive.md) (directive now Queue-only, no longer "honoured by every drain"), [ADR-0046](0046-implement-defaults-to-worktree-execution.md) (the `--in-worktree` opt-in keeps its name but re-bases its fork point), and [ADR-0062](0062-no-directive-drain-persists-a-default-binding.md) (Queue no longer persists a default binding). Depends on [ADR-0070](0070-worktree-set-integration-is-removed-merging-is-the-humans-own-concern.md) (no Integration-target fallback).

Foreground and Queue drains now route differently, on purpose.

**Foreground `pop tasks implement`** always runs in the **current checkout**. It ignores the set's Worktree directive entirely. A live Runtime execution lock elsewhere refuses; otherwise it (re)binds the set to the current checkout and drains there. Rebinding off an *idle* managed binding prompts to delete that managed worktree (the only foreground teardown point); rebinding off an adopted or trunk binding silently re-points. The explicit isolation opt-in keeps its name, **`--in-worktree`**, but re-bases: it now provisions a managed worktree forked from the **current checkout's HEAD** (previously trunk), binds the set to it, and drains there.

**The Queue** honours bindings and directives only. Precedence: an existing binding wins; else a `managed: true` directive provisions a managed worktree forked from the **Trunk worktree**, or a `{ name }` directive adopts that named worktree; else the set is **not drainable** and surfaces as a needs-bind fault. The Queue never invents a checkout and records no default binding.

## Why

The directive being "honoured by every drain" meant a `managed: true` set could never be drained quickly in the checkout you were already sitting in — foreground would silently spin up an isolated worktree you didn't ask for. Splitting by trigger matches intent: when *you* type `implement`, "here, now" is what you mean, and isolation should be an explicit `--in-worktree`; when the *Queue* drains unattended across repos, it has no "here", so an authored directive is the only sane source of a checkout. Re-basing `--in-worktree` to fork from the current HEAD rather than trunk lets a foreground experiment branch off exactly the state in front of you; the Queue, having no current, keeps forking from trunk.

## Consequences

- The same set can drain from different bases depending on trigger (current HEAD foreground, trunk in the Queue). This asymmetry is intended, not a bug.
- An unbound `auto_drain` set with no directive is not Queue-drainable; the dashboard shows it as `needs bind` and the human binds via `b`. Auto-drain and binding stay orthogonal, but auto-drain without a binding-or-directive simply doesn't run.
- `--in-worktree` keeps its name, so existing scripts and muscle memory are unaffected — but its fork base changes from trunk to the current checkout's HEAD, which alters what a foreground isolated drain branches off.
