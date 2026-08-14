# Pop tracks only the upstream skills it already ships

[ADR-0009](0009-planning-skills-are-embedded-in-the-binary.md) says pop embeds copies of Matt Pocock's skills so it never depends on his set being installed. This is the same boundary read from the other side, and it decides what an upstream-refresh pass is allowed to pull in: **pop embeds an upstream skill only when it already ships it — refresh the pin, review the above-marker drift — or when a pop-shipped skill's body names it, in which case it must be embedded too.** Everything else upstream ships is deliberately not mirrored, and what a developer has hand-installed in their own `~/.claude/skills` is outside pop's concern entirely. As of this decision no pop-shipped skill names an unshipped upstream skill, so the second clause adopts nothing today; it exists so a future skill body cannot reference `/codebase-design` (or any other upstream skill) without the embed that keeps ADR-0009's self-containment true.

## Considered Options

- **Mirror upstream wholesale on each refresh:** keeps parity with a set that is actively developed, but every skill lands in the same agent skill namespace and is re-billed in every session's context, and pop would be shipping workflow skills (`implement`, `triage`, `tdd`) that compete with its own task machinery.
- **Adopt case-by-case with no rule:** what we did until now; each refresh re-litigates the same question, and drift-by-mirroring creeps in one skill at a time.
- **Ship-set plus named references (chosen):** the embedded set only grows when a pop skill body creates a real dependency, which is exactly when ADR-0009 forces the embed anyway.

## Consequences

- An upstream refresh is a bounded job: diff the pinned refs of the skills in `overlayPinnedFiles` and the other marked overlays, bump the headers, done. The count of skills to review does not grow with upstream's catalog.
- Reviewing a pop skill body means checking its skill references resolve inside pop's own set. A reference to an unshipped upstream skill is a defect, not a convenience.
- The list is expected to change as pop's set changes; the rule is about *how* it changes, not which names are on it.
