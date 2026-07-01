---
fragment: 26927c9e
generation: 0007
branch: master
---

+ SetRef
  The resolved, fork-free coordinates of one registered Task set that the Queue
  write-path acts on: its definition/state paths, repository identity (repo key
  and common dir), project and runtime paths, plus the per-build derived facts
  (parked, bound, orphaned, auto-drain, raw status). Carried, never re-resolved,
  so acting on a set forks no git (ADR-0060). Embedded in the dashboard row and
  passed as the sole input to the Drain-control verbs, which lets those verbs run
  against a named set without a TUI row.
  avoid: DashboardRow (the presentation row that embeds a SetRef and adds display
  labels), Drain target (the destination a drain lands on, not the set it acts
  on), ResolveInput (the CWD-based address that re-resolves coordinates)
  under: Queue

+ Drain control
  The Queue write-path module (queue/draincontrol.go): the set of mutation verbs
  the dashboard reaches to launch a Drain, bind/adopt/provision a Worktree, unpark
  a set, and preview — LaunchDrain, CreateWorktree, AdoptWorktree,
  ProvisionManagedWorktree, UnparkSet, PreviewDrain, and peers. Keyed on SetRef,
  not on the dashboard row, so the same verbs are callable from `pop queue`
  commands, not only the TUI. Lifted out of the dashboard model/view file so the
  write-path's locality is one module.
  avoid: dashboard actions, DashboardRow callbacks (the verbs no longer take a
  view type)
  under: Queue
