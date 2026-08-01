---
name: setup-matt-pocock-skills
description: Set up this repo for the pop task-planning skills — choose a work store and lay out the domain docs. Run once, manually, before first use of the planning skills.
disable-model-invocation: true
---

<!--
base: mattpocock/skills engineering/setup-matt-pocock-skills@ed37663cc5fbef691ddfecd080dff42f7e7e350d

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream engineering/setup-matt-pocock-skills/SKILL.md
body at the pinned ref above. Pop inlines rather than delegating to Matt's
skills, per ADR-0009 (skills are embedded in the binary and ship to machines
without them installed). The name is kept upstream-verbatim — pop honors the
flow's origin rather than renaming it (ADR-0141) — with a pop description and
`disable-model-invocation` (this is a manual-only setup session). Pop's
Work-store seam and its trims to upstream's tracker/triage/scaffolding steps
live below the marker (ADR-0136, ADR-0141, ADR-0169). To review upstream
drift, diff the region between this header and the marker against
engineering/setup-matt-pocock-skills@<newref>.
-->

# Setup Matt Pocock's Skills

Scaffold the per-repo configuration that the engineering skills assume:

- **Issue tracker** — where issues live (GitHub by default; local markdown is also supported out of the box)
- **Triage labels** — the strings used for the five canonical triage roles
- **Domain docs** — where `CONTEXT.md` and ADRs live, and the consumer rules for reading them

This is a prompt-driven skill, not a deterministic script. Explore, present what you found, confirm with the user, then write.

## Process

### 1. Explore

Look at the current repo to understand its starting state. Read whatever exists; don't assume:

- `git remote -v` and `.git/config` — is this a GitHub repo? Which one?
- `AGENTS.md` and `CLAUDE.md` at the repo root — does either exist? Is there already an `## Agent skills` section in either?
- `CONTEXT.md` and `CONTEXT-MAP.md` at the repo root
- `docs/adr/` and any `src/*/docs/adr/` directories
- `docs/agents/` — does this skill's prior output already exist?
- `.scratch/` — sign that a local-markdown issue tracker convention is already in use
- Is the `triage` skill installed? (a `triage` skill folder alongside this one, or `triage` in your available skills.) This decides whether Section B runs at all.
- Monorepo signals — a `pnpm-workspace.yaml`, a `workspaces` field in `package.json`, or a populated `packages/*` with its own `src/`. Present only in a genuinely large multi-package repo; their absence means single-context, which is almost every repo.

### 2. Present findings and ask

Summarise what's present and what's missing. Then take the sections in order — one section, one answer, then the next.

Lead each section with the recommended answer so the user can accept it in a word. Give a one-line explainer only when the choice genuinely branches; skip the section entirely when exploration already settled it (Section B when `triage` isn't installed, Section C when there's no monorepo).

**Section A — Issue tracker.**

> Explainer: The "issue tracker" is where issues live for this repo. Skills like `to-tickets`, `triage`, `to-spec`, and `qa` read from and write to it — they need to know whether to call `gh issue create`, write a markdown file under `.scratch/`, or follow some other workflow you describe. Pick the place you actually track work for this repo.

Default posture: these skills were designed for GitHub. If a `git remote` points at GitHub, propose that. If a `git remote` points at GitLab (`gitlab.com` or a self-hosted host), propose GitLab. Otherwise (or if the user prefers), offer:

- **GitHub** — issues live in the repo's GitHub Issues (uses the `gh` CLI)
- **GitLab** — issues live in the repo's GitLab Issues (uses the [`glab`](https://gitlab.com/gitlab-org/cli) CLI)
- **Local markdown** — issues live as files under `.scratch/<feature>/` in this repo (good for solo projects or repos without a remote)
- **Other** (Jira, Linear, etc.) — ask the user to describe the workflow in one paragraph; the skill will record it as freeform prose

Record the choice in `docs/agents/issue-tracker.md`. The GitHub and GitLab templates carry a "PRs as a request surface" flag, defaulted **off** — leave it off and don't raise it; a user who wants external PRs in the triage queue can flip the flag in the file later.

**Section B — Triage label vocabulary.** Skip this section entirely if the `triage` skill isn't installed (exploration told you) — an uninstalled skill needs no labels.

If it is installed, ask exactly one question:

> Do you want to keep the default triage labels? (recommended: **yes**)

The defaults are the five canonical roles, each label string equal to its name: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. On **yes**, write them as-is. Only if the user says no — usually because their tracker already uses other names (e.g. `bug:triage` for `needs-triage`) — collect the overrides so `triage` applies existing labels instead of creating duplicates.

**Section C — Domain docs.** Default to **single-context** — one `CONTEXT.md` + `docs/adr/` at the repo root. This fits almost every repo; write it without asking.

Offer **multi-context** — a root `CONTEXT-MAP.md` pointing to per-context `CONTEXT.md` files — only when exploration found monorepo signals. Then confirm which layout they want.

### 3. Confirm and edit

Show the user a draft of:

- The `## Agent skills` block to add to whichever of `CLAUDE.md` / `AGENTS.md` is being edited (see step 4 for selection rules)
- The contents of `docs/agents/issue-tracker.md`, `docs/agents/domain.md`, and `docs/agents/triage-labels.md` (the last only when `triage` is installed)

Let them edit before writing.

### 4. Write

**Pick the file to edit:**

- If `CLAUDE.md` exists, edit it.
- Else if `AGENTS.md` exists, edit it.
- If neither exists, ask the user which one to create — don't pick for them.

Never create `AGENTS.md` when `CLAUDE.md` already exists (or vice versa) — always edit the one that's already there.

If an `## Agent skills` block already exists in the chosen file, update its contents in-place rather than appending a duplicate. Don't overwrite user edits to the surrounding sections.

The block:

```markdown
## Agent skills

### Issue tracker

[one-line summary of where issues are tracked]. See `docs/agents/issue-tracker.md`.

### Triage labels

[one-line summary of the label vocabulary]. See `docs/agents/triage-labels.md`.

### Domain docs

[one-line summary of layout — "single-context" or "multi-context"]. See `docs/agents/domain.md`.
```

Include the `### Triage labels` sub-block, and write `docs/agents/triage-labels.md`, only when `triage` is installed and Section B ran. When it isn't, both are omitted.

Then write the docs files using the seed templates in this skill folder as a starting point:

- [issue-tracker-github.md](./issue-tracker-github.md) — GitHub issue tracker
- [issue-tracker-gitlab.md](./issue-tracker-gitlab.md) — GitLab issue tracker
- [issue-tracker-local.md](./issue-tracker-local.md) — local-markdown issue tracker
- [triage-labels.md](./triage-labels.md) — label mapping (only if `triage` is installed)
- [domain.md](./domain.md) — domain doc consumer rules + layout

For "other" issue trackers, write `docs/agents/issue-tracker.md` from scratch using the user's description.

### 5. Done

Tell the user the setup is complete and which engineering skills will now read from these files. Mention they can edit `docs/agents/*.md` directly later — re-running this skill is only necessary if they want to switch issue trackers or restart from scratch.
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Everything below is pop-specific and replaces the parts of the verbatim region
above that assume Matt's full skill set. Pop ships no `triage` skill and never
writes into a repo's `.pop/` tree, so the deltas below (ADR-0141) trim
upstream's Section A tracker menu, negate Section B, replace step 4's
instruction-file selection with the `AGENTS.md` + `CLAUDE.md`-symlink layout every
agent CLI pop drains through can read, and forbid `.pop/`
scaffolding — while keeping upstream's `docs/agents/domain.md` write and the
`## Agent skills` block, the only discovery path a repo-resident agent has.
Where a line below contradicts the verbatim upstream region, the line below
wins; the upstream text is kept byte-intact only so drift stays diffable.
-->

## Issue tracker doc resolution

In **Section A**, add pop's Work store as a first-class option alongside
GitHub / GitLab / Local markdown / Other:

- **pop (machine work store)** — issues, specs, and task sets live in pop's
  per-machine Work store, resolved at run time by the planning skills. Good when
  the same machine drives many repos and you don't want a tracker choice
  committed into each one.

When the user picks pop, **write NO repo `docs/agents/issue-tracker.md` by
default.** An absent repo doc is the signal: the planning skills fall back to
the machine-level `~/.agents/docs/issue-tracker.md`, so each machine resolves
its own store. Do not synthesize a repo tracker doc from a template — there is
no pop tracker template, and a committed doc would override that machine-level
default for everyone.

Offer, as a **one-line option only**, committing a repo doc to pin the choice
for the whole team:

> Want to pin pop as the tracker for everyone who clones this repo? I can
> commit a one-line `docs/agents/issue-tracker.md` that names pop; otherwise I
> leave it absent and each machine uses its own
> `~/.agents/docs/issue-tracker.md`.

Only write the repo doc if the user says yes. Silence means leave it absent.

## Section B — negated

Pop ships **no `triage` skill**, so Section B never runs and no triage labels
exist. Skip the "Do you want to keep the default triage labels?" question
entirely, never write `docs/agents/triage-labels.md`, and omit the
`### Triage labels` sub-block from the `## Agent skills` block (Section 4). The
upstream Section B and its label vocabulary are kept above only so upstream
drift stays diffable — treat them as dead text here.

Consequently this skill folder ships **no `triage-labels.md` seed template** —
its omission is deliberate, not the missing-companion bug that 02 caught. The
`./triage-labels.md` link in the verbatim region above is dead by design: with
Section B negated the template is unreachable, so unlike `domain.md` and the
three `issue-tracker-*.md` seeds it is intentionally not embedded.

## Step 4 file selection — `AGENTS.md` is the file, `CLAUDE.md` the symlink

Upstream's step 4 ("if `CLAUDE.md` exists edit it, else `AGENTS.md`, never create
the other, ask when neither exists") is **replaced**. Do not ask which name to
use. The layout is always:

- `AGENTS.md` — the real file, holding the `## Agent skills` block.
- `CLAUDE.md` — a committed symlink to `AGENTS.md`.

Because pop drains attempts through several agent CLIs and they disagree on the
name, verified on installed versions:

| CLI | Reads at the repo root |
| --- | --- |
| claude 2.1.220 | `CLAUDE.md` only — a project-root `AGENTS.md` is invisible to it |
| codex | `AGENTS.md` |
| kimi | `AGENTS.md` / `agents.md` only |
| pi 0.77.0 | first match of `AGENTS.md`, `CLAUDE.md` |
| opencode 1.18.4 | first match of `AGENTS.md`, `CLAUDE.md`, `CONTEXT.md` |

Either name alone is a blind spot for some of them. The symlink is one file under
two names, so it cannot drift, and the two CLIs that would take both stop at the
first match — no double-load, no duplicated context cost.

How to get there from what exploration found:

- **Neither exists** — write `AGENTS.md`, then `ln -s AGENTS.md CLAUDE.md` and
  stage both. In a git repo the symlink must be committed (mode `120000`), or
  clones lose it.
- **Only `CLAUDE.md`, a regular file** — `git mv CLAUDE.md AGENTS.md`, retitle its
  heading, then `ln -s AGENTS.md CLAUDE.md`. Preserve every existing section;
  this is a rename, not a rewrite.
- **Only `AGENTS.md`** — edit it, add the symlink.
- **`CLAUDE.md` already a symlink to `AGENTS.md`** — nothing to do but edit
  `AGENTS.md`.
- **Both exist as regular files** — do not guess. Show the user both and ask which
  content survives before collapsing them; divergent instruction files are the
  failure this layout exists to prevent.

Keep the file short. It is read on turn one of every session in the repo, so
depth belongs in `docs/agents/*.md` with a one-line pointer here.

## Never scaffold `.pop/`

Do not create a `.pop/` directory or any file under it. Pop's per-repo files
(project routines and their state) are created lazily by pop itself on first use
(ADR-0137); this setup skill must not pre-seed them. Setup only touches
`docs/agents/domain.md` (kept) and the `## Agent skills` block (kept), plus the
optional pinned tracker doc above.
