---
status: accepted
---

# The issue tracker doc resolves at a vendor-neutral user path

## Context

[ADR-0136](0136-planning-skills-publish-through-a-work-store-seam.md) gave the planning skills a two-layer seam: a repo-level `docs/agents/issue-tracker.md` wins when present, otherwise the skills read pop's own adapter doc. [ADR-0150](0150-the-config-dir-holds-only-hand-authored-files.md) moved that doc to a **Shipped asset** at `${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md`, rewritten on every Integration refresh, and removed the machine-global override.

Two things are wrong with the second layer as it stands. It is **named after pop** and **located inside pop's data dir**, so four skill bodies — `to-tasks`, `to-spec`, `wayfinder`, `setup-matt-pocock-skills` — hardcode a pop path and are therefore only correct when pop is the thing running them. These skills are otherwise vendor-neutral: they describe breaking work into slices and publishing it, and nothing in that is pop-specific. And the two layers disagree on filename — `docs/agents/issue-tracker.md` at repo scope, `work-store.md` at machine scope — for one document that plays one role.

Meanwhile the user-level agent-config convention already exists on disk, independent of pop: `~/.agents/` holds `AGENTS.md` and `skills/`, and each agent CLI symlinks into it (`~/.claude/CLAUDE.md` → `~/.agents/AGENTS.md`). A vendor-neutral home for the second layer is not a new idea to invent, only one to adopt.

ADR-0150 rejected a machine-global override as "unused, re-addable later". The path below re-adds the *capability* as a by-product rather than as a feature: once the user layer is a symlink pop declines to overwrite, anything already sitting at that path is the override.

## Decision

- **Both layers are named `issue-tracker.md`.** Repo scope stays `docs/agents/issue-tracker.md`. Machine scope becomes `~/.agents/docs/issue-tracker.md`. The glossary term is renamed to match: **Issue tracker doc**, retiring **Work store doc**. **Work store** keeps its meaning as the destination abstraction and keeps "issue tracker" on its `_Avoid_` list *as the abstraction's name* — the phrase is sanctioned only for the document and its filename.
- **Skill bodies name only those two paths.** Resolution reads: repo doc if present, else `~/.agents/docs/issue-tracker.md`, else stop and report that no issue-tracker doc is configured. No third fallback to pop's data dir, no "run `pop integrate`" hint, no mention of pop at all in `to-tasks`, `to-spec`, and `wayfinder`. `setup-matt-pocock-skills` stays pop-aware — it is the skill that *chooses* the Work store, so it must be able to explain the default — but its machine-layer sentence points at the vendor-neutral path.
- **Pop's asset moves to `~/.local/share/pop/agents/docs/issue-tracker.md`**, mirroring the user-level layout. It remains a Shipped asset under ADR-0150's contract: Integration refresh rewrites it whenever its bytes differ from the embedded copy.
- **Integration refresh creates the user-level symlink, create-if-absent.** It creates `~/.agents/docs/` (0755) when missing, then symlinks `~/.agents/docs/issue-tracker.md` → the asset path, emitting one Integrate outcome line. It does nothing when anything already occupies either path: a regular file, a directory, or a symlink pointing elsewhere is left untouched, including a dangling link to pop's own asset (the target reappears on the same refresh that seeds it). This is a deliberate, narrow exception to ADR-0150's "pop writes only under its own data dir" — the write is a link, never content, and it never overwrites.
- **The override is whatever occupies that path.** A user who wants different machine-wide publish behaviour writes a regular file at `~/.agents/docs/issue-tracker.md`, or links it at another store's doc. Pop then never touches it. This softens ADR-0150's "there is no machine-global override" without reintroducing the staleness it feared: pop's *content* still tracks the binary byte-for-byte, and an override is now an explicit act by the user rather than an editable copy pop seeded for them.
- **Stale paths are deleted on refresh, unconditionally**, one outcome line each: `~/.local/share/pop/work-store.md` (this rename) and `~/.config/pop/work-store.md` (ADR-0150's, retained).

## Considered Options

- **Keep the machine layer inside pop's data dir and merely rename it.** Rejected: the rename fixes the filename disagreement but leaves the skills quoting a pop path, which is the reason they cannot be lifted out of pop.
- **`${XDG_CONFIG_HOME:-~/.config}/agents/issue-tracker.md`** as the vendor-neutral path. XDG-correct, but no agent CLI reads it today, whereas `~/.agents/` is already the live convention on the machine this seam serves. Rejected as correctness nobody would benefit from.
- **An env seam (`$AGENTS_HOME`) for the user-level root.** Rejected: one more resolution layer that nobody sets. `~/.agents` is hardcoded, reached through the existing `userHomeDir` deps seam so tests can redirect it.
- **Pop writes the real file at `~/.agents/docs/issue-tracker.md`, with no data-dir copy.** Simpler — no symlink at all. Rejected: it puts pop-authored content outside pop's data dir permanently, which is exactly what ADR-0150 forbade, and makes "did the user edit this?" unanswerable again.
- **A third, silent fallback to the data-dir asset when the symlink is missing.** Rejected: it makes the stated two-layer rule a lie and hides a broken install behind a path the skill text does not mention.
- **The dotfile manager owns the symlink** (a chezmoi `symlink_` entry). Rejected as the sole mechanism: it works on one machine and leaves every other install with a dead second layer. The dotfile repo instead *ignores* `~/.agents/docs` so the two never fight.

## Consequences

- The old path is quoted in four skill bodies, in `CLAUDE.md`/`AGENTS.md`, in `docs/agents/navigation.md`, and in the doc's own opening paragraph — all rewritten together, along with the skills' `## Work store resolution` heading, which becomes `## Issue tracker doc resolution`.
- The embedded source file `integrate/work-store.md` is renamed to `integrate/issue-tracker.md`, with its `//go:embed` directive and the `workStoreDocPath` / `seedWorkStoreDoc` / `removeLegacyWorkStoreDoc` seam in `integrate/deps.go` following.
- `to-tasks`'s `managed` / `auto-drain` arguments stay pop-specific and stay gated on the resolved store being pop's — de-popping the resolution text does not de-pop the flags, which already warn and no-op against a non-pop store.
- A machine that has never run Integration refresh has no second layer, so a repo without `docs/agents/issue-tracker.md` fails loudly at publish time. That is the intended trade for dropping the silent fallback.
