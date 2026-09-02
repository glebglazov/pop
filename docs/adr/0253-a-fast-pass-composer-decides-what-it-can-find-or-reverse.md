---
status: accepted
---

# A fast-pass composer decides what it can find, and reports it

Pop will ship `grill-with-docs-fast`, a second human-opened composer beside `grill-with-docs` for designs that are easy to implement but still deserve the domain language. It **overrides `grilling`'s "the _decisions_ are the user's — put each to them and wait"**: it asks only the decisions that are genuinely the human's preference *and* reshape the rest of the design tree, decides the rest itself, and reports each such **Fast-pass decision** in the round's ledger. Every shared rule the two composers follow moves out of `grill-with-docs`'s body into one document both install. This amends the composer enumeration in [ADR-0225](0225-domain-modeling-owns-the-fragment-discipline.md); its skill-boundary and write-destination decisions remain in force.

## Decision

1. **`grill-with-docs-fast` is a separate skill, not a mode.** It is human-opened (`disable-model-invocation: true`) like its parent, because it commits repository artifacts. A mode flag on a human-opened skill is a thing the human forgets to pass; the trigger is the point.

2. **The ask/decide filter is findability plus branch factor.** Ask when the answer is a genuine preference of the human's — not derivable from the code, the glossary or the ADRs — *and* the wrong choice would reshape decisions downstream of it. Everything else is decided and reported. Reversibility is deliberately **not** a criterion: an easily reversed but tree-reshaping call still needs the human, and the fact-finding rule already covers "look it up yourself."

3. **One round is the target; a second round only for decisions a first-round answer opened.** If the filtered frontier exceeds roughly three questions, the session says so and names `grill-with-docs` as the better fit — and then continues anyway. The escape valve is a report, never a refusal; the human overrides.

4. **Fast-pass decisions are reported per round and persisted in the closing commit body.** Each ledger line is the call, the alternative rejected, and one clause of why, printed *before* the human answers that round's questions so an override costs no extra turn. The full ledger is restated once at the close and written into the commit body under `Decided without asking:`, so it survives into the worktree a later `to-tasks` forks from HEAD. It stays out of `CONTEXT.md`, which is a glossary, and out of ADRs unless a call independently meets the three ADR criteria.

5. **`domain-modeling`'s discipline runs at full strength; only its conduct changes.** Scenario stress-testing becomes fact-finding rather than interrogation: invent the edge-case scenario, resolve it against the code and the glossary union, and surface it as a question only if it passes the filter. `domain-modeling` itself is unchanged.

6. **Every rule the two composers share lives in one document, `GRILL-SESSION.md`.** `grill-with-docs` owns it and holds all three of its own behaviours there — the round-close glossary write beat, the unified fact-finding activity, and the commit at close. Both composers' bodies reduce to loading `grilling` and `domain-modeling` and following the document; `grill-with-docs-fast`'s body carries only its delta. `sharedSkillDocs` copies it into the fast sibling, so `sharedSkillDocOwner` generalises from a single constant to per-document ownership.

## Considered Options

- **A `--fast` mode on `grill-with-docs`.** Rejected per decision 1: one body, but a trigger the human must remember, on a skill a human already opens by name.
- **Have `grill-with-docs-fast` load `grill-with-docs` for the close.** Impossible, not merely undesirable: a `disable-model-invocation` skill cannot be loaded by another skill, which is why `grilling` and `domain-modeling` are agent-loaded at all.
- **Restate the close in the fast body.** Rejected because two copies of the commit procedure is exactly the duplication ADR-0225 exists to prevent, and it would give the read-whole direction two bodies to keep honest.
- **Extract the close into a third agent-loaded skill (`grill-commit`).** Rejected: the close is a procedure both sessions follow verbatim, which is a document, not a session shape — and ADR-0225 already declined new taxonomy for one thing.
- **Scope the shared document to the close only.** Rejected because the round-close glossary beat and the fact-finding rule would then be duplicated instead — the same problem one size smaller.
- **Reversibility as the ask/decide criterion.** Rejected per decision 2: it asks about cheap-to-change calls that reshape everything and stays silent on expensive ones the code already answers.
- **Dial `domain-modeling` down for speed.** Rejected per decision 5: reading the model without sharpening it forfeits the reason the fast pass loads the discipline at all.

## Consequences

- Pop has two composers whose shared behaviour has exactly one copy. A change to the close is one edit; a drift between the siblings becomes impossible rather than merely discouraged.
- `grill-with-docs`'s body shrinks to a loader. The behaviour assertions pinned on it in `integrate/grill_composition_test.go` move to assertions on `GRILL-SESSION.md` and on the rendered copy each sibling installs.
- The fast pass contradicts a rule in the upstream text it inlines. That contradiction is legible only because it is a *marked* override in the fast body, the same shape as `domain-modeling`'s single-writer override; an unmarked one would read as a skill ignoring the skill it loads.
- A fast-pass session's reasoning is recoverable from `git log` alone, without the transcript.
