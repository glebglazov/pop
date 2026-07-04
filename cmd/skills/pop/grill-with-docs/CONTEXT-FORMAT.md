# CONTEXT.md Format

## Structure

```md
# {Context Name}

{One or two sentence description of what this context is and why it exists.}

## Language

**Order**:
{A one or two sentence description of the term}
_Avoid_: Purchase, transaction

**Invoice**:
A request for payment sent to a customer after delivery.
_Avoid_: Bill, payment request

**Customer**:
A person or organization that places orders.
_Avoid_: Client, buyer, account
```

## Rules

- **Be opinionated.** When multiple words exist for the same concept, pick the best one and list the others under `_Avoid_`.
- **Keep definitions tight.** One or two sentences max. Define what it IS, not what it does.
- **Only include terms specific to this project's context.** General programming concepts (timeouts, error types, utility patterns) don't belong even if the project uses them extensively. Before adding a term, ask: is this a concept unique to this context, or a general programming concept? Only the former belongs.
- **Group terms under subheadings** when natural clusters emerge. If all terms belong to a single cohesive area, a flat list is fine.

## Single vs multi-context repos

**Single context (most repos):** One `CONTEXT.md` at the repo root.

**Multiple contexts:** A `CONTEXT-MAP.md` at the repo root lists the contexts, where they live, and how they relate to each other:

```md
# Context Map

## Contexts

- [Ordering](./src/ordering/CONTEXT.md) — receives and tracks customer orders
- [Billing](./src/billing/CONTEXT.md) — generates invoices and processes payments
- [Fulfillment](./src/fulfillment/CONTEXT.md) — manages warehouse picking and shipping

## Relationships

- **Ordering → Fulfillment**: Ordering emits `OrderPlaced` events; Fulfillment consumes them to start picking
- **Fulfillment → Billing**: Fulfillment emits `ShipmentDispatched` events; Billing consumes them to generate invoices
- **Ordering ↔ Billing**: Shared types for `CustomerId` and `Money`
```

The skill infers which structure applies:

- If `CONTEXT-MAP.md` exists, read it to find contexts
- If only a root `CONTEXT.md` exists, single context
- If neither exists, create a root `CONTEXT.md` lazily when the first term is resolved

When multiple contexts exist, infer which one the current topic relates to. If unclear, ask.

A context's **link text** in the map (`Ordering`, `Billing`) is its canonical identifier: it names the context in prose and, slugified, prefixes that context's fragment files (see the fragment scheme below). The bullet-link map layout above is the canonical `CONTEXT-MAP.md` format — keep to it so the slug is derivable.

## Concurrent writes: fragments

`CONTEXT.md` is one shared file. When agents run in parallel — or a team uses this skill — concurrent writes to it conflict. The fix: **never write the base file during a normal session.** Each session writes its own delta fragment; readers union base + fragments; a human folds fragments back in on demand.

### Write to your own fragment

All session fragments live in **one hidden directory at the repository root, `.grill-context/`** — never beside the base `CONTEXT.md`. This keeps the churn of parallel sessions out of the working directories, and keeps a multi-context repo to a single fragment dir instead of one per context. Create `.grill-context/` lazily the first time this session resolves a term.

The filename says which context the fragment deltas and its generation:

```
# multi-context repo (CONTEXT-MAP.md exists)
.grill-context/<slug>.<counter>.<uuid>.md

# single-context repo (only a root CONTEXT.md, no map)
.grill-context/CONTEXT.<counter>.<uuid>.md
```

- `slug` binds the fragment to its context. Derive it from `CONTEXT-MAP.md`: take the context's link text, lowercase it, and collapse every run of non-alphanumeric characters to a single `-` (`Ordering` → `ordering`, `Order Fulfillment` → `order-fulfillment`). The slug prefix is the *only* thing separating one context's fragments from another's in the shared dir, so context link text must be unique after slugifying. In a single-context repo there is no map — use the literal `CONTEXT` in the slug position.
- `counter` is a zero-padded, **per-context** generation such as `0001`, `0002`, `0003`. Pick it by scanning **all** fragments for this context — both `.grill-context/<slug>.*.*.md` and any legacy fragments colocated beside the base (see below) — and using `max(counter) + 1`; if none exist, use `0001`. Scope the scan to this context's slug so parallel contexts never inflate each other's generations.
- `uuid` comes from `uuidgen | head -c8`.

A fragment's context is given entirely by its filename slug (or, for a legacy fragment, the directory it sits in) — there is no `context:` field in the body.

Reuse that one file for every term you resolve this session. Because every session writes a different uuid, parallel work never collides at the file level. If two agents both start from the same visible fragments, they may create the same counter with different uuids; that is fine.

A fragment is a list of **delta ops**, not a full glossary:

```md
---
fragment: 8f3a2c1d
generation: 0002
branch: feat/settlement      # or task id — where this came from
---

+ Settlement
  Funds moved from escrow to the merchant after dispatch.
  avoid: payout, transfer
  under: Payments

~ Order
  An agreement to supply goods at an agreed price.
  was: A request from a customer to purchase goods.

- Buyer
```

- `+ Term` — **add** a new term. Body is the definition; optional `avoid:` and optional `under: <heading>` placement hint.
- `~ Term` — **redefine** an existing term. New definition, plus a `was:` snapshot of the effective definition *at the moment you edited it* (base plus visible lower-generation fragments) — so a later consolidation can tell if the meaning drifted underneath.
- `- Term` — **retire** a term.

Provenance is just `fragment:` (= the uuid), `generation:` (= the filename counter), and `branch:`/task. Don't track commit SHAs — the fragment rides in the same commit as the change that motivated it, so `git log --follow` is your index for free.

### Read = union, in memory

The effective glossary for a context is its **base `CONTEXT.md` overlaid with every fragment that deltas it**, computed at read time. Fragments live in two places, and you read both:

- `.grill-context/<slug>.*.*.md` at the repo root — where this scheme writes them now. (Single-context: `.grill-context/CONTEXT.*.*.md`.) Select this context's fragments by slug prefix.
- legacy `<dir>/CONTEXT.*.*.md` colocated beside the base — where older sessions wrote them. Still read so nothing is lost; `grill-consolidate` drains these into the base over time.

This is read-only — globbing and unioning never mutates a file, so it never conflicts. Glob both locations at session start and treat the union as the glossary you challenge terms against. `.grill-context/` is a dotdir, so include hidden paths when you glob it — `rg --hidden`, `ls -a`, or shell `dotglob` — since default ripgrep skips hidden paths.

Overlay rules:

- A fragment op beats the base (`~` redefinition wins over the base definition; `-` removes it).
- Higher generations beat lower generations for the same term. This means a generation-`0002` fragment can intentionally override a delta from a generation-`0001` fragment that was visible when the later session started.
- **Two fragments in the same generation touching the same term = collision.** Render both, marked `⚠ contested — needs consolidation`. Do *not* silently pick one. This is where a genuine parallel semantic conflict announces itself instead of hiding.
- A `+` term whose `under:` matches an existing heading slots there; with no hint or a novel heading, it renders under `## Unfiled (pending consolidation)`.

### Consolidate with `grill-consolidate`

Folding fragments into the base is the **only** operation that mutates `CONTEXT.md`, so it belongs to the `grill-consolidate` skill. Run that skill on demand — when fragments pile up, or before a release — as a deliberate single-writer maintenance pass, never automatically and never in parallel.

`CONTEXT-MAP.md` is **not** fragmented — adding or rewiring a context is rare and structural, so tolerate the occasional conflict or settle it during consolidation.
