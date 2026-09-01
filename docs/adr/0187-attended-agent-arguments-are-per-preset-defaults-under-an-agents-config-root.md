---
status: superseded by ADR-0195
relates: "carves an attended-only exception out of the flags-come-last rule of [ADR-0017](0017-agent-preset-augmentation-rides-on-agent-flag.md), adds a capability in the shape [ADR-0166](0166-invocation-shape-capabilities-move-onto-the-preset-spec.md) established, and picks a config root beside the verb sub-tables of [ADR-0092](0092-task-config-parented-under-tasks-with-verb-named-sub-tables.md)"
---

# Attended agent arguments are per-preset defaults under an `[agents]` config root

> **Superseded by [ADR-0195](0195-an-attended-entry-owns-its-whole-invocation.md):**
> attended sessions gained a list of their own
> ([ADR-0194](0194-agent-lists-are-grouped-by-kind-of-work-and-attended-is-one-of-the-groups.md)),
> so their arguments and model now live in that list's entries rather than in
> `[agents.<preset>].attended_args` / `.attended_model`, which are removed — a
> per-preset key could hold only one attended configuration per agent, and two
> entries naming the same agent with different models is the first thing a
> human wants. This ADR's principles survive: one chokepoint at
> `ResolveAgentAssistanceInvocation`, and the human at the terminal owning
> their permission posture, now expressed as declared flags appended only where
> the entry's `cmd` does not already name them.
