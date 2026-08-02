# Wayfinding decisions mint into the repo through the slices that implement them

## Context

[ADR-0136](0136-planning-skills-publish-through-a-work-store-seam.md) put maps in
pop's per-machine Work store, and this map's ticket 02 ruled that **wayfinding
writes nothing into the repo**: resolutions live in ticket answers, ADRs are
planned rather than written, and `to-spec` mints the ADR files at handoff.

Nothing enforces that today, and the enforcement gap is not theoretical. The
wayfinder overlay's charting delta and the Work-store doc's grilling ticket-type
override both name `grill-with-docs`, a skill whose contract *mandates*
`.grill-context` fragment writes and a commit at session close. A grilling ticket
therefore loads a skill that instructs it to do the one thing wayfinding forbids.
Ticket 05's session did exactly that and had to be reverted by hand.

Ticket 02 §7 also contains a contradiction that only surfaces under the
task-set flow: it says `to-spec` mints the ADR files *and* that "ADRs land
through the normal set flow, so spawned sets never miss the decisions they came
from". Those cannot both hold. Task sets fork **trunk HEAD**, and the Work-store
doc is explicit that publishing does not commit — "artifacts must already be
committed". An ADR minted by `to-spec` would sit uncommitted in the working tree,
the managed worktree would fork past it, and the set would never see the decision
it came from. That is the ordering trap §7 believed it had dodged.

A third gap sits underneath both: an ADR verdict recorded as prose in a ticket
answer enforces nothing and is unparseable by the skills downstream of it.

## Decision

**1. The interview primitive is extracted, and each composing skill states its
own destination.**

- `batch-grill-me` — the interview primitive (design tree, frontier, rounds,
  find-facts-yourself). A verbatim 1-1 overlay of upstream
  `in-progress/batch-grill-me@fde4cd5`. Reads the glossary union, writes nothing.
- `grill-with-docs` — primitive + domain-modeling write discipline +
  commit-on-close. Behaviour unchanged, now composed; its verbatim region shrinks
  to the domain-modeling half.
- `grill-with-map` — primitive + wayfinding answer discipline. Writes only into
  the map directory; never the repo, never a commit. This is the skill a
  wayfinding grilling ticket loads.

The union rule (glossary = base `CONTEXT.md` + `.grill-context/**`) and the
`+`/`~`/`-` op syntax are shared, so they live in **one pop-owned source**,
`integrate/skills/pop/_shared/CONTEXT-FORMAT.md`, copied into each skill
directory at install. Only the destination differs per skill: a `.grill-context`
fragment for `grill-with-docs`, a draft in the map directory for
`grill-with-map`.

**2. Decisions are drafted in the map directory, as files.**

`grill-with-map` applies the same three-criteria ADR test `grill-with-docs`
applies — the verdict is made in the session, with the human present, exactly as
it is today. What changes is only where the artifact lands:

```
wayfinder/<map-id>/
├── map.md
├── issues/
├── adrs/          ADR drafts, <8hex>-<slug>.md, unnumbered
├── context/       glossary op drafts, NN-<slug>.md, one per ticket
└── prototypes/    (ticket 02 §9)
```

Draft ids are 8 hex characters, matching the existing fragment convention
(`CONTEXT.0003.670C8D55.md`). They are draft-side identity only and die at mint;
the repo's sequential ADR numbering is untouched, with the next free number
picked **at mint time** so parallel maps cannot collide on a reserved number.

The ticket answer holds the decision summary and **links** the draft. It never
inlines the ADR body — the same "link assets, don't paste them in full" rule the
Work-store doc already applies to prototypes, and the same one-place rule ticket
01 established.

**3. The verdict is an argument to `resolve`, not prose.**

```
pop map resolve <map-id> <NN> --answer-file <path> \
    --adr adrs/<8hex>-<slug>.md \
    --context context/NN-<slug>.md
```

Both repeatable, both optional. `resolve` verifies each named file exists and
records them on the ticket's manifest entry as `adr_drafts` / `context_drafts`.
No prose is inspected anywhere.

`resolve` is **validate-then-write**: the draft check runs before any write, so a
refusal leaves the ticket untouched and a retry is clean. The `## Answer` write
**replaces** the section rather than appending, making the verb re-runnable on an
already-resolved ticket. This **amends ticket 02 §3**, whose "Decisions-so-far
append" contradicts §6's "rendered from the manifest on every resolve"; §6 is the
correct half, and the rendering is idempotent by construction.

**4. Minting happens in the slice that implements the decision.**

`to-tasks` reads the map's manifest and, for each draft, emits acceptance
criteria that are pure file operations onto the slice implementing that ticket's
subject:

```
- [ ] docs/adr/NNNN-<slug>.md created from wayfinder/<map-id>/adrs/<8hex>-<slug>.md
      (next free ADR number)
- [ ] .grill-context/CONTEXT.<gen>.<HASH>.md created from
      wayfinder/<map-id>/context/NN-<slug>.md
```

Attribution needs no inference: the slice's `## Parent` is the map ticket, so the
owning slice is already identified. Slices implementing no decision get the
backlink and no checkbox. Each draft mints exactly once across all sets spawned
from a map.

The drain commits the ADR and the fragment alongside the code they describe, on
trunk's normal flow. `to-spec` mints nothing and gains no repo-write capability.

**5. Where the knowledge lives.** Minting is a publish mechanic, so it goes in
the Work-store doc's "Publishing tickets" section as a *Map-sourced sets*
subsection. The `to-tasks` overlay gains one sentence — also read the map's
`index.json` — because "the manifest is an input" is an input-mode fact and
everything downstream of it is the doc's.

**6. Timing.** The skill extraction lands **now**, as a standalone slice ahead of
`pop map`: extract the primitive, recompose `grill-with-docs`, add
`grill-with-map`, repoint `integrate/issue-tracker.md` and the wayfinder overlay.
Until `pop map` exists the boundary is text-only, which is sufficient because the
primitive has no write instructions to disobey.

## Considered Options

- **Enforce by refusing `pop map resolve` on a dirty repo tree.** Kept only as a
  *warning*. Pop cannot distinguish an unrelated in-flight change from a stray
  fragment, and blocking a resolve over someone else's dirty tree is worse than
  the trap. The failure mode is *silent* writes; a warning makes them loud.
- **Two skills, with wayfinder invoking `batch-grill-me` directly and the answer
  discipline living in the Work-store doc** (ticket 02 §8's shape). Rejected: it
  is the "invoke the primitive and remember the exceptions" arrangement that
  already failed on ticket 05. A skill whose text mandates repo writes must never
  be the one a wayfinding ticket loads, and the discipline needs a home the model
  has loaded.
- **Mint at handoff, in `to-spec` / `to-tasks`, with a `minted` marker for
  idempotence.** Rejected: it writes uncommitted repo files that the managed
  worktree forks past — the ordering trap above — and needs a marker to stay
  idempotent that the slice-owned form does not.
- **A dedicated "record decisions" first slice that every other slice blocks on.**
  Rejected: it is the handoff batch pass relocated, and it separates each ADR from
  the change it describes.
- **A verdict recorded as prose in the answer, with `to-tasks` inferring
  ownership.** Rejected: unenforceable, unparseable, and it asks `to-tasks` to
  re-derive a judgment the grilling session already made with the human present.
- **Wayfinding carries no glossary deltas; the implementing session coins the
  terms.** Rejected: vocabulary is precisely what a grilling ticket argues out,
  and discarding it invites the colliding-term failure that ticket 02 §8 cites as
  the reason wayfinding reads the glossary at all.
- **Backlinks from the minted ADR and fragment to the map.** Rejected: the map
  lives in a per-machine store, so the link points at a path no teammate can open
  and the local store may archive out from under it. An ADR that needs the map to
  be understood is a badly written ADR. Fragments are transient and any backlink
  dies at consolidation. Lineage already exists exactly once, per ticket 04.

## Consequences

- Decisions live outside the repo for the life of the map, and a chunk that is
  never implemented leaves no repo trace at all. Accepted: no work, no record.
  Drafts survive in the map directory — `Status: done` does not reap them.
- Repo ADR numbers are no longer knowable at decision time, so a ticket answer
  cannot cite "ADR-0175" for a decision this map made. It cites the draft.
- `pop map resolve` grows two repeatable flags and a manifest field pair; `pop map
  show` can then report decisions whose drafts are still unminted.
- Three skill files exist where two do today, and one shared `CONTEXT-FORMAT.md`
  source is copied into each skill directory at install rather than referenced
  across directories.
- A slice that mints an ADR carries a docs change into a code commit. That is
  intentional — it is the mechanism that keeps the decision and the change on the
  same commit — but it means `docs/adr/**` is no longer touched exclusively by
  `docs(...)` commits.
