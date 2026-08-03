---
name: to-spec
description: Turn the current conversation context into a spec and write it as a local markdown file. Use when the user wants to create a spec from the current context.
disable-model-invocation: true
---

<!--
base: mattpocock/skills engineering/to-spec@ed37663cc5fbef691ddfecd080dff42f7e7e350d

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream engineering/to-spec/SKILL.md body at
the pinned ref above. Pop inlines rather than delegating to Matt's skills, per
ADR-0009 (skills are embedded in the binary and ship to machines without them
installed). Pop-authored frontmatter is kept (name `to-spec`, pop description,
`disable-model-invocation`: to-spec is a manual-only planning skill). Pop's
Work-store seam and the wayfinder-map source live below the marker (ADR-0136,
ADR-0169). To review upstream drift, diff the region between this header and
the marker against engineering/to-spec@<newref>.
-->

This skill takes the current conversation context and codebase understanding and produces a spec (you may know this document as a PRD). Do NOT interview the user — just synthesize what you already know.

The issue tracker and triage label vocabulary should have been provided to you — run `/setup-matt-pocock-skills` if not.

## Process

1. Explore the repo to understand the current state of the codebase, if you haven't already. Use the project's domain glossary vocabulary throughout the spec, and respect any ADRs in the area you're touching.

2. Sketch out the seams at which you're going to test the feature. Existing seams should be preferred to new ones. Use the highest seam possible. If new seams are needed, propose them at the highest point you can. The fewer seams across the codebase, the better - the ideal number is one.

Check with the user that these seams match their expectations.

3. Write the spec using the template below, then publish it to the project issue tracker. Apply the `ready-for-agent` triage label - no need for additional triage.

<spec-template>

## Problem Statement

The problem that the user is facing, from the user's perspective.

## Solution

The solution to the problem, from the user's perspective.

## User Stories

A LONG, numbered list of user stories. Each user story should be in the format of:

1. As an <actor>, I want a <feature>, so that <benefit>

<user-story-example>
1. As a mobile bank customer, I want to see balance on my accounts, so that I can make better informed decisions about my spending
</user-story-example>

This list of user stories should be extremely extensive and cover all aspects of the feature.

## Implementation Decisions

A list of implementation decisions that were made. This can include:

- The modules that will be built/modified
- The interfaces of those modules that will be modified
- Technical clarifications from the developer
- Architectural decisions
- Schema changes
- API contracts
- Specific interactions

Do NOT include specific file paths or code snippets. They may end up being outdated very quickly.

Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it within the relevant decision and note briefly that it came from a prototype. Trim to the decision-rich parts — not a working demo, just the important bits.

## Testing Decisions

A list of testing decisions that were made. Include:

- A description of what makes a good test (only test external behavior, not implementation details)
- Which modules will be tested
- Prior art for the tests (i.e. similar types of tests in the codebase)

## Out of Scope

A description of the things that are out of scope for this spec.

## Further Notes

Any further notes about the feature.

</spec-template>
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Everything below is pop-specific and replaces the tracker-doc seam in the
verbatim region above. It swaps upstream's `/setup-matt-pocock-skills` line and
its step-3 publish mechanics for pop's Work-store doc, and adds the
wayfinder-map source (ADR-0136, ADR-0169). Where a line below contradicts the
verbatim upstream region, the line below wins; the upstream text is kept
byte-intact only so drift stays diffable.
-->

## Issue tracker doc resolution

Ignore upstream's "run `/setup-matt-pocock-skills`" line **and** its step-3
publish mechanics (publish to the tracker, apply the `ready-for-agent` label).
Resolve the issue-tracker doc two-layer instead:

1. If the repo has `docs/agents/issue-tracker.md`, that doc wins.
2. Otherwise read `~/.agents/docs/issue-tracker.md`.
3. If neither exists, stop and tell the user no issue-tracker doc is
   configured — there is no further fallback.

Publish the spec per the resolved doc's **"Publishing a spec"** section. That
section owns every publish mechanic — the co-located `spec.md` path, the
`<task-set-name>` naming convention, the inert-until-registered rule, and the
Map back-link. None of it is restated here; consult the doc.

## Wayfinder Map source

*(Irreducible pop bit: reading a Map as the spec source has no home in the doc's
publish sections — it is an input mode of `to-spec`, not a publish mechanic. The
doc's "Publishing a spec" section owns the reverse direction: the `Source map:`
line. The map's own record of the set is written later, by `to-tasks`, per the
doc's "Map-sourced sets" section — never by hand.)*

When the breakdown source is a Map (the user names a map id, or the session is
handing off from wayfinder), read `$(pop work show-path)/maps/<map-id>/map.md`
and each **resolved** ticket under `issues/` — at minimum every ticket linked
from **Decisions so far**, plus any other resolved tickets whose `## Answer`
should inform the spec. Synthesize from the map's Destination, **Decisions so
far**, and those answers alongside conversation context; you need not load open
or unresolved tickets.
