---
status: accepted
supersedes: [ADR-0033]
---

# Task-set panes live in their bound checkout's session

Every pane pop opens for a Task set — drain, verify, assist, fold, runtime shell, and
the Work daemon's unattended auto-drain — lands in the tmux session of the **checkout
the set is bound to**, not in the originating project's session. A set bound to the
Trunk worktree therefore targets the trunk session, which is no change; a set bound to
a Managed worktree gets that worktree's own session, created detached on first spawn.

## Context

ADR-0033 put every queue-spawned drain in a `pop-work` window inside the *originating
project* session, with the pane's cwd rewritten to the bound checkout. It did that to
stop the daemon manufacturing sessions the human never chose: a **Worktree set** was
declared "an ephemeral execution context, not a navigable Project peer".

That premise has expired. A Managed worktree is now long-lived and routinely navigated
— `ctrl+g` from the Work dashboard, the Worktree picker, `pop work` — and the Task set
bound to it is worked over days, not minutes. The split it produced is the actual
defect: the pane's *cwd* is the worktree while the pane's *session* is the trunk, so
the operator reading a set's drain sits in the trunk session, one `ctrl+g` away from
the session that checkout actually owns. Two sessions for one checkout, and the
correspondence between them lives only in the operator's head.

The proliferation ADR-0033 feared is real, and is paid for elsewhere in the same
effort: the project dashboard gains a nested display mode that renders a project's
non-trunk sessions as a second level under the project rather than as flat top-level
rows. Session-per-checkout stops being noise once the picker can fold it.

Session naming was already the same function everywhere — `project.SessionNameWith` —
differing only in which path each caller handed it. Routines already handed it the
bound directory (`routine.sessionAndDir`); Task sets handed it the project path.

## Decision

The rule is one sentence: **a checkout's session is the session named after that
checkout**, and a Task set's panes go to the session of the checkout it is bound to.

It gets one owner, `project.CheckoutSessionWith(d, path)`, in the package that already
owns session naming. Its callers:

- `tasks/drain`'s Task-set pane coords, feeding all five dashboard verbs, plus the
  daemon's spawn path, which re-derives after routing because routing may have just
  provisioned the checkout;
- `routine.sessionAndDir`, which keeps its non-git `RoutinesSessionName` fallback as a
  wrapper over the shared rule;
- the `ctrl+g` / worktree-picker open path, whose `checkoutSessionName` already
  reported the naming diagnosis to the operator.

Consequences of the rule, decided with it:

- **A missing or unreadable bound worktree refuses the verb.** No fall back to trunk.
  Falling back would silently reintroduce exactly the trunk-locality this ADR removes,
  at the moment the operator is least able to notice. The bound-checkout validation
  that guarded only drain now guards all five verbs.
- **`ctrl+g` stays a separate verb.** It navigates a *human* to a checkout with
  birth-time Workbench shaping (ADR-0075/0078); a handoff verb spawns a pane and
  focuses it. They now agree on the destination session, which is the whole win;
  merging them would drag Workbench prompting into the unattended daemon.
- **A handoff-born session is unshaped.** `EnsureTaggedPane` creates the session
  detached, so a checkout first touched by a drain never receives its Preferred
  Workbench, and a later `ctrl+g` flat-attaches per ADR-0075 rather than reshaping.
- **The `pop-work` window survives unchanged.** One `pop-work` window per session,
  panes tiled, one pane per (set, verb), a running tagged pane focused rather than
  re-sent, the runtime shell always a fresh untagged pane.
- **tmux session names are untouched.** They stay `<project>/<worktree>`, derived from
  the worktree directory as always. Nothing is renamed and there is no migration.

## Considered options

- **Keep the trunk session and rely on cwd** (ADR-0033, the status quo). Rejected —
  it is the defect. Two sessions for one checkout, with the mapping between them
  unwritten.
- **A session per worktree set** — rejected by ADR-0033 as manufacturing unchosen
  sessions that look like Projects but aren't. Now *chosen*: managed worktrees are
  navigable Projects in every other surface, so a session per checkout is the honest
  model, and nested project-dashboard display absorbs the row count.
- **A window per set inside the trunk session.** Rejected — a tab per set was already
  rejected by ADR-0033 for readability, and it keeps the cwd/session split intact
  while adding a second axis of proliferation.
- **Fall back to the trunk session when the bound worktree is gone.** Rejected — a
  silent fallback is indistinguishable from correct behaviour until an agent has
  already run in the wrong checkout.
- **Fold `ctrl+g` and the handoff verbs into one primitive.** Rejected — the two
  differ in exactly the way that matters: one is human-attended and shapes a
  Workbench, the other runs under a daemon with nobody watching.
- **Shape a handoff-born session with the checkout's Preferred Workbench.** Rejected
  — running Workbench templates unattended is worse than a missing Workbench, and it
  would make `tasks/drain` depend on the workbench layer.
- **Derive the session name without forking git**, from the common dir a scan already
  holds. Rejected for now — it re-implements the naming rule in a second place to save
  one fork per dispatched drain. The daemon's fill stays lazy, so the idle full-fleet
  listing pays nothing.
- **Supersede ADR-0033 in part**, keeping its `pop-work` window claim alive.
  Rejected — the window claim is restated here, and half a live ADR leaves a future
  reader reconciling two documents for one behaviour.

## Consequences

- `project.CheckoutSessionWith` is the single derivation; a caller that hands it a
  project path instead of a bound checkout is now a bug with one name.
- The Task-set pane coords lose their `projectPath` resolution entirely, including the
  `resolveRepresentative` fan-out that existed only to find the project session. A git
  fan-out disappears from in front of a verb the operator is waiting on.
- The Work daemon creates a session per bound checkout it drains into, where it
  previously created at most one per project. Nested project-dashboard display is what
  keeps that readable.
- Tests that pinned the old invariant by name — "SessionName must not be derived from
  bound checkout" — invert.
- The glossary's **Work window** entry, which read "within a Project's session …
  instead of … per-worktree sessions" and listed "worktree session" among terms to
  avoid, inverts; so does **Supervision scope**'s closing clause about creating the
  project's session on demand.
- ADR-0033 becomes `status: superseded by ADR-0180`, whole.
