# Worktree-set integration is human or agent attended

Status: accepted

ADR-0028 also deferred the merge-reconciliation boundary: Pop may compute mergeability for
completed Worktree sets, but it must not silently integrate them. That boundary now stands as the
implemented rule. Pop uses git to classify a set branch as clean or conflicting, then leaves
semantic integration to the human and, for conflicts, to an attended agent session.

## Why

A clean git merge is only textual evidence. Two Worktree sets can merge without conflicts and
still break the working branch semantically. Pop therefore treats **Mergeability** as routing
information, not as permission to mutate trunk.

For conflicts, Pop also cannot be the resolver. ADR-0024 says Pop makes zero model calls; semantic
conflict resolution requires judgment and code understanding. ADR-0012 established the attended
agent boundary for human-in-the-loop work: Pop may prepare context and launch assistance, while
the human remains present for the decision. Worktree-set conflicts use that same shape.

## Decision

Pop never auto-integrates a Worktree set merely because it completed. It computes mergeability
with a no-side-effect `git merge-tree` dry run against the working branch. Clean sets can be
integrated only by an explicit human-triggered command, with `auto_merge_clean` reserved for
operators who opt into that semantic risk. Conflicting sets are kept for inspection and routed to
attended agent assistance; Pop does not invent conflict resolutions itself.

## Considered options

- **Auto-merge every clean set.** Rejected — textual cleanliness is not semantic safety.
- **Have Pop resolve conflicts directly.** Rejected — it would require model calls or a brittle
  heuristic resolver, both outside Pop's role.
- **Treat conflicts as terminal failures.** Rejected — the worktree branch contains useful agent
  work, and attended assistance is the existing boundary for human-guided resolution.

## Consequences

- Queue status may report mergeability as clean, conflicts, or unknown, but those labels are not
  integration outcomes.
- Worktrees and branches for sets awaiting integration are retained so the human or attended
  agent can inspect the exact result.
- ADR-0028's no-unattended-integration framing remains the source design; this ADR records the
  accepted implementation boundary and ties it to ADR-0024 and ADR-0012.
