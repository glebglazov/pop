---
fragment: ed94b2da
generation: 0014
branch: master
---

+ Skills prefix
  The configurable string prepended to an embedded skill's base name to form its installed name (`<prefix><base>`). Set via `skills_prefix` in `[integrations]`, default `pop-`; an empty value installs skills under their bare base name.
  avoid: skill_prefix, pop- prefix, namespace
  under: Agent integrations

~ Pop-owned marker
  How pop recognises an installed artifact as its own, independent of the skill's name: a symlink resolving into pop's render tree, or — for copy-mode installs — a `pop-owned: true` frontmatter field written into every rendered skill. The legacy `pop-` name-prefix ownership check is retired; the **Skills prefix** can be empty without losing ownership detection for newly rendered skills.
  was: How pop recognises an installed artifact as its own, independent of the skill's name: a symlink resolving into pop's render tree, or — for copy-mode installs — a `pop-owned: true` frontmatter field written into every rendered skill. Replaces the legacy `pop-` name prefix as the ownership signal, so the **Skills prefix** can be empty without losing ownership detection.

+ Stale agent entry cleanup
  After integrate links a component's freshly rendered skill names at an agent location, pop removes any remaining pop-owned entries there whose names are no longer in that render set — typically leftovers from a prior **Skills prefix** or base-name change. Scoped per component; never removes unowned or foreign skills.
  avoid: prune stale, stale-name prune
  under: Agent integrations

~ Pane skill
  The embedded skill that teaches an agent to drive `pop pane`. Its embed base name is `tmux-pane` (installed as `<skills_prefix>tmux-pane`, or bare `tmux-pane` when the prefix is empty). Still selected in config via the **Integration skill alias** `pane`. An opt-in **Integration component**; pane monitoring works without it.
  was: The embedded skill that teaches an agent to drive `pop pane`. An opt-in **Integration component**; pane monitoring works without it. (CONTEXT.md)

~ Integration refresh
  Reconciling installed **Integration components** to the state pop now expects: it re-renders by resolved name (not just content), so **Skills prefix** or base-name changes are applied and stale old-named entries pruned; it installs any baseline-listed component that is missing and not opted-out; and it leaves uninstalled agents alone. Runs on the binary-revision-gated picker-launch path and on `pop integrate --update-existing`. Never prompts; never re-adds or updates an opted-out component.
  was: (see CONTEXT.0011.6F97499F — same reconcile-by-resolved-name intent; opt-out persistence now via runtime TOML per ADR-0065)
