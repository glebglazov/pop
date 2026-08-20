---
name: to-tasks
description: Break a plan or spec into independently-grabbable work items written as local markdown files, binding the set to the worktree you run it in. Use when the user wants to convert a plan into tasks, create implementation tickets, or break down work into actionable items. Accepts `managed` (isolated pop-owned worktree) and `auto-drain` (queue drains it unattended) arguments.
disable-model-invocation: true
---

<!--
base: mattpocock/skills engineering/to-tickets@8b78b53

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream engineering/to-tickets/SKILL.md body
at the pinned ref above. Pop inlines rather than delegating to Matt's skills, per
ADR-0009 (skills are embedded in the binary and ship to machines without them
installed). Pop-authored frontmatter is kept (name `to-tasks`, pop description,
`disable-model-invocation`): **human-opened, deliberately**. `to-tasks` is one
of the two handoff skills (with `to-spec`) that end a wayfinding map — breaking
it into the tasks a map hands off to — and handoff is the moment a human judges
the map done. Making either handoff skill model-loadable would let an agent end
a map on its own, so the flag stays regardless of how agent-loadable the rest
of the map's skills become. Pop's
Work-store seam, quiz negation, invocation arguments, and the wayfinder-map
source live below the marker (ADR-0136, ADR-0169). To review upstream drift,
diff the region between this header and the marker against
engineering/to-tickets@<newref>.
-->

# To Tickets

Break a plan, spec, or conversation into a set of **tickets** — tracer-bullet vertical slices, each declaring the tickets that **block** it.

The issue tracker and triage label vocabulary should have been provided to you — run `/setup-matt-pocock-skills` if not.

## Process

### 1. Gather context

Work from whatever is already in the conversation context. If the user passes a reference (a spec path, an issue number or URL) as an argument, fetch it and read its full body and comments.

### 2. Explore the codebase (optional)

If you have not already explored the codebase, do so to understand the current state of the code. Ticket titles and descriptions should use the project's domain glossary vocabulary, and respect ADRs in the area you're touching.

Look for opportunities to prefactor the code to make the implementation easier. "Make the change easy, then make the easy change."

### 3. Draft vertical slices

Break the work into **tracer bullet** tickets.

<vertical-slice-rules>

- Each slice cuts a narrow but COMPLETE path through every layer (schema, API, UI, tests) — vertical, NOT a horizontal slice of one layer
- A completed slice is demoable or verifiable on its own
- Each slice is sized to fit in a single fresh context window
- Any prefactoring should be done first

</vertical-slice-rules>

Give each ticket its **blocking edges** — the other tickets that must complete before it can start. A ticket with no blockers can start immediately.

**Wide refactors are the exception to vertical slicing.** A **wide refactor** is one mechanical change — rename a column, retype a shared symbol — whose **blast radius** fans across the whole codebase, so a single edit breaks thousands of call sites at once and no vertical slice can land green. Don't force it into a tracer bullet; sequence it as **expand–contract**. First expand: add the new form beside the old so nothing breaks. Then migrate the call sites over in batches sized by blast radius (per package, per directory), each batch its own ticket blocked by the expand, keeping CI green batch to batch because the old form still exists. Finally contract: delete the old form once no caller remains, in a ticket blocked by every migrate batch. When even the batches can't stay green alone, keep the sequence but let them share an integration branch that all block a final integrate-and-verify ticket — green is promised only there.

### 4. Quiz the user

Present the proposed breakdown as a numbered list. For each ticket, show:

- **Title**: short descriptive name
- **Blocked by**: which other tickets (if any) must complete first
- **What it delivers**: the end-to-end behaviour this ticket makes work

Ask the user:

- Does the granularity feel right? (too coarse / too fine)
- Are the blocking edges correct — does each ticket only depend on tickets that genuinely gate it?
- Should any tickets be merged or split further?

Iterate until the user approves the breakdown.

### 5. Publish the tickets to the configured tracker

Publish the approved tickets. **How** depends on the tracker `/setup-matt-pocock-skills` configured — the tickets are the same either way, only the shape of the blocking edges changes:

- **Local files** → write one file per ticket under `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01` in dependency order (blockers first). Each file's "Blocked by" lists the numbers/titles it depends on. Use the per-ticket file template below — one ticket per file, never a single combined file.
- **A real issue tracker (GitHub, Linear, …)** → publish one issue per ticket in dependency order (blockers first) so each ticket's blocking edges can reference real identifiers. Use the platform's native blocking / sub-issue relationship where it has one; otherwise set each ticket's "Blocked by" to the blocking issues. Apply the `ready-for-agent` triage label unless instructed otherwise — the tickets are agent-grabbable by construction.

Work the **frontier**: any ticket whose blockers are all done. For a purely linear chain that means top to bottom.

Do NOT close or modify any parent issue.

<local-ticket-template>

# <NN> — <Ticket title>

**What to build:** the end-to-end behaviour this ticket makes work, from the user's perspective — not a layer-by-layer implementation list.

**Blocked by:** the numbers/titles of the tickets that gate this one, or "None — can start immediately".

**Status:** ready-for-agent

- [ ] Acceptance criterion 1
- [ ] Acceptance criterion 2

</local-ticket-template>

<issue-template>

## Parent

A reference to the parent issue on the tracker (if the source was an existing issue, otherwise omit this section).

## What to build

The end-to-end behaviour this ticket makes work, from the user's perspective — not layer-by-layer implementation.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2

## Blocked by

- A reference to each blocking ticket, or "None — can start immediately".

</issue-template>

In either form, avoid specific file paths or code snippets — they go stale fast. Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it and note briefly that it came from a prototype. Trim to the decision-rich parts — not a working demo, just the important bits.
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Everything below is pop-specific and replaces the tracker-doc seam in the
verbatim region above. It swaps upstream's `/setup-matt-pocock-skills` line and
its step-5 publish mechanics for pop's Work-store doc, negates the Quiz step,
and adds pop's invocation arguments (ADR-0136, ADR-0169). Where a line below
contradicts the verbatim upstream region, the line below wins; the upstream
text is kept byte-intact only so drift stays diffable.
-->

## Issue tracker doc resolution

Ignore upstream's "run `/setup-matt-pocock-skills`" line **and** its step-5
publish mechanics (templates, `ready-for-agent` label, `.scratch/…` paths).
Resolve the issue-tracker doc two-layer instead:

1. If the repo has `docs/agents/issue-tracker.md`, that doc wins.
2. Otherwise read `~/.agents/docs/issue-tracker.md`.
3. If neither exists, stop and tell the user no issue-tracker doc is
   configured — there is no further fallback.

Publish the tickets per the resolved doc's **"Publishing tickets"** section. That
section owns every publish mechanic — the task-markdown template, the `index.json`
manifest, the effort heuristic, HITL/AFK typing, `pop tasks register`, the
MALFORMED fix loop, and the whole-set drain suggestion. None of it is restated
here; consult the doc.

## Commit convention

*(Irreducible pop bit: the convention is resolved while the work is broken down,
not when a task commits. pop commits each task's work under the subject written
here **verbatim** — it renders nothing and reformats nothing at commit time — so
if this step is skipped the repository's grammar is lost for the whole set.)*

Resolve the convention once, before publishing, by asking pop:

```
pop conventions get commits
```

Do not derive the grammar yourself. The command resolves this repository's
answer — your own `~/.agents/docs/projects/<project>/commits.md`, else your own
`~/.agents/docs/commits.md`, else the repository's committed
`docs/agents/commits.md`, else pop's own shipped answer — and prints it with the
rank it came from, plus your overlay where you have written one. It always exits 0 and always prints rules to follow, so take
its output as resolved rather than weighing ranks yourself.

If what resolves is that the repository has **no discernible** commit convention,
write neither field: the set carries no convention and each task commits under
pop's built-in default format. Do not guess one.

When a convention resolves, write both fields into the set's `index.json`:

- each task's **`commit_subject`** — the final, literal subject line for that
  task's work, rendered *now* from that task's intent under the convention;
- the set-level **`commit_convention`** — the convention text itself, which is
  what renders a subject for a task spawned mid-drain (a Remediation).

This section owns the **discovery contract** only. Where those keys sit, how they
are spelled and what pop does when one is absent belong to `pop tasks
authoring-guide`; read it before writing the manifest.

**Non-pop stores.** Both fields are pop-manifest keys. When the resolved Work
store is not pop, skip this step entirely.

## No quiz

Skip upstream's **"### 4. Quiz the user"** step entirely — do not present the
breakdown for approval or iterate on it. Breakdown approval belongs to the
planning session that precedes `to-tasks`; publish without a second gate.

## Slice sizing

Sharpen upstream's "sized to fit in a single fresh context window": a slice is
sized for **one drain attempt under ~120 agent turns**, peaking under ~100k
context.

The budget is not a comfort limit. An attempt's token cost is the sum of its
context across every turn, and context only grows, so cost rises with the
*square* of turn count — measured drains show a 60-turn task at 2.7M tokens
against a 252-turn task at 29.5M. Two 120-turn slices cost roughly half of one
250-turn slice doing the same work, because each starts from a fresh context.

Split when a candidate slice would:

- cross layers that each need their own discovery pass (a CLI change *and* the
  desktop surface that consumes it),
- need interactive verification (browser driving, screenshot review) *on top of*
  non-trivial implementation — the verification is its own slice, blocked by the
  build,
- carry more than a handful of acceptance criteria whose checks don't share a
  single test command.

Vertical slicing still governs — a split must stay a complete path through the
layers it claims, never a horizontal layer split. When a slice genuinely cannot
go below the budget without going horizontal, leave it whole and say so in the
set; the budget yields to the tracer-bullet rule, not the reverse.

## Arguments

`to-tasks` reads optional keyword arguments from the invocation that map straight
to `pop tasks register` flags:

- **`managed`** / **`isolated`** → `--managed`
- **`auto-drain`** / **`drain`** → `--auto-drain`
- **`no-drain`** / **`manual`** → drops `--auto-drain`

That is the whole mapping. What each flag *means* at register time, what runs
when no keyword is typed at all, and how a keyword interacts with the default all
belong to the doc's "Publishing tickets" → "Register the set" section, which
states the default once, so this skill carries none of its own. Read that section
before registering.

**Non-pop stores.** These arguments are pop-store-only. When the resolved Work
store is **not** pop (a repo `docs/agents/issue-tracker.md` points at a real
tracker), warn the user, **ignore** all of them, and publish to the configured
store per that doc — do not attempt to plumb `managed`/`auto-drain` there.

## Wayfinder Map source

*(Irreducible pop bit: reading a Map as the breakdown source has no home in the
doc's publish sections — it is an input mode of `to-tasks`, not a publish
mechanic.)*

When invoked directly on a Map (the user names a map id, with no co-located
`spec.md` yet), read `$(pop work show-path)/maps/<map-id>/map.md` and each
**resolved** ticket under `issues/` — at minimum every ticket linked from
**Decisions so far**, plus any other resolved tickets whose `## Answer` should
inform the breakdown. Also read the map's `index.json`: it is where ticket status
and each ticket's `adr_drafts` / `context_drafts` live. Everything you then owe
the map — minting those drafts through the implementing slices, `source_map` on
the set, and `pop map spawned` after registering — is the doc's *Publishing
tickets* → *Map-sourced sets* section; follow it there.
