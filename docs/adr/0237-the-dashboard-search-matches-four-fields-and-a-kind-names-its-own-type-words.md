---
status: accepted
relates: "widens the search [ADR-0213](0213-a-text-entry-mode-may-reserve-only-keys-that-produce-no-text.md) established, and keeps status vocabulary on the Work view preset side of ADR-0197 / [ADR-0229](0229-an-unanswered-filter-field-never-removes-a-row.md)"
---

# The dashboard search matches four fields and a kind names its own type words

## Context

The **Work dashboard search** matched two fields: a row's `Project` and its `ID`,
case-insensitively, by substring (`dashboard/dashboard.go:2493-2506`). Two rows the
operator can see and name were unreachable by it — a row's **Work kind**, and the worktree
in the WORKTREE column.

The kind is the sharper gap. Page A holds Task sets and Maps interleaved, and there is no
way to ask for one of them by typing. The obvious fix — match `ref.Kind`'s id — makes the
question `map`, because the enum's members are `task-set`, `map` and `routine`
(`work/ref/ref.go:20-26`). But the word on the screen is `WAYFINDING`: it is the Map's one
STATUS cell label (`wayfinder/workkind.go:675`), chosen because what a reader needs from a
Map row is how much thinking is left. So the word the operator reads and the word the enum
holds are different words, and a search that only knows the enum fails the person typing
what they see.

## Decision

**The search matches four fields, and a kind supplies the words it answers to.**

1. **Four fields, OR-ed: ID, Project, type words, worktree.** Worktree means both
   `Container.Worktree` (the destination label) and `Container.Checkout` (the directory),
   so a fragment of what is on screen and a fragment of the path both find the row.
   `RuntimePath` adds nothing over `Checkout` for the rows that have it.

2. **Status text is deliberately excluded.** Adding the status cell would have made "way"
   find Maps for free — it is where `WAYFINDING` actually lives. It was rejected because
   status is **Work view preset** vocabulary (ADR-0197 decision 1, ADR-0229): putting the
   same question in two grammars gives the operator two answers that can disagree, and `/`
   would quietly become a worse `f`.

3. **A new `Kind.TypeWords` seam.** A Task set answers to `task-set`, `set`, `tasks`; a
   Map to `map`, `wayfinder`, `wayfinding`; a Routine to `routine`. `wayfinding` is in the
   list precisely because it is the word on screen — the vocabulary follows the render,
   not the enum. The seam lives on the kind for the reason every other one does: a list of
   words maintained beside the kind that renders them cannot drift from what is displayed,
   and a central table would be a second place to forget.

4. **A Task set has no type word on screen, and that is accepted.** `set` and `tasks` are
   invisible vocabulary, discoverable only through `C-h`. A Task set is the default kind
   of page A — it is the crowd, not the thing being picked out of it.

5. **Terms are AND-ed, fields are OR-ed.** The query splits on whitespace; each term must
   match at least one field, and different terms may match different fields. `way pop` is
   "Maps in project pop"; `way set` is correctly empty. OR-ing the terms was considered and
   rejected: it can only widen, so a second word would return more rows than the first
   alone — the opposite of what typing a second word means.

6. **Substring, not fuzzy.** Subsequence matching across four fields is very loose — a
   two-letter query would match nearly every row through a path — and the ranking that
   makes fuzzy usable in a picker cannot exist here, because ADR-0232 gives the preset's
   sort precedence over everything. Fuzzy without ranking is noise.

7. **Narrowing, not highlighting; and the empty-state line is unchanged.** `/` keeps
   subtracting rows. Marking matches in place would be a third row-selection concept
   beside the preset and the **Selection**, which ADR-0224 worked to keep to two.
   `emptySearchLine` still names the whole query and the way out; per-term diagnostics is
   a line that grows with the query for a question nobody asked.

## Consequences

- `work.Kind` gains `TypeWords`, pinned by `work/conformance_test.go` across all three
  kinds.
- A Map's status label is now load-bearing in a second place: renaming `WAYFINDING` means
  updating the Map's type words with it, or the search stops matching what the table says.
