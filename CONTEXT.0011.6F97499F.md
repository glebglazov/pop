---
fragment: 6F97499F
generation: 0011
branch: master
---

+ Skill prefix
  The configurable string prepended to an embedded skill's base name to form its installed name (`<prefix><base>`). Set via `skill_prefix` in `[integrations]`, default `pop-`; an empty value installs skills under their bare base name.
  avoid: pop- prefix, namespace
  under: Agent integrations

+ Pop-owned marker
  How pop recognises an installed artifact as its own, independent of the skill's name: a symlink resolving into pop's render tree, or — for copy-mode installs — a `pop-owned: true` frontmatter field written into the rendered skill. Replaces the legacy `pop-` name prefix as the ownership signal, so the **Skill prefix** can be empty without losing ownership detection.
  avoid: ownership convention, pop- name check
  under: Agent integrations

~ Integration conflict
  A skill already present at an embedded skill's resolved install name (see **Skill prefix**) that pop does not recognise as its own (see **Pop-owned marker**). Pop never installs over, removes, or refreshes a conflicting skill; integrate and the health check report the conflict and leave resolution to the user.
  was: A skill already present at an agent's skill location under an embedded skill's name (with or without the `pop-` prefix) that pop does not own. Pop never installs over, removes, or refreshes a conflicting skill; the wizard and health check report the conflict and leave resolution to the user.

~ Integration component
  An individually-installable unit `pop integrate` lands for one agent: the status wiring (core), the **Pane skill**, or the **Task planning skills**. `pop integrate <agent>` installs them all by default; each non-core component can be declined with a `--no-<component>` flag, and the decline is persisted (see **Component opt-out**).
  was: An individually consented unit `pop integrate` can install for one agent: the status wiring (core), the **Pane skill**, or the **Task planning skills**. Running integrate implies consent to the status wiring only; every other component is an explicit per-component opt-in.

~ Integration refresh
  The automatic re-render of installed **Integration components** when the pop binary changes; it also installs any default component that is missing and not opted-out, and leaves uninstalled agents alone. Never prompts; never re-adds an opted-out component (see **Component opt-out**).
  was: The automatic re-render of already-installed **Integration components** when the pop binary changes. Refresh never adds components the user did not opt into, never prompts, and leaves uninstalled agents alone.

+ Component opt-out
  Persisted per-agent negative consent: a declined **Integration component** is recorded so **Integration refresh** does not silently re-add it. Set by a `--no-<component>` flag or `pop integrate remove`; cleared by a bare `pop integrate <agent>`, which re-asserts the full default set.
  avoid: negative consent, decline list
  under: Agent integrations

- Integration wizard
