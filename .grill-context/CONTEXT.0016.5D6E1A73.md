~ Map manifest
  The `index.json` beside a Map's `map.md` — the machine-readable half of a Map,
  mirroring the **Task manifest** so no consumer hand-parses metadata out of N
  ticket markdown files. Per Decision ticket it carries id, file, title, type,
  status (`open` | `resolved`; a claim is pop.db state, never a file state),
  `out_of_scope`, `blocked_by`, `adr_drafts` and `context_drafts`, plus a
  Map-level `spawned_sets` array defaulting to empty. Blocking edges live here
  because they are definitional and travel with the content. Where one exists it
  is the source of truth for status, type and blocking; a Map without one still
  reads its ticket markdown headers. Validation runs on **every read of the Map**,
  not in `pop map register` alone, so a problem introduced after charting is
  visible without anyone re-registering by hand, and it names every problem at
  once. It has two severities. **Errors** render the Map `BROKEN`: unknown status
  or type, a blocker naming no entry, an entry with no markdown file, a markdown
  file with no entry. **Warnings** are reported everywhere and refuse nothing: a
  file under `adrs/` or `context/` that no ticket's `adr_drafts`/`context_drafts`
  names — the reverse of the check `resolve` already runs on a declared draft, and
  the one that catches an artifact the handoff would otherwise drop, since a
  spawned set mints its checkboxes from those arrays. Advisory rather than
  blocking because a draft still being written is indistinguishable from one
  forgotten, and because the session that most often leaves one — **Map assist** —
  resolves nothing, so there is no write to withhold; auto-attaching orphans to
  the ticket being resolved was rejected on the same ground, a wrong attribution
  being worse than a reported orphan because it is invisible.
  was: The `index.json` beside a Map's `map.md` — the machine-readable half of a
  Map, mirroring the **Task manifest** so no consumer hand-parses metadata out of
  N ticket markdown files. Per Decision ticket it carries id, file, title, type,
  status (`open` | `resolved`; a claim is pop.db state, never a file state),
  `out_of_scope`, `blocked_by`, `adr_drafts` and `context_drafts`, plus a
  Map-level `spawned_sets` array defaulting to empty. Blocking edges live here
  because they are definitional and travel with the content. Where one exists it
  is the source of truth for status, type and blocking; a Map without one still
  reads its ticket markdown headers. Validation names every problem — unknown
  status or type, a blocker naming no entry, an entry with no markdown file, a
  markdown file with no entry — and a failing manifest renders the Map `BROKEN`.
  under: Wayfinder

<!-- Minted by the validator-hardening slice of the
2026-08-03-worktree-session-locality set. No ADR is owed: these are validator
rules, not a compatibility surface (map ticket 09's answer). The task-set half of
the same round — the orphan-markdown check — is already carried by
`~ Task-set manifest validation` in CONTEXT.0014. -->
