<!--
base: mattpocock/skills skills/engineering/setup-matt-pocock-skills/issue-tracker-local.md@8b36d4f

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream
skills/engineering/setup-matt-pocock-skills/issue-tracker-local.md at the
pinned ref mattpocock/skills@8b36d4f. Pop
inlines the seed templates rather than delegating to Matt's skills
(ADR-0009); local markdown stays a selectable Section A tracker choice and
needs its seed. To review upstream drift, diff the region between this header
and the marker against
skills/engineering/setup-matt-pocock-skills/issue-tracker-local.md@<newref>.
-->

# Issue tracker: Local Markdown

Issues and specs for this repo live as markdown files in `.scratch/`.

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- The spec is `.scratch/<feature-slug>/spec.md`
- Implementation issues are one file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01` — never a single combined tickets file
- Triage state is recorded as a `Status:` line near the top of each issue file (see `triage-labels.md` for the role strings)
- Comments and conversation history append to the bottom of the file under a `## Comments` heading

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/` (creating the directory if needed).

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or the issue number directly.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a file with one **child** file per ticket.

- **Map**: `.scratch/<effort>/map.md` — the Notes / Decisions-so-far / Fog body.
- **Child ticket**: `.scratch/<effort>/issues/NN-<slug>.md`, numbered from `01`, with the question in the body. A `Type:` line records the ticket type (`research`/`prototype`/`grilling`/`task`); a `Status:` line records `claimed`/`resolved`.
- **Blocking**: a `Blocked by: NN, NN` line near the top. A ticket is unblocked when every file it lists is `resolved`.
- **Frontier**: scan `.scratch/<effort>/issues/` for files that are open, unblocked, and unclaimed; first by number wins.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under an `## Answer` heading, set `Status: resolved`, then append a context pointer (gist + link) to the map's Decisions-so-far in `map.md`.
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Pop carries no delta to this seed — the upstream local-markdown tracker
template applies verbatim. The region is kept present (header + marker) only
so drift stays diffable; there is no pop-specific content below.
-->
