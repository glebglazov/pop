---
status: accepted
---

# Session templates are explicit weighted split trees applied on demand

## Context

pop creates flat sessions: `tmux new-session -ds <name> -c <dir>` gives one window,
one pane, no arrangement. Users want their sessions to come up *shaped* — named
windows, panes running specific commands, a deliberate split layout — declared once
in config and reused. This ADR records the model for that feature: the **Session
template**.

## Decision

A **Session template** is a named blueprint for the shape of a tmux **Session**. It
is config-only and inert until applied.

- **Naming.** The whole-session blueprint is a *Session template*. The per-window
  pane geometry keeps tmux's own word, **layout** (`main-vertical`, `tiled`). A
  template's leaf node is a **Pane spec** (name/command/cwd/weight) — deliberately
  distinct from the glossary's **Pane** (the live, tracked tmux pane with an id and
  attention status that a spec *produces* on apply).

- **Where templates live, and resolution.** Templates are defined in three homes:
  the global `config.toml` `[[session_templates]]` library, a repo's `.pop.toml`,
  and a global `[repo."<path>"]` block. For a given checkout the visible set is the
  **union by name**, resolved **most-specific-wins**: `[repo]` override > `.pop.toml`
  > global library; a name collision warns. Because `.pop.toml` already resolves from
  **Repository identity** (the bare repo root for pop-style bare repos), a bare repo's
  templates propagate to **all its worktrees for free** — no Trunk-specific mechanism
  is built. (Caveat: non-bare linked worktrees each have their own identity, so this
  propagation is a bare-repo property.)

- **Geometry is an explicit weighted split tree, not a tmux preset.** A window is the
  root container; each node is either a Pane spec (leaf) or an unnamed split container
  (has nested `panes`). A container has `direction = row | column` and weighted
  children; `weight` is a relative integer **normalized within its siblings**, default
  `1`. Recursion is **unbounded**. We use `row`/`column` (flexbox) rather than tmux's
  `horizontal`/`vertical`, which are the most confusing words in tmux (`split -h` draws
  a *vertical* divider); the mapping to `-h`/`-v` is internal. tmux splits are binary
  and size by cells, so an N-ary weighted container is realized as a sequence of
  `split-window` + `resize-pane`, honoring weights to the nearest cell.

- **Per-pane attributes.** Every pane defaults its cwd to the **session directory**;
  an optional `cwd` is resolved relative to it (with `~`/absolute allowed) and inherits
  down containers. After build, the **first window** is active and its first leaf pane
  focused, overridable by a single `focus = true` Pane spec. Window `name` is
  **required and unique** within a template; a missing/duplicate name is a non-fatal
  load finding (per ADR 0054), degrading that one template rather than aborting load.

- **Apply is a manual, non-destructive verb.** `pop template apply <name>` (plus
  `pop template list`) instantiates a template into the current session (`-t` to
  override). It is **additive + skip-by-name**: each template window is created fresh,
  but a window whose name already exists in the target is skipped (warn). It **never**
  destroys existing windows or panes. There is no `pop tmux` namespace — tmux is the
  substrate under every command, and the feature lives under a domain verb like the
  rest of pop's surface.

- **Healing is descoped.** Reconstructing a partially-torn-down template window is out
  of scope; its natural future home is on-demand session *startup*, not general
  re-apply. Deterministic healing of a nested window would mean rebuilding the whole
  window anyway (tmux split trees are creation-order-bound and not reconstructable from
  surviving panes), so it is deferred rather than half-built.

## Considered options

- **tmux preset layouts** (`even-horizontal`, `main-vertical`, `tiled`) instead of an
  explicit tree. Rejected — no control over sizes or pane position beyond fill-order;
  weights give proportional control the presets can't express.
- **Wipe + rebuild on apply.** Rejected — a manual command that kills the shell you're
  sitting in and any running work is hostile. Additive + skip-by-name is safe and
  idempotent enough.
- **A `pop tmux` command namespace** gathering raw tmux operations. Rejected — pop is
  organized by domain noun (`project`, `worktree`, `pane`, `task`), not by the
  underlying tool; a tmux grab-bag cuts across every domain and leaks the substrate.
- **A hard 2-level nesting cap.** Rejected — unbounded recursion costs nothing extra in
  the model, and TOML key-path verbosity past depth 2 is its own natural disincentive.
- **Stretching `Pane` over both declared and live.** Rejected — disjoint fields (a spec
  has command/weight and no id/status; a Pane has id/status and no birth command) make
  the conflation leak meaningless fields onto each side.
