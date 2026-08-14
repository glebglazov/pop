---
name: grilling
description: Interview the user relentlessly about a plan or design until you reach a shared understanding — map the design tree, ask the whole settled frontier one round at a time, and find every fact yourself. Reads the project glossary as input and writes nothing. Use when a plan needs stress-testing without anything on disk being touched.
---

<!--
base: mattpocock/skills productivity/grilling@8b78b53

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a verbatim copy of productivity/grilling/SKILL.md at the pinned
ref above — the interview primitive on its own: design tree, frontier, rounds,
question format, find-facts-yourself. Pop inlines it rather than delegating to
`/grilling`, per ADR-0009 (skills are embedded in the binary and ship to
machines without Matt's skills installed). Upstream's frontmatter is replaced by
pop's; the local name is `grilling`, upstream's own: **agent-loaded**. This is the
interview primitive alone, and pop's `grill-with-map` composes over it for every
wayfinding ticket — a wayfinding session must be able to load both without a
human re-opening each one by hand, or every grilling ticket needs a manual
workaround just to start. The manual gate belongs to the sessions that commit
decisions to the repo (`grill-with-docs`, `wayfinder`, and the handoff skills
that close a map), not to the shared interview step underneath them.

**The upstream twin is `productivity/grilling`, not the in-progress experiment
this skill was first pinned to.** It was pinned to that in-progress experiment
(ref `fde4cd5`) on the reading that upstream's shipped grilling was committed
to one-question-at-a-time, so the batch variant looked abandoned and the pin
was treated as frozen. The opposite happened: upstream `a4b2009` reworked
shipped `grilling` to the round-by-round frontier model and deleted the
experiment, so the primitive pop inlines is now upstream's shipped skill. The
pin tracks a **live** upstream again — a new commit on `productivity/grilling`
is a real drift signal, not noise. To review drift, diff the region between
this header and the marker against productivity/grilling@<newref>.

The local name now matches upstream's own, `grilling` (ADR-0141's amendment):
the old local name credited that in-progress experiment, and the experiment no
longer exists — upstream absorbed it into shipped `productivity/grilling` and
deleted it. A local name that points at nothing is renamed to the live skill
it actually tracks.
-->

Interview the user relentlessly until you reach a shared understanding. Map this as a **design tree**: every decision branches into the decisions that hang off it.

Work the tree in **rounds**. The **frontier** is every decision whose prerequisites are already settled — the questions you can ask _now_ without guessing at answers you haven't heard yet. Ask the whole frontier in one round: number each question and give your recommended answer. Then wait for the user's answers before the next round.

Each question should be formatted like so:

```
❓ **Q1** - **<question title>**: <question body, might be multiple paragraphs, including multiple choices>

➡️ <your recommended answer>
```

Each round the user answers reshapes the tree — settled decisions push the frontier outward and unblock questions that depended on them. Recompute the frontier and ask the next round. A question whose answer depends on another question still open in this round belongs to a _later_ round, not this one.

Finding _facts_ is your job, never the user's. When a frontier question needs a fact from the environment (filesystem, tools, etc.), dispatch a sub-agent to find it — don't ask the user for anything you could look up yourself. Don't block on it: a running exploration is an unsettled prerequisite, so only the questions downstream of it wait for the sub-agent to report — ask the rest of the frontier now. The _decisions_ are the user's — put each to them and wait.

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
