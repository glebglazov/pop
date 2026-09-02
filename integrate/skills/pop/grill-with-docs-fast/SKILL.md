---
name: grill-with-docs-fast
description: Fast-pass grilling session for work that is easy to implement but still deserves the project's domain language. Same contract and same close as grill-with-docs, in as few rounds as possible: it asks only the decisions that are genuinely yours to make and that reshape the rest of the design, decides everything else itself, and reports each such call so you can override it. Use when the user wants a plan settled quickly without answering every small question.
disable-model-invocation: true
---

<!--
No upstream base, so no drift pin and no verbatim region: this file is
pop-original composition (ADR-0253). It is `grill-with-docs`' sibling and it is
deliberately thin — every rule the two composers share lives in the
`GRILL-SESSION.md` this skill receives from `grill-with-docs` at install time
(`sharedSkillDocs` in integrate/catalog.go), so this body is only the delta.

It cannot delegate by loading its sibling: `grill-with-docs` is human-opened,
and a `disable-model-invocation` skill cannot be loaded by another skill — the
same reason `grilling` and `domain-modeling` are agent-loaded. Hence a shared
document rather than a shared skill.

**human-opened** (`disable-model-invocation`, like its sibling): this session
commits decisions to the repository, and ADR-0225 puts the manual gate on
exactly those sessions. It is also the trigger that makes the fast pass worth
having as a skill instead of a mode — a human chooses the pace by choosing which
one to open.

The override below is marked, in the shape of `domain-modeling`'s single-writer
override, because it contradicts a rule in the interview primitive this skill
loads. An unmarked one would read as a skill ignoring the skill it loads.
-->

# Grill with docs, fast pass

The same session as `grill-with-docs` — same two skills, same glossary
fragments, same ADRs, same closing commit — conducted in as few rounds as
possible. What changes is who answers the small questions: this session answers
them itself and tells the user what it decided.

Use it for work that is easy to implement and still deserves the project's
language. Everything the two siblings share is in
[GRILL-SESSION.md](./GRILL-SESSION.md) — the round-close beat for glossary
writes, fact-finding as one activity, and the close. Read it and follow it as
written; this body adds only what differs.

## Load both skills before the first round

Run the `grilling` skill for the conversation itself: map the design tree, and
work the frontier in rounds. Run the `domain-modeling` skill for the model the
conversation is sharpening. Read both before the first round, exactly as
`grill-with-docs` does.

## Override: not every decision goes to the user

**Override (negates `grilling`'s "The _decisions_ are the user's — put each to
them and wait"): put a frontier decision to the user only when both of these
hold, and decide the rest yourself.**

1. **It is genuinely theirs.** The answer is a preference of the user's that is
   not derivable from the code, the glossary union, or the ADRs. Anything you
   could look up is a fact, and facts were never theirs to supply.
2. **It reshapes the tree.** The wrong choice would change which decisions get
   made downstream of it, not just how one of them reads.

Reversibility is deliberately **not** a criterion. A cheaply reversed call that
reshapes the tree still needs the user; an expensive one the code already
answers does not.

Everything that fails the test — naming inside a package, which existing helper
to reuse, where a test goes, how an error reads, file placement — is a
**fast-pass decision**: decide it, and report it. Never ask a question you have
already decided; a question with a foregone answer costs the user a turn and
buys nothing.

## Report every decision you make

Each round, print the fast-pass decisions **before** that round's questions, so
an override costs no extra turn — the user answers and overrides in one message:

```
⚡ **Fast-pass decisions I made for you**
- **<the call>** → <what you chose>. *(Rejected: <the alternative>. <one clause of why>.)*
```

An override is the user's word against yours and wins without argument. At the
close, restate the whole ledger once, so the settled state is in one place.

## One round, and an escape valve that never blocks

One round is the target. A second round happens only when a first-round answer
*opened* a decision that passes both tests above — not because more questions
occurred to you.

If the filtered frontier for round one comes out at more than about three
questions, the work is not a fast pass. Say so, name `grill-with-docs` as the
better fit — **and then carry on with the fast pass anyway**. This is a report,
never a refusal and never a question; the user redirects if they want to.

## Conduct the discipline, don't interrogate with it

`domain-modeling` runs at full strength here — all four activities, unchanged.
Only its conduct changes: when you invent a concrete scenario to stress-test a
relationship, resolve it yourself against the code and the glossary union
instead of putting it to the user, and surface it as a question only if the
answer passes both tests above. Otherwise the scenario and its resolution go in
the ledger. A fast pass that skipped the sharpening would forfeit the reason it
loads the discipline at all.

## One addition to the close

Follow `GRILL-SESSION.md`'s closing commit exactly, with one thing added at its
subject step: **write the session's full fast-pass ledger into the commit body**,
under a `Decided without asking:` heading, one short line per call. The
resolved commit convention still governs the grammar of the whole message,
heading included — if it has no room for such a section, it wins.

The ledger is persisted because a fast pass leaves fewer answers in the
transcript than an ordinary grilling does, and the reasoning has to survive into
the worktree a later `to-tasks` forks from HEAD. It goes in the commit body and
nowhere else: `CONTEXT.md` is a glossary, and an ADR is for a decision that
independently meets its own three criteria.
