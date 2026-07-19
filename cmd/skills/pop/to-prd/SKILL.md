---
name: to-prd
description: Turn the current conversation context into a PRD and write it as a local markdown file. Use when the user wants to create a PRD from the current context.
disable-model-invocation: true
---

# To PRD

This skill takes the current conversation context and codebase understanding and produces a PRD as a local markdown file. Do NOT interview the user — just synthesize what you already know.

## Process

1. Explore the repo to understand the current state of the codebase, if you haven't already. Use the project's domain glossary vocabulary throughout the PRD, and respect any ADRs in the area you're touching.

**Wayfinder Map source:** When the breakdown source is a Map (the user names a map id, or the session is handing off from wayfinder), read `$(pop work show-path)/wayfinder/<map-id>/map.md` and each **resolved** ticket under `issues/` — at minimum every ticket linked from **Decisions so far**, plus any other resolved tickets whose `## Answer` should inform the PRD. Synthesize from the map's Destination, **Decisions so far**, and those answers alongside conversation context; you need not load open or unresolved tickets.

2. Sketch out the major modules you will need to build or modify to complete the implementation. Actively look for opportunities to extract deep modules that can be tested in isolation.

A deep module (as opposed to a shallow module) is one which encapsulates a lot of functionality in a simple, testable interface which rarely changes.

Check with the user that these modules match their expectations. Check with the user which modules they want tests written for.

3. Write the PRD using the template below, then save it as `prd.md` **inside its own task-set folder** (ADR-0088), co-located with the task files that to-tasks will add later: `<tasks-dir>/<task-set-name>/prd.md`, where `<tasks-dir>` is `$(pop work show-path)/tasks` — run `pop work show-path` for the storage root and append `/tasks`, or equivalently run `pop tasks show-path` (same directory; ADR-0130 compatibility alias). Create the `<tasks-dir>/<task-set-name>/` directory now — it holds only `prd.md` at this stage; the set stays inert (invisible to the dashboard, never scheduled) until it is later registered with `pop tasks register`.

When the source is a Map, include `Source map: <map-id>` as the first line of `prd.md` (before the template headings), then append `<task-set-name>` under the map's `## Spawned sets` section in `map.md` (create the section if absent) — the forward link both ways (ADR-0129).

`<task-set-name>` is `<timestamp>-<slug>`, where `<slug>` is a descriptive hyphen-delimited name (e.g. `user-auth`). The slug carries over to the task set when this PRD is later broken down with to-tasks — to-tasks fills in the task markdown and `index.json` alongside this `prd.md`.

<naming-convention>
`<timestamp>` is a human-readable local date/time prefix so task sets sort chronologically:

- Default: `YYYY-MM-DD` (e.g. `2026-05-31`)
- If a folder with the same date and slug already exists: `YYYY-MM-DD-HHMM` (24-hour local time, e.g. `2026-05-31-2036`)

Examples: `2026-05-31-user-auth/prd.md`, `2026-05-31-2036-user-auth/prd.md`
</naming-convention>

<prd-template>

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

A description of the things that are out of scope for this PRD.

## Further Notes

Any further notes about the feature.

</prd-template>
