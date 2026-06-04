# Integrate is a per-component consent wizard

`pop integrate <agent>` is a re-entrant **Integration wizard** over individually consented **Integration components**: the status wiring (hooks or agent extension) installs as the core component implied by running the command at all; the **Pane skill** and the **Workload planning skills** (with their global-gitignore sub-step) each require an explicit per-component opt-in, preceded by an explanation of what the component brings. **Integration refresh** only ever re-renders components the user previously opted into — it never adds, never prompts. Non-interactive runs require explicit component flags and fail without them rather than installing a default.

## Why

The dividing line is invasiveness from the user's point of view. Status wiring is plumbing: it makes the agent report pane status to the **Monitor** without changing how the agent behaves — "almost free", and exactly what the word *integrate* promises. A skill is behavior injection: it changes what the agent does, so it is a genuinely separate "do I want these opinionated skills?" decision. Bundling skills behind the integrate verb (as the previous design did with the pane skill) smuggled the second consent in behind the first. The wizard keeps one discoverable front door while grading consent per component, and re-running it any time is the upgrade path as users discover features progressively.

## Considered Options

- **Bundle all skills into `integrate <agent>` (status quo extended).** Rejected: anyone integrating just for pane monitoring would receive opinionated planning skills — a non-consensual install.
- **Per-feature setup commands (`pop pane setup`, `pop workload setup`, `pop monitor setup`).** Rejected: the status wiring is cross-cutting infrastructure consumed by multiple features (dashboard, pickers, activity sorting), so no feature namespace is its natural owner; and the wizard provides the same progressive opt-in with one surface instead of three.
- **A separate `pop skills add` distributor command.** Rejected: a second install surface duplicating integrate's per-agent fan-out and refresh machinery.

## Consequences

Components have stable identifiers (status wiring, pane skill, workload skills, workload gitignore) used by wizard steps, non-interactive flags, removal, and **Doctor** supporting evidence where command-family checks need integration state. An **Integration conflict** — a same-named skill at the agent's skill location that pop does not own — is never overwritten, removed, or refreshed; the wizard reports it and leaves resolution to the user. Removal is symmetric for pop-owned artifacts only; the global gitignore line is reported for manual removal rather than edited destructively. Agents that cannot host a component (opencode and codex for multi-file skills) are reported as not-supported rather than receiving a silently degraded install.
