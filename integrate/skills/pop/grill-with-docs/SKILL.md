---
name: grill-with-docs
description: Grilling session that challenges your plan against the existing domain model, sharpens terminology, and records what settles as glossary fragments and ADRs — conflict-free under parallel agents and teams via generation-numbered fragments and sequential-id ADRs — then commits the session's own artifacts as it closes. Use when user wants to stress-test a plan against their project's language and documented decisions.
disable-model-invocation: true
---

<!--
No upstream base, so no drift pin and no verbatim region: this file is
pop-original composition (ADR-0225 decision 4). Upstream reaches the same
session by loading two skills together, and this file is that composition plus
the three things that are pop's alone — the round-close beat for glossary
writes, one fact-finding activity, and the closing commit.

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

Read both before the first round. Everything below is what this session adds on
top of them; nothing below repeats a rule either skill already carries.

## Glossary writes ride the round

**Write once a round, not per term.** The interview settles decisions in rounds,
so in this session the glossary writes ride the same beat: at each round's close,
if that round settled any terms, write their ops to your fragment in a single
update, and skip rounds that settled nothing. This is the session beat
`domain-modeling` leaves to a composing workflow; it replaces that skill's
write-when-it-settles timing and nothing else — where the fragment lives, how its
generation is picked, and the op syntax are unchanged.

## Fact-finding is one activity

The interview's "find facts yourself, never ask the user," and the discipline's
"challenge against the glossary" and "cross-reference with code" are **the same
activity**, not three separate ones. Read the code and the base+fragment
glossary union directly — inline for a cheap check, a non-blocking sub-agent for
heavy exploration — and surface any contradiction between what you find and what
the user claimed. There is no path where you ask the user to supply a fact you
could have looked up.

## Closing the session

Once you've proposed the final glossary updates and any ADRs, and the user signals the design is settled (or asks to wrap up), **commit the artifacts this session produced automatically** — don't ask first. Committing is always desired at the close, so just do it and report what was committed. Do this once, at the natural close — don't commit mid-grill or after every individual fragment.

Why this matters: these artifacts often get carried into downstream work via a fresh git worktree forked from the current branch's HEAD (for example when `to-tasks` later turns the plan into work items). Anything not committed to HEAD is left behind. The session that produced the artifacts is the right place to commit them, so don't defer this to a later skill.

To commit:

1. **Skip if nothing to do.** If the working directory is not a git repository, or this session created/modified no committable repository files, say so and skip.
2. **Identify session paths.** From this conversation's history, list *exactly* the repository files this session created or modified — the base glossary (`CONTEXT.md`, `CONTEXT-MAP.md`), session fragments (`.grill-context/**`, plus any legacy `CONTEXT.*.md` colocated beside a base), ADRs (`docs/adr/**`), and any code or prototype the session touched. Commit CONTEXT fragments **as-is** — folding them into the base is the separate `grill-consolidate` pass, never part of this commit. Do **not** include files this session never touched, even if dirty; prior-session artifacts are intentionally out of scope.
3. **Stage exactly those paths** (never `git add -A`) and create a **single commit**. Derive a short `<topic-slug>` from the subject of the grilling session (the term or area discussed). The type follows content:
   - docs-only → `docs(<topic-slug>): <summary> (ADR-NNNN + glossary)` (drop whichever parenthetical part doesn't apply)
   - mixed code + docs → a fitting conventional type (`feat`, `chore`, …), still scoped `(<topic-slug>)`

   Write a short human `<summary>` of what the artifacts cover (e.g. `effort-model-resolution glossary + ADR-0032`).

   Before writing the subject, resolve the commit convention by asking pop:

   ```
   pop conventions get commits
   ```

   Do not derive the grammar yourself. The command always exits 0 and always
   prints rules to follow — pop's own shipped answer where nobody has written
   one — so take the printed convention as resolved and match its grammar and
   trailer. The `type` still follows content (docs-only vs. mixed), as above.
4. **Report.** After committing, show the user the exact files staged and the commit subject. Separately, report any dirty files this session did *not* touch as "left alone — not staged" so nothing is silently swept or split.

After the commit, the plan is settled and persisted; the user will typically move on to a separate step (such as `to-tasks`) themselves.
