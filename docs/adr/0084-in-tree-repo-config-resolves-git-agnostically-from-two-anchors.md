---
status: accepted
relates: "builds on [0083](0083-per-project-config-is-a-committed-in-tree-base-plus-a-gitignored-personal-override.md); reuses the trunk resolver of [0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md)"
---

# In-tree repo config resolves git-agnostically from two anchors

## Context

With in-tree per-project config ([ADR-0083](0083-per-project-config-is-a-committed-in-tree-base-plus-a-gitignored-personal-override.md)),
a child worktree must see config authored in the main/**Trunk worktree** — the
original ask was "place `.pop.toml` in the main worktree, inherit it in
children." But `git worktree add` **never copies untracked/gitignored files**,
so a gitignored file in the trunk is simply *absent* in a fresh child;
inheritance cannot be a filesystem side effect. Separately, today `.pop.toml` is
read from the **Repository identity** root, which for a bare repo sits beside
`.bare` — not in the main worktree.

## Decision

pop resolves in-tree repo/worktree config **git-agnostically, by filesystem
path**, from two anchors:

- **this worktree's tree** → worktree scope
- **the Trunk worktree's tree** → repo scope, reusing ADR-0078's `Deps.Trunk`
  resolver, its no-trunk fallthrough, and its "this checkout *is* trunk → read
  once, don't double-warn" guard.

**Presence at an anchor decides the layer.** A file present only at the trunk
fills the repo-scope layer and propagates **dynamically** to un-overridden
children (re-point or edit trunk and they follow through), matching ADR-0078
runtime inheritance. pop **never** calls `git check-ignore` or inspects tracking
— committed vs gitignored determines only *how a file arrived*, never how pop
reads it. A worktree's own committed copy (a git fork-time copy) therefore reads
as a worktree-scope override of the trunk's repo-scope copy, which is intended.

The repo-scope anchor is the **trunk worktree's tree, falling back to the
identity root**, so existing bare-repo `.pop.toml` files (beside `.bare`) keep
working. If both exist, pop **warns** to prompt consolidation rather than
erroring.

## Considered options

- **Git-aware dual-mode** (committed → read local copy only; gitignored →
  trunk-walk). Rejected: it cannot deliver dynamic propagation for committed
  files anyway — every fork already holds a copy, so pop can't distinguish
  "unchanged inherited" from "deliberately set the same" — and it forces pop to
  probe git-tracking. One presence-based rule is simpler and lands the same
  behavior.
- **Trunk-worktree-only anchor** (no identity-root fallback). Rejected: silently
  breaks existing bare-repo users who keep `.pop.toml` beside `.bare`.
- **Identity-root-only** (today's behavior). Rejected: contradicts the "put it in
  the main worktree" driver and has no natural equivalent for non-bare linked
  worktrees, whose identity is their own path.
