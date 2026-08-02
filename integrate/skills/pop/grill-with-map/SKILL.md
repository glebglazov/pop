---
name: grill-with-map
description: Wayfinding grilling session for one Decision ticket on a Map — interviews the human to a settled answer and writes only into the Map's own directory (ADR drafts, glossary-op drafts, prototypes), never into the repository and never a commit. Use when resolving a grilling ticket on a wayfinder Map.
disable-model-invocation: true
---

# Grill with a Map

The grilling skill a **wayfinding** ticket loads. It is the interview primitive
plus the wayfinding answer discipline: every artifact the session produces lands
inside the Map's own directory, and the repository is left exactly as it was
found.

## Composed over batch-grill-me

Run the `batch-grill-me` skill for the conversation itself: map the design tree,
ask the whole settled frontier one round at a time, find every fact yourself,
and stop when the frontier is empty. That skill is the interview and nothing
else — it writes nothing and asks which skill records the decisions. This one is
that answer, and its destination is the Map.

Scope the tree to the ticket you are resolving. A question that belongs to a
different ticket is fog to name in the answer, not a branch to walk here.

## Write only into the Map directory

Every write goes under the Map's own folder, the one holding `map.md` and
`issues/`:

```
<map-dir>/
├── map.md
├── issues/NN-<slug>.md      the ticket you are resolving
├── adrs/<8hex>-<slug>.md    ADR drafts, unnumbered
├── context/NN-<slug>.md     glossary op drafts, one file per ticket
└── prototypes/NN-<slug>/    scratch prototypes
```

Nothing else is written. Specifically, in the repository under study: no
`CONTEXT.md` edit, no `.grill-context/` fragment, no `docs/adr/` file, no code
change, and **no commit** — not at the close of the session, not at any point
during it. If you find yourself reaching for `git add`, the discipline has been
broken.

This is not a caution, it is the contract: a Map lives for many sessions across
many days, and the repository must stay clean for all of it. The decisions
reach the repo later, minted by the slice that implements them (see *Closing*
below).

## Read the glossary, draft your ops into the Map

Read the project's glossary before the first round and challenge the user's
terms against it. The glossary is the **union** of the base `CONTEXT.md` and
every delta fragment that touches it, computed in memory at read time.
[CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md) has the union rule, where fragments
live, and the `+`/`~`/`-` op syntax. Unioning is read-only and stays safe with
any number of sessions running in parallel.

Writing is where this skill differs from `grill-with-docs`: **the ops go into
the Map, not into `.grill-context/`.** One file per ticket, at
`context/NN-<slug>.md`, where `NN-<slug>` matches the ticket you are resolving.
Use the same `+`/`~`/`-` op bodies CONTEXT-FORMAT.md specifies, and drop the
fragment frontmatter — a draft is not a generation-numbered fragment, and the
generation is picked when the draft is minted. Write once at each round's close,
appending that round's settled ops to the one file; skip rounds that settled no
terms.

## ADR drafts

Apply the same three-criteria test `grill-with-docs` applies — the verdict is
made here, in session, with the human present. Offer an ADR only when all three
hold:

1. **Hard to reverse** — the cost of changing your mind later is meaningful
2. **Surprising without context** — a future reader will wonder "why did they do
   it this way?"
3. **The result of a real trade-off** — there were genuine alternatives and you
   picked one for specific reasons

If any of the three is missing, skip the ADR; the ticket answer alone carries
the decision.

When the test passes, write the ADR body to `adrs/<8hex>-<slug>.md` in the Map
directory. Use the section structure in [ADR-FORMAT.md](./ADR-FORMAT.md) —
Context, Decision, Considered Options, Consequences — and **ignore its location
and numbering rules**: a draft carries no ADR number and never lands in
`docs/adr/`. The `8hex` id is draft-side identity only (`uuidgen | head -c8`,
lowercased); it dies at mint, when the repo's next free sequential number is
picked. Never cite an `ADR-NNNN` number for a decision this Map made — the
number does not exist yet.

Write the ADR body in full in the draft. It is the single copy of the decision's
repo-facing form.

## The answer links the drafts

The ticket's `## Answer` holds the decision and a one-line gist of why, and
**links** each draft it produced — it never inlines the ADR body. Same rule the
Work-store doc already applies to prototypes: link assets, don't paste them in
full. The mechanics of resolving a ticket (where the answer goes, the status
flip, the Map's decision index) belong to the Work-store doc; follow it, and add
nothing to it.

A prototype run during the session goes to `prototypes/NN-<slug>/` under the Map
directory, not into the repository, with its path and verdict recorded in the
answer.

## Closing the session

The session closes when the ticket's question is settled and its answer, drafts
and links are written. There is nothing to commit, so don't offer to: the drafts
stay in the Map for the rest of its life.

Each draft is **minted** later — copied into `docs/adr/NNNN-<slug>.md` or into a
`.grill-context/` fragment — by the Task-set slice that implements the decision,
so the ADR and the code it describes land in the same commit. That is the
implementing session's job, never this one's.
