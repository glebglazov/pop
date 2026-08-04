---
status: accepted
relates: "keeps the fork-free, store-free picker walk of [ADR-0110](0110-managed-worktrees-surface-in-the-project-picker-via-a-filesystem-walk.md), and frees the `◆` glyph [ADR-0138](0138-project-routines-are-committed-prompts-discovered-live-from-pop-routines.md) also uses, without amending it"
---

# The project dashboard nests a project's sessions under one colourless glyph column

## Context

`pop project dashboard` is a flat list. Its rows are configured projects, every
pop-managed worktree found by a filesystem walk (ADR-0110), and every worktree of
a bare repo — each rendered under a full `<project>/<worktree>` name, sorted by
one unified recency. At a dozen projects with live worktree sessions the list is
mostly prefixes: the same project name repeated down the column, with the part
that distinguishes the rows pushed to the right.

Each row also carries two glyph columns: an icon saying whether a session is
live (`iconDirSession`, `iconAttention`) and a marker saying which **Work kind**
that session hosts — `▲` Task set, `●` Routine, `◆` Map (`cmd/session.go:23-53`).
The Map marker collided with `projectRoutineBadge = "◆"` on the Work dashboard
(ADR-0138), where the same glyph means a Project routine.

A live prototype (map ticket 04, branch `proto/nested-project-dashboard`) put
both problems in front of a human: expand/collapse against the flat list at 3
and 15 projects, with the icon model, the disclosure signal and the glyph
vocabulary each on a live toggle.

## Decision

**Group a project's non-trunk live sessions as a second level under the project
row, opt-in, and render every row through a single glyph column.**

1. **Nesting is display-only and opt-in.** `[project] worktree_display = "flat" |
   "nested"`, default `"flat"` — permanently, not as a migration step. Read once
   where the picker is constructed; a change takes effect on the next
   invocation. No tmux session is renamed and no path changes; a nested row is
   the same row with its `<project>/` prefix left unrendered.

2. **One glyph column, no colour carrying meaning** (nested mode's vocabulary;
   the column itself is mode-independent — see the amendment). `■` for any live session,
   `◇` for a **Map session**, nothing for a row with no session. The Task-set and
   Routine markers (`▲`, `●`) are not rendered on this surface at all. Which kind
   of Work a session hosts is the Work dashboard's question; this list answers
   "is there a session here", plus the one distinction worth making — a Map
   session is not a checkout you sit in.

3. **`▸`/`▾` trails a project holding nested sessions.** A colourless
   "there is more here" signal, in nested mode only. No accent border, no
   folded-glyph summary of the hidden children, no count.

4. **The two modes list different rows, deliberately.** Flat shows every
   worktree, session or not — today's **row set**, unchanged (its glyphs are
   fused too; see the amendment). Nested shows only
   worktrees with a live session: a session-less worktree is not something you
   attach to from here, and it stays reachable by typing a query, and in
   `pop worktree dashboard`, which is the checkout list.

   The nested level's membership rule is therefore **this project's non-trunk
   live sessions**, not "its worktrees". A **Map session** is a member without
   amending the rule (point 7); the level answers "what can I attach to under
   this project", and a checkout is not the only answer.

   **A bare repository synthesizes a parent row** from its display name, because
   its worktrees *are* its top-level rows today and it has no row of its own. The
   synthesized row names no checkout, so every action that opens, kills or records
   a row skips it — a half-nested list is the worse artifact, but a grouping
   header must not pretend to be a project.

5. **Typing flattens.** With a query, the picker matches the whole universe —
   every project and every worktree, cold ones included, at depth 0 under full
   prefixed names — so a query can match the prefix. Arrows are picker-level
   expand/collapse only while the query is empty; with a query typed they are
   the textfield's cursor keys, unchanged.

6. **A project sinks to its most recent worktree's recency**, and children sort
   inside their group by the same comparator the top level uses. Collapsing from
   a child lands the cursor on the parent; a second `right` on an expanded row
   walks into its first child. Quick-access digits number the visible rows, so
   they shift when a group opens. Expansion state lives in the process: it
   survives the picker's in-loop reopens and nothing is written to disk.

7. **A Map session nests under its project, attributed from tmux's
   `#{session_path}`.** A **Map session** is rooted at the **Trunk worktree** of
   exactly one repository, so it belongs to a project as firmly as a worktree
   does — and in the prototype it was the one long unattributed row in an
   otherwise grouped list.

   - **Attribution.** `WorkSessions()` already runs one `list-sessions -F` with
     the Work stamp; `#{session_path}` is appended to that same format string, so
     the fact rides along for zero extra process spawns and zero filesystem I/O.
     The path is matched to a project **group**, never to an individual row.
   - **Naming.** `<glyph> pop/<map-id>` in flat mode, `<glyph> <map-id>` nested.
     The `pop-map-` prefix is dropped — `◇` already says "map" — and the map id
     is rendered **whole, date included**: it is the string typed into every
     `pop map` verb, and two maps can share a slug.
   - **Sorting.** Among the worktrees, by its own recency, same comparator, no
     pin. A project sinks to its most recent child whether that child is a
     worktree or a Map, so grilling a map an hour ago pulls its project down.
     The recency itself comes from session activity rather than History (a Map
     session has no directory to have visited), which is the existing
     standalone-session fallback and must survive the row ceasing to be
     classified as standalone.
   - **Bare repos: depth 1, always.** A bare repo's Trunk *is* one of the
     worktree rows, so an exact-row match would put the Map at depth 3. It sits
     alongside the worktrees instead. For a non-bare repo the Trunk path equals
     the project path, so the group match is exact and the rule costs nothing.
   - **Fallback.** A path that resolves to no configured project leaves the Map
     a top-level row, rendered `<glyph> <map-id>` with no prefix. No synthesized
     parent — point 4 synthesizes one for a *bare* repo, where the project is
     configured and only its row shape differs; inventing one for an untracked
     repository would produce a parent row whose `Enter` opens the Trunk session
     of a project pop does not track.

Scope is project rows, worktree rows and Map rows. **Maps are the only Work kind
this needs to reach:** `StampWorkSession` has one caller today
(`wayfinder/session.go:85`), and after
[ADR-0180](0180-task-set-panes-live-in-their-bound-checkouts-session.md) a
Task set's panes live in its bound checkout's session — already a project or
worktree row. A Routine that later spawns a checkout-less session of its own
inherits this rule rather than reopening it.

## Amendment (2026-08-04): the single glyph column is mode-independent

The original decision fused the two columns as part of nested mode, and read
"flat is unchanged" as covering its glyphs as well as its rows. That left the
`◆`/`◇` collision fixed in one mode only: in flat a live Map session still
rendered `□ ◆`, two glyphs for one fact, and every row of the list paid a
permanent marker gutter to say it.

**Both modes render one glyph column.** The precedence is shared — unread output,
then Work kind, then session presence — and the kind glyph *replaces* the
live-session glyph rather than sitting beside it, since a row cannot host Work
without hosting a session. A row with no session is blank in both.

**The vocabularies stay different, and that is the point.** Flat is the whole
inventory and loses no distinction it had: a Map, a Task set, a Routine, a bare
standalone session and a live checkout each keep their own glyph in the one
column. Nested's vocabulary — `■` for any live session, `◇` for a Map — was
settled by a human against a live prototype and does not move. **A Map is the
hollow diamond in both**, so `◆` is off this surface entirely and stays ADR-0138's.

Point 4's "unchanged" is accordingly narrowed to the **row set**: flat still
lists exactly the rows it listed, in the same order, under full prefixed names,
with no grouping, no indentation and no disclosure triangle.

## Considered options

- **Keep the flat prefixed list.** Rejected at the prototype: nesting read
  better at 15 projects *and* at 3, and the human's reaction was unqualified.
- **Keep the two glyph columns, fixing only the `◆` collision.** Rejected: with
  the columns side by side the pair read as noise, and the kind distinction they
  encoded is the Work dashboard's job. Fusing them removes the collision as a
  by-product rather than renaming around it.
- **An accent-coloured or bordered project icon** to mean "this project has
  worktree sessions" (the shape charted before the prototype). Rejected: once
  session-less worktrees are hidden from the nested level, the disclosure
  triangle already carries that signal, and the border dissolved against a
  themed terminal. `▣` failed the same way.
- **Hide cold worktrees in both modes**, keeping the two modes' row sets
  identical so the toggle is arrangement-only. Rejected: that silently removes
  checkouts from a list where they are visible today. Flat mode is the honest
  full list; nested trades completeness for grouping, and says so.
- **Filter within groups, auto-expanding matches.** Rejected: with cold
  worktrees off the nested level, flattening on a query is what keeps every
  checkout reachable, and a query already carries its own structure.
- **Persist expansion across invocations** in history or `pop.db`. Rejected as
  state earned nothing asked for it; a fresh dashboard opening collapsed is
  predictable.
- **Attribute a Map by resolving its id back through the Work store** — scan
  `repos/*/`, read each `repo.json`'s `repository_path`, derive the working-tree
  root. Rejected on speed, which was the stated constraint: it costs a directory
  read plus a JSON read per repository on the dashboard's hot path, where
  `#{session_path}` costs nothing at all. It is the more *correct* derivation —
  the stamp is durable and the store is authoritative — and it remains the
  fallback to reach for if the cheap one proves too lossy in practice.
- **Keep the Map session top-level** (the shape charted before ticket 04).
  Rejected: it survives only as the unattributable-Map fallback. Grouped
  neighbours made the single un-prefixed full-length row conspicuous.
- **Pin the Map row to the top or bottom of its group.** Rejected: a second
  ordering rule owned by one row type, against no complaint about recency.
- **Nest the Map under its Trunk worktree's row** in a bare repo, matching the
  session path to an exact row. Rejected: it invents a third level for one row
  type, which is worse than the flat list this replaces.

## Consequences

- Two rendering paths behind one config key. `ui.Item` grows a display depth and
  a disclosure glyph, and the picker grows a caller-supplied tree seam — the
  gestures live in the picker while the rows stay `cmd`'s, so grouping and
  flattening are pure functions over a slice rather than a widget owning a
  hierarchy.
- **`◆` stops meaning "Map session"**, which clears its cross-surface collision
  with ADR-0138's `projectRoutineBadge`. ADR-0138 is not amended — it said
  nothing false; the collision was on this surface's side.
- The glossary term **Work session** no longer holds: `pop project dashboard`
  does not badge sessions by kind. Redefined in the glossary.
- Flat mode keeps the `<project>/` prefix, so ADR-0110's managed-worktree rows
  are untouched under the default. Nothing changes for a user who never sets the
  key.
- `pop worktree dashboard` becomes the only place a session-less worktree is
  listed in nested mode. Acceptable because a scratch worktree is born with a
  session attached (`ctrl+t` opens one immediately), so cold worktrees are the
  exception.
- Quick-access digits are no longer stable while browsing: opening a group
  renumbers the rows below it. This is the cost of numbering what is visible;
  the alternative left an expanded child unreachable by hotkey.
- **A Map's project attribution is derived from tmux's mutable start directory.**
  `#{session_path}` can drift — `attach -c` rewrites it — so an attribution can
  go stale. A drifted Map degrades to a top-level row rather than erroring or
  landing under the wrong project, because the fallback for "cannot attribute"
  already exists. Written down here so a future reader finds the trade rather
  than reading the fallback as a bug.
- `WorkSession` grows a directory field, and the project dashboard's grouping
  reads it. No new tmux call, no new store read.
- The glossary term coined for the grouping is **Session nesting**, not
  "Worktree nesting": the level holds a non-worktree member, so a term naming
  the membership rule after worktrees would be wrong on arrival. The config key
  keeps its `worktree_display` name deliberately — renaming a key to match a
  glossary term is churn.
