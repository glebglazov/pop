---
name: batch-grill-me
description: Interview the user relentlessly about a plan or design until you reach a shared understanding — map the design tree, ask the whole settled frontier one round at a time, and find every fact yourself. Reads the project glossary as input and writes nothing. Use when a plan needs stress-testing without anything on disk being touched.
disable-model-invocation: true
---

<!--
base: mattpocock/skills in-progress/batch-grill-me@fde4cd5

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a verbatim copy of in-progress/batch-grill-me/SKILL.md at the pinned
ref above — the interview primitive on its own: design tree, frontier, rounds,
find-facts-yourself. Pop inlines it rather than delegating to
`/batch-grill-me`, per ADR-0009 (skills are embedded in the binary and ship to
machines without Matt's skills installed). Upstream's `disable-model-invocation`
frontmatter is kept: grilling is a manual-only session the user opens, never
something the model starts on its own.

**The pinned ref is in-progress and unpublished upstream.** Upstream's shipped
grilling skills are explicitly one-question-at-a-time, so the batch variant
appears abandoned or absorbed and this ref is unlikely to ever move. A frozen
pin is acceptable precisely because of that: the drift diff costs nothing and
will keep coming back empty. Do not read the pin as tracking a live upstream —
the signal to re-pin is a *new* upstream batch-grilling skill, not a new commit
on this ref. To review drift, diff the region between this header and the
marker against in-progress/batch-grill-me@<newref>.
-->

Interview the user relentlessly until you reach a shared understanding. Map this as a **design tree**: every decision branches into the decisions that hang off it.

Work the tree in **rounds**. The **frontier** is every decision whose prerequisites are already settled — the questions you can ask *now* without guessing at answers you haven't heard yet. Ask the whole frontier in one round: number each question and give your recommended answer. Then wait for the user's answers before the next round.

Each round the user answers reshapes the tree — settled decisions push the frontier outward and unblock questions that depended on them. Recompute the frontier and ask the next round. A question whose answer depends on another question still open in this round belongs to a *later* round, not this one.

Finding *facts* is your job, never the user's. When a frontier question needs a fact from the environment (filesystem, tools, etc.), dispatch a sub-agent to find it — don't ask the user for anything you could look up yourself. Don't block on it: a running exploration is an unsettled prerequisite, so only the questions downstream of it wait for the sub-agent to report — ask the rest of the frontier now. The *decisions* are the user's — put each to them and wait.

The session is done when the frontier is empty: every branch of the design tree visited, nothing left silently assumed. Do not act on it until the user confirms you have reached a shared understanding.
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Everything below is pop-specific and has no upstream twin. It gives the
primitive its read boundary: the glossary is an input, and the session writes
nothing at all. Skills that compose this primitive with a write discipline
(grill-with-docs, and the wayfinding variant) carry their own writing rules;
none of them belong here, because an instruction that is absent cannot be
disobeyed.
-->

## Read the glossary, write nothing

Read the project's glossary before the first round and challenge the user's
terms against it. The glossary is the **union** of the base `CONTEXT.md` and
every delta fragment that touches it — computed in memory at read time, never
by editing anything. [CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md) has the union
rule, where fragments live, and the `+`/`~`/`-` op syntax you need to read them.
Unioning is read-only, so it stays safe with any number of sessions running in
parallel.

This skill writes nothing. No `CONTEXT.md` edit, no fragment, no ADR, no notes
file, no commit — not even a scratch file to hold the answers. The value it
produces is the shared understanding in the conversation.

If the decisions from this session need to be recorded, that is the job of
whichever skill composed this one. Ask the user which one they want rather than
picking a destination yourself.
