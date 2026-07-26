---
status: accepted
---

# Planning skills publish through a Work-store seam, with pop as the seeded machine-global default

Matt Pocock's engineering skills externalize tracker specifics into a per-repo doc (`docs/agents/issue-tracker.md`) with per-operation sections; the skill bodies are tracker-agnostic. Pop's `to-tasks`, `to-prd`, and `wayfinder` instead hardcoded the pop pipeline. We restructure all three as marked overlays on their pinned upstream twins ([ADR-0112](0112-grill-skills-are-a-marked-overlay-on-pinned-domain-modeling.md) pattern; wayfinder already was one) — `to-tasks` on `to-tickets`, `to-prd` renamed **`to-spec`** on upstream `to-spec` — and move every pop-specific publish behaviour into one **Work store doc**. Resolution is two-layer: the repo-level `docs/agents/issue-tracker.md` (upstream convention, byte-compatible with `setup-matt-pocock-skills` output) wins when present; otherwise skills read the machine-global pop doc at `${XDG_CONFIG_HOME:-~/.config}/pop/work-store.md`, which Integration refresh seeds create-if-absent and **never overwrites** — user edits are the machine-global override. All five agent integrations (claude, codex, cursor, opencode, pi) resolve through the same two files.

## Considered Options

- **Per-skill bundled doc copies:** self-contained, but the only benefits (works without pop, no path resolution) are hollow — the pop Work store is unusable without pop, and the XDG path is deterministic. N copies also kill the user's one-file global override; refresh would stomp edits.
- **Managed blocks in per-agent global memory files (`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, …):** pop would own content inside five user-owned personal files, pay an every-session token tax, and inherit per-agent path quirks — for config used only during planning skills. Rejected.
- **A `pop work store-doc` CLI chokepoint:** collides at first letter with `show-path` (CLI naming rule) and adds a command where a deterministic path suffices. Rejected.
- **Two files, no command (chosen):** repo doc → seeded global doc. Accepted cost: once seeded, the global doc no longer tracks improvements to pop's embedded default — it is config, staleness is the user's prerogative.

## Consequences

- Pop-specific drafting vocabulary (effort, HITL/AFK typing) moves into the Work store doc's sections, not skill overlays — the overlays stay near-minimal so upstream drift review remains a mechanical above-marker diff. All three bases re-pin at one current upstream ref.
- Upstream `to-tickets`' "Quiz the user" step is dropped via an explicit overlay negation line (breakdown approval belongs to the planning session that precedes `to-tasks`), not by forking the base.
- The co-located set artifact renames `prd.md` → `spec.md` with **no backward-compat read** (amends [ADR-0088](0088-prd-colocates-into-the-task-set-folder.md)'s filename; co-location is unchanged). Skill `to-prd` becomes `to-spec`.
- `managed`/`auto-drain` invocation args are pop-store-only: when the resolved Work store is not pop, skills warn and ignore them, then publish to the configured store.
- A repo that explicitly wants pop (pinning against default changes, or for unmodified upstream skills' benefit) writes an `issue-tracker.md` that says so — no new repo-level filename or convention is introduced.
