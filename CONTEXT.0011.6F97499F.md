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
  A skill already present at an embedded skill's resolved install name (see **Skill prefix**) that pop does not recognise as its own (see **Pop-owned marker**). Pop never installs over, removes, or refreshes a conflicting skill; the wizard and health check report the conflict and leave resolution to the user.
  was: A skill already present at an agent's skill location under an embedded skill's name (with or without the `pop-` prefix) that pop does not own. Pop never installs over, removes, or refreshes a conflicting skill; the wizard and health check report the conflict and leave resolution to the user.
