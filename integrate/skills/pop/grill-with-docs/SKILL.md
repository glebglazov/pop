---
name: grill-with-docs
description: Grilling session that challenges your plan against the existing domain model, sharpens terminology, and records what settles as glossary fragments and ADRs — conflict-free under parallel agents and teams via generation-numbered fragments and sequential-id ADRs — then commits the session's own artifacts as it closes. Use when user wants to stress-test a plan against their project's language and documented decisions.
disable-model-invocation: true
---

<!--
No upstream base, so no drift pin and no verbatim region: this file is
pop-original composition (ADR-0225 decision 4). Upstream reaches the same
session by loading two skills together, and this file is that composition plus
what is pop's alone — the round-close beat for glossary
writes, one fact-finding activity, and the close.

Those rules now live in `GRILL-SESSION.md` beside this file rather than in this
body (ADR-0253 decision 6), because `grill-with-docs-fast` follows them
identically and cannot load them: this skill is human-opened, and a
`disable-model-invocation` skill cannot be loaded by another skill. This skill
owns that document; Integration copies it to the fast sibling.

Neither composed skill is inlined here. `grilling` is the interview primitive
and `domain-modeling` is the glossary/ADR discipline; both ship in this same
component and both are agent-loaded, so this session can load them. Restating
either would give pop a second copy of a rule to keep honest, which is the one
thing the composition exists to prevent. The two format documents this file used
to carry are `domain-modeling`'s companions — it reaches them through that
skill, not through a copy of its own.

**human-opened** (`disable-model-invocation`, as upstream's domain-modeling
counterpart is): this is the session that commits decisions to the repository,
so a human opens it with `/grill-with-docs` and the model never starts it on
its own.
-->

# Grill with docs

A grilling session whose decisions land in the repository. It is two skills plus
a beat, a rule and a commit of its own.

## Load both skills before the first round

Run the `grilling` skill for the conversation itself: map the design tree, ask
the whole settled frontier one round at a time, find every fact yourself, and
stop when the frontier is empty. That skill is the interview and nothing else —
it writes nothing, and leaves the destination to whichever skill composed it.

Run the `domain-modeling` skill for the model the conversation is sharpening:
challenge the user's terms against the glossary union, sharpen fuzzy language,
stress-test relationships with concrete scenarios, write a glossary op to this
session's fragment when a term settles, and offer an ADR only when its three
criteria all hold.

Read both before the first round.

## Then follow this session's own rules

They are in [GRILL-SESSION.md](./GRILL-SESSION.md), which this skill owns: the
round-close beat for glossary writes, fact-finding as one activity, and the
close — the commit, then the ask for what to do with the settled plan. Read it before the first round too — the beat
governs round one — and follow it as written. Nothing in it repeats a rule
either loaded skill already carries, and this body adds nothing to it.
