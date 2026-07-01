---
status: accepted
---

# The Worktree picker owns interactive worktree creation

## Context

[ADR-0029](0029-pop-owns-git-worktree-add-for-worktree-sets.md) scoped pop's
ownership of `git worktree add` narrowly to **Worktree sets** (unattended
Queue-driven execution provisioning) and explicitly **rejected** "add creation to
the interactive Worktree picker generally," keeping the picker "a navigation and
deletion surface." Creation for humans lived in an out-of-band bash script
(`tmux-create-worktree`) that shelled out to git and handed the new path back via
`pop project switch`.

We now want pop to own the *whole* worktree lifecycle for humans, not just deletion
and Queue provisioning — so the brittle bash script can retire and the create flow
gets pop's testable deps/mocks, repo-context detection, and Workbench session
shaping for free.

## Decision

Interactive worktree creation is a first-class action of the **Worktree picker**,
bound to `ctrl+a` in `pop worktree dashboard`. The flow: pick a branch (local +
remote, main/master first) → name the worktree (hand-rolled prompt, default =
branch with `/`→`-`) → `git worktree add` (reuse-existing-branch vs. `-b` new
branch; bare vs. non-bare path derivation — the retired script's logic, ported) →
open the session and **attach immediately**, shaped by a **Workbench** when
`[workbench] pick_on_create` is set (reusing the existing picker create-path,
[ADR-0075](0075-workbench-apply-reconciles-by-pane-identity.md)) else a flat
session.

This **reverses** ADR-0029's rejected option and its "navigation and deletion
only" boundary. Queue **Worktree set** parallelism is no longer the sole place pop
owns `git worktree add`. ADR-0029's trust model for the *unattended* daemon
(declarative `worktree_ready`, no repo-sourced shell) is untouched — this is an
*attended*, human-driven surface, so that boundary does not apply.

## Considered options

- **A `pop project switch --workbench <name>` flag; script keeps orchestrating.**
  An earlier cut of this design: the bash script fzf-picks the branch and workbench
  and hands the name to a non-interactive `switch`. Rejected once the goal became
  "pop owns the lifecycle" — the native flow reuses `promptWorkbenchForCreate` and
  `createSessionFromWorkbench` directly, leaving the flag with no caller. Never
  built; nothing to supersede.
- **Add `bubbles/textinput` for the name prompt.** Rejected — pop imports zero
  `charmbracelet/bubbles` components and hand-rolls all TUI on raw bubbletea; the
  prompt follows house style rather than introducing the dependency.
- **A standalone `pop worktree create` subcommand.** Deferred — the dashboard is the
  worktree hub; a tmux key opens it and `ctrl+a` creates. A bindable subcommand can
  be added later if one-keypress creation is missed.

## Consequences

- The glossary's **Worktree picker** definition is updated: creation is in scope;
  the navigation-and-deletion-only wording is gone. `pop project switch` remains a
  valid external hand-back seam for *other* tools.
- The `tmux-create-worktree` bash script is retired; its users rebind their tmux key
  to open `pop worktree dashboard`.
