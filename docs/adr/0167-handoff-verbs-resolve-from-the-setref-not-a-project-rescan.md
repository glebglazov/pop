---
status: accepted
---

# Handoff verbs resolve from the SetRef, not a project rescan

Every **Handoff verb** — drain, verify, assist, fold, shell, wayfinder — entered
through `dashboardScansForDefinition`, which listed every configured project and
ran one git invocation per project, serially, to find the one that owns the row's
definition path. On a config globbing `~/Dev/*/*` that is 55 projects: a measured
**2.694s** out of a 2.795s handoff, with the remaining six steps costing 82ms
combined. Because a handoff quits the dashboard, nothing renders during that
window — the operator sees a key that does nothing, waits, gives up, and reports
the binding as broken. That is how this was found.

The **SetRef** glossary entry already stated the invariant this violated:
"Carried, never re-resolved, so acting on a set forks no git (ADR-0060)." The
fan-out was re-deriving facts the row was already carrying — project path,
runtime path, repo key, common dir, definition path — plus a session name that is
a pure function of the project path.

**Decision.** A verb addressed by a SetRef builds its scan from that SetRef and
performs no **Project scan fan-out**. The fan-out survives in exactly one place:
`resolveRepresentative` for an *unbound* set, where several checkouts of the same
repository genuinely must be compared to pick the trunk. There it runs
concurrently, with the same goroutine shape `expandConfiguredPaths` already uses
one frame above it. Separately, a handoff reports that it is handing off the
moment it dispatches, rather than relying on being fast enough not to need to.

## Considered options

- **Parallelize the fan-out and keep it everywhere.** Would have cut the wall
  clock to roughly one git invocation, and required no thought about which facts
  the row owns. Rejected as treating the symptom: 55 git processes to select a
  path already in hand is wrong at one process per project too, and it would have
  left the SetRef invariant quietly false.
- **Cache the resolved scans for the dashboard's lifetime.** Cheap, but it makes
  the *cache* the authority on a fact the snapshot already owns, and it does
  nothing for the non-TUI callers of the Drain control verbs.

## Consequences

The fan-out incidentally verified that the row's definition path still belonged
to a registered project, and dropping it drops that check on the SetRef path. The
guard is not lost: a stale set fails at the point of use — a bound worktree that
no longer validates, or a checkout that no longer exists — with an error naming
the set, rather than silently disappearing behind "no longer in a registered
queue project" 2.7 seconds later.
