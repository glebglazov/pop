---
status: accepted
---

# Every attribution pass contributes a tier, and the lift becomes a band

> **Relates:** amends [ADR-0201](0201-a-pane-is-attributed-to-work-kind-side-and-seeds-the-dashboard-cursor.md),
> [ADR-0209](0209-an-attributed-pane-pins-its-rows-to-the-top-and-says-nothing-else.md) and
> [ADR-0241](0241-a-repository-pass-lifts-the-work-that-lives-where-the-pane-stands.md).
> Their text is left intact — an ADR is a dated record, not a live description — so all three
> still describe a first-hit ladder and a `▸` mark on every lifted row. The glossary carries
> the live description.

**Pane work attribution** stops being first-hit. Every pass contributes, and the strongest
pass that named a container becomes that container's **Attribution tier** — the top-level
key of the **Work surface sort order** on page A. The **Work lift** stops being a block
that is moved and becomes that ordering plus a background band over the attributed region.

## Context

The ladder answered with one pass and silenced the rest. A pane carrying `@pop_set` for one
Task set was therefore attributed to that set *alone*, even while standing in a checkout two
more sets were bound to and in a repository holding sixty more. That is what a human hit: a
dashboard opened from a pop drain pane lifted one pop row and marked it `▸`, while the two
other open pop sets — bound to the very checkout the pane was standing in — sat unmarked
below it. They happened to sort directly underneath, being the newest rows on the page, so
the mark was the only thing telling the truth, and it read as a bug rather than as an answer.

The passes are not competing answers to one question. They are three answers of differing
strength to *how* the pane relates to a row: this pane **is** that work (tag), that work
lives in the checkout I am standing in (neighbourhood), that work lives in this repository
(repository). First-hit throws away two thirds of what the ladder knows, and it throws away
exactly the part a human reads as relevance.

ADR-0241 had already walked one step down this road — its decision 2 made the weakest pass
merge across kinds, on the ground that its meaning is plural where a tag's is singular. The
step this ADR takes is the same observation applied to the ladder as a whole: strength is
not precedence, it is rank.

## Decision

**1. Every pass runs, and a container carries the strongest tier that named it.**
`AttributePane`, `AttributePaneNeighbourhood` and `AttributePaneRepository` stay three seam
methods, and the builder now calls all three rather than stopping at the first that answers.
A container named by two passes takes the stronger; the tiers are tag, neighbourhood,
repository, and untiered.

**2. The tier is the first key of the Work surface sort order on page A, above kind
precedence.** ADR-0209 decision 1 applied the lift *after* the sort because no comparator
could raise a Map above the task-set block. A tier term above kind precedence expresses that
directly and is strictly more general: the old block is what the ordering looks like when the
tag tier is the only occupied one. Within a tier the page falls through to its ordinary
ordering, so the tier partitions the page into *here* and *elsewhere* and shuffles nothing
inside either half. A Map in the pane's repository consequently outranks a Task set in
somebody else's, which inverts kind precedence deliberately: where the human is standing is a
stronger signal than "sets before maps".

**3. This is not the return of the membership tiers, and the difference is recorded.** The
live-drain / auto-drain / orphaned tiers were retired for putting a July container above an
August one in a newest-first view. They were properties **of a row** — auto-drain is a
durable registration bit, so every set that ever enabled it floated forever, on every machine
and in every pane, which made the violation constant. The attribution tier is a property of
the **relation** between a row and the pane: the same container is tier 3 from one pane and
untiered from the next. It is also gated by `lift = true`, which those tiers never were, so
the archival date views that retirement protected stay in pure date order.

**4. The neighbourhood pass is one tier, ordered by Bound-checkout lift order.** The
live-drain rung is absorbed rather than promoted to a tier of its own: its answer is the set
whose drain owns the checkout, and **Bound-checkout lift order**'s first term is the
**Checkout claim** holder — the same fact reached a second way. One ordering question, one
mechanism.

**5. The tag pass stops being first-hit across kinds.** Every kind answers it and the
answers concatenate in kind precedence order. A pane carrying two true tags is rare and both
answers are correct; the leader — and therefore the seeded cursor — is unchanged in every
case that exists today.

**6. The repository tier stays unrestricted.** It now fires on every build rather than only
when the passes above are silent, which removes ADR-0241 decision 3's stated justification
for its volume, but not its reasoning: narrowing it to bound or recent sets reintroduces the
"reachable only if somebody happened to bind it" silence it was added to remove. The preset
remains the volume governor (ADR-0209 decision 7) — a pane in pop's own checkout names 64
containers and `active` renders 3 of them. Under a wide preset such as `all` the page really
does become "this repository first, then the world", and that is accepted as a fair reading
of an archival view.

**7. The attributed region is drawn as a background band; `▸` survives on the tag tier
alone.** The band's job is the *boundary* — where "work that lives here" stops — so it is one
shade over the whole region, not one per tier: a shade per tier needs a legend that is not on
screen, and tiers are an ordering rather than categories anyone names. The mark is kept for
the tag tier because it says the one thing the band cannot: this pane **is** that work, rather
than that work merely lives here. That distinction is precisely what the reported confusion
was about, so collapsing both signals into one would answer the complaint by deleting the
information.

**8. The band is full width across both lines of a row, from a light/dark palette pair, and
unconditional.** Full width including the prefix gutter, or it reads as a rendering fault
beside the cursor block; both lines, or every row is sliced in half. It is pop's first
background colour — the whole palette is foregrounds over the terminal's own background — so
it is a `paletteColor` pair like every other house colour and falls to the dark member on a
terminal that never answers the appearance query (ADR-0230). It renders even when it covers
the whole page: a band that vanishes when another project's work arrives teaches the human
that its absence means something, and it does not.

**9. A marked row leaves the band with the rest of it.** The **Selection area** moves marked
rows into a region at the foot (ADR-0215); a band inside a region of one kind of thing marks
nothing off from anything, and dropping it keeps the band's invariant simple — one contiguous
region at the top of the table, never twice on one screen.

**10. The preset grant and its spelling are unchanged.** `lift = true` still grants the whole
behaviour and `pin` remains its permanent silent alias. The concept named below is new; a
third alias generation for a boolean nobody is confused about is churn.

**11. Named Attribution tier, and "relevance" stays page B's word.** `Routine relevance tier`
already means page B's ordering by a Routine's own liveness, which has nothing to do with the
pane. Two same-named tiers on two pages of one dashboard is the collision ADR-0241 renamed
"pin" to escape. **Work lift** keeps its name for the behaviour: attributed work is still
lifted above unattributed work, and the word survives the change from block to ordering.

## Considered Options

**Keep first-hit and widen the individual passes.** Rejected: every widening either makes a
weaker pass beat a stronger one or reproduces the silence one layer down. The passes are
ranked answers, not competing ones, and no amount of widening expresses rank.

**Shade the band per tier.** Rejected in decision 7. Three shades of one hue is a legend the
dashboard has nowhere to print, and the reader who most needs the band — the one who just
opened it — is the least likely to know the legend.

**Suppress the band when every row on the page is attributed.** Rejected in decision 8. It
saves a colour in the one case where nothing is lost by drawing it, and it teaches a false
meaning for the band's absence.

**Narrow the repository tier now that it always fires** (bound sets only, or a recency
window). Rejected in decision 6: it is ADR-0241's rejected option wearing a new justification.

**Drop `▸` entirely, as the band was proposed to replace it.** Rejected in decision 7 — but
narrowly. If one mechanism is ever wanted, the band is the one to keep: it answers the
question that was actually asked, where the mark answers a rarer one.

## Consequences

- `work.Container.Lifted bool` becomes a tier value, and `ui.List`'s `Lifted` option becomes
  a row-state the cell renderer bands rather than a prefix-column mark. The `▸` cell stays,
  wired to the tag tier only.
- `work.AttributePane` no longer returns at the first answering pass; `Attribution` keeps its
  ranked container list, whose head still seeds the dashboard cursor at first render — the
  tag-tier row where there is one, exactly as today.
- The dashboard gains the product's first background colour, and every future row-render
  question (search-match highlight, status tones, the cursor block) is now a foreground over
  a background that may or may not be there.
- `pop work status` builds with empty pane facts, so it gets no tiers, no band and no
  reordering. The two surfaces' orders now differ by one key under a lifting preset, which is
  the same divergence the block already introduced.
- ADR-0241 decision 2's "the passes above it stay first-hit" and decision 1's "reached only
  when nothing above answered" no longer describe the ladder. Their reasoning survives as the
  argument for ranking; only the arity changed.
