---
status: accepted
relates: "amends the one-unified-recency statement of [ADR-0185](0185-project-dashboard-nests-worktree-sessions-under-one-glyph-column.md) with an opt-in second ordering; leaves [ADR-0188](0188-history-records-human-landings-and-moves-into-the-execution-state-store.md)'s recency untouched inside each tier; follows the tier-then-recency shape of [ADR-0220](0220-artifacts-are-ordered-by-type-tier-not-by-recency.md)"
---

# Live-first ordering tiers attachable rows next to the prompt

## Context

The project dashboard sorts every row — configured projects, walked worktrees,
standalone sessions — along one unified recency timeline, oldest first, with the
tail of the slice rendered at the foot of the list where the prompt, the cursor's
starting row and the **Quick selection** digits are. Recency does not care whether
a row has anything to attach to, so a checkout visited an hour ago and left
session-less outranks a live session from this morning. At a dozen rows the hot
positions by the prompt are gappy: the digits land on a mix of attachable
sessions and dead checkouts, and the rows a user actually returns to all day —
the live ones — are interleaved upward through the list.

## Decision

**`[project] session_ordering` is a two-word vocabulary — `"unified"`, the
permanent default and today's behavior, and `"live-first"`, which tiers the rows
carrying a live-session glyph after everything else in the slice, next to the
prompt — with recency ordering untouched within each tier.** A value outside the
vocabulary is collected as a finding per
[ADR-0054](0054-config-validation-is-caller-scoped.md): the dashboard still
opens, on the unified timeline, and the warning banner names the rejected value.
The deprecated `[select]` table is honored like every other project key.

Liveness is `rowHasLiveSession` — the glyph the row already carries when the
sort runs — not a second lookup against the session-activity map. The tier and
**Session nesting** therefore read the same fact and cannot disagree: a row
wearing the attention glyph tiers live even when the activity map has no entry
for its name.

Under `worktree_display = "nested"` the tier survives nesting by composition,
not by a special case: a node ranks at its most recent folded-in row (ADR-0185,
"a project sinks to its most recent child"), so a session-less project whose
worktree or Map sessions are live rides its children into the live tier while
the most recently visited session-less checkout stays in the dead tier above.
That composition is pinned by test, not assumed.

## Considered options

- **A bool (`live_sessions_first = true`).** Rejected: the next ordering idea
  forces a second bool or a breaking rename, and `worktree_display` already
  chose a validated vocabulary for exactly this shape of display preference.
- **Changing the default.** Rejected: row order is muscle memory, and an
  upgrade must not reshuffle anyone's picker. Unified is the permanent default
  the way flat is for **Session nesting** — live-first is a preference, not a
  migration.
- **Tiering by an activity-map lookup instead of the glyph.** Rejected: it is a
  second liveness predicate in the same pipeline, and the two can disagree — a
  stale attention row would tier dead while nesting folds it as a live child.
  One fact, read twice, beats two facts that usually agree.
- **A per-row-type pin.** ADR-0185 rejected pinning the Map row to an edge of
  its group as "a second ordering rule owned by one row type, against no
  complaint about recency". This decision is not that rejection reversed: the
  complaint about recency now exists (the gaps at the prompt end), and the rule
  is global over every row and opt-in, owned by the ordering itself rather than
  by one row type.

## Consequences

- **Amends ADR-0185's ordering statement.** "Sorted by one unified recency" is
  now the default rather than the invariant; everything else in that ADR —
  nesting, ranks, glyphs, the Map rules — stands untouched and is what makes
  the tier compose in nested mode.
- **ADR-0188 is untouched.** Within each tier the order is still human landings
  from History with the session-activity fallback for standalone rows; daemon
  activity remains invisible to recency.
- The ordering is read once per picker invocation, like `worktree_display`: a
  config change takes effect on the next invocation, never mid-session.
- Glossary: **Live-first ordering** is defined.
