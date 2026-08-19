---
name: grilling
description: Interview the user relentlessly about a plan or design until you reach a shared understanding — map the design tree, ask the whole settled frontier one round at a time, and find every fact yourself. The interview alone; the skill that composes it owns where the decisions land. Use when a plan needs stress-testing without anything on disk being touched.
---

<!--
base: mattpocock/skills productivity/grilling@8b78b53

Everything below this comment is a verbatim copy of productivity/grilling/SKILL.md
at the pinned ref above, and this file has no pop overlay marker at all. Per ADR-0225 the
interview primitive owns the interview and nothing else: no glossary rule, no
CONTEXT-FORMAT.md companion, no write destination. A rule that is absent cannot
be disobeyed, and every rule pop used to keep here has an owner of its own now —
`domain-modeling` for the repository's glossary and ADRs, `grill-with-map` for a
Map's drafts. Pop inlines the upstream text rather than delegating to `/grilling`,
per ADR-0009 (skills are embedded in the binary and ship to machines without
Matt's skills installed).

Upstream's frontmatter is replaced by pop's; the local name is `grilling`,
upstream's own: **agent-loaded**. The workflows that compose it — `grill-with-docs`,
which loads it beside `domain-modeling`, and `grill-with-map` — must be able to
load it without a human opening it by hand, or every composed session needs a
manual workaround just to start. The manual gate belongs to the sessions that
commit decisions to the repo, not to the shared interview step underneath them.

Bare grilling therefore has upstream's behaviour, glossary included: it does not
read the domain model on its own. A session that reads or writes the model loads
the discipline that owns it, and an ordinary agent in a configured repository
gets the glossary from that repository's own domain instructions.

**The upstream twin is `productivity/grilling`, not the in-progress experiment
this skill was first pinned to.** It was pinned to that in-progress experiment
(ref `fde4cd5`) on the reading that upstream's shipped grilling was committed
to one-question-at-a-time, so the batch variant looked abandoned and the pin
was treated as frozen. The opposite happened: upstream `a4b2009` reworked
shipped `grilling` to the round-by-round frontier model and deleted the
experiment, so the primitive pop inlines is now upstream's shipped skill. The
pin tracks a **live** upstream again — a new commit on `productivity/grilling`
is a real drift signal, not noise. To review drift, diff everything below this
comment against productivity/grilling@<newref>.

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
