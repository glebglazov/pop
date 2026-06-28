---
status: accepted
relates: "supersedes ADR-0010"
---

# Integrate installs all components by default; consent is opt-out

`pop integrate <agent>` with no flags installs the full set — status wiring, the **Pane skill**, and the **Task planning skills** — without per-component prompting. Components are declined with `--no-pane-skill` / `--no-task-skills`; the old positive `--pane-skill` / `--task-skills` flags are deprecated to no-ops with a notice. A decline is **persisted as per-agent negative consent**, and **Integration refresh** now *adds* any default component that is missing and not opted-out (it previously only re-rendered already-installed components). A bare explicit `pop integrate <agent>` re-asserts the full set (clearing opt-outs); `--no-X` and `pop integrate remove` set them. This supersedes [ADR-0010](0010-integrate-is-a-per-component-consent-wizard.md).

## Why

ADR-0010 graded consent per component because a skill is behaviour injection and bundling it behind *integrate* smuggled a second consent in behind the first. In practice the prompting was friction on the common path — most users integrating an agent want the whole toolkit, and the per-component wizard made them earn it. Defaulting everything on with a cheap, persisted opt-out keeps the invasiveness escape hatch ADR-0010 cared about (decline once, stay declined) while making the common case zero-ceremony. The cost is new state — a per-agent opt-out set — and a refresh that can now add, not just update.

## Considered Options

- **Keep the wizard, default its prompts to Yes.** Rejected: still prompts on every run; the friction ADR-0010 introduced remains, just pre-answered.
- **Default-on but non-persisted opt-out (per-invocation `--no-*` only).** Rejected: refresh would silently re-add a component the user removed, since nothing records the decline.
- **Default-on but refresh stays refresh-only (never adds).** Rejected: a newly-shipped default component would never reach already-integrated agents without a manual re-integrate, undercutting "default-on".

## Consequences

- New persisted state: a per-agent set of opted-out component ids, consulted by both explicit integrate and refresh. `pop integrate remove` records the opt-out so refresh respects it.
- Refresh semantics widen: for each integrated agent it installs missing non-opted-out default components and refreshes installed ones. Conflicts (unowned same-named entries) are still skipped, never overwritten.
- The **Integration wizard** as a per-component consent flow is retired; `pop integrate` is now a non-prompting install of the resolved set.
- Not addressed here: orphaned skills from a component removed from the catalog entirely are still not auto-pruned (refresh iterates the catalog, so it never visits them). That remains a separate catalog-wide GC concern.
