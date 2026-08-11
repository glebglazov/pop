---
fragment: 3E038BED
generation: 0009
branch: master
---

+ Config override layer
  `config.override.toml` in the pop data dir: the pop-written config layer that
  beats **every** hand-authored source, holding whole-key values a human has
  deliberately overridden. The inverse of the runtime layer beside it — a
  gap-filler that loses to hand-authored config, keyed by checkout and by repo
  identity — so the two stay separate files rather than one file with two
  precedences. Machine-wide (`global` scope) and whole-key: an entry is the
  complete TOML value for one key, not a patch on it. Well-formed by
  construction: pop validates on write and never leaves a **Config finding** in
  a file it wrote itself.
  avoid: runtime override, config.runtime.toml, session override, config patch
  under: Configuration

+ Config dashboard
  The TUI that reads and writes the **Config override layer**: a searchable list
  of **Override-exposed key**s on the left, dotted path per row with its `desc`
  dimmed beneath and a marker on the overridden ones, and a preview on the right
  in config format — the effective TOML value, a provenance line naming the layer
  that produced it, and the source value dimmed below when an override exists.
  `Enter` seeds `$EDITOR` in place with the full `key = value` line, `ctrl+y`
  copies the source value, `ctrl+x` deletes the override. A self-contained `ui/`
  component rather than a page, because it opens with `alt+c` from three
  unrelated hosts — the **Work dashboard** shell and the project and worktree
  pickers — and while open it suspends its host's keys entirely and writes
  nothing to stdout, whose content is the pickers' return value. Pop's third
  dashboard, so it is never "the dashboard" unqualified.
  avoid: config editor, override picker, settings dashboard, the dashboard
  under: Configuration

+ Override-exposed key
  A config key a human may override from the **Config dashboard**, marked
  as such on its struct field by an `override:"<scope>"` tag whose value names
  the scope the override lands at — `global` today, `repo` reserved. Reflection
  over the tag is the whole registry, as `desc` already backs the key catalog;
  an unmarked key is invisible to the editor. Four keys are exposed at the cut,
  the `agents` list of each **Work agent group**.
  avoid: settable key, editable key, whitelist, exposed setting
  under: Configuration

~ Agent override
  A persisted replacement of one **Work agent group**'s whole ordered `agents`
  list, written to the **Config override layer** and edited through the **Config
  dashboard**. It is the list, not a promotion of one entry: the ordering
  a human leaves behind the head line *is* the tail **Agent fallback** walks, so
  there is no separate pin-versus-promote question. Removing the override
  restores the source list, which for `verify` and `routine` means their
  fallthrough to `implement`'s list — deliberately different from an override
  set to an empty array, which disables that fallthrough. Repeated `--agent`
  flags still beat it for one command.
  was: A session-lived promotion of one **Agent entry** to the head of its
    **Work group**, opened with `alt+a` from any Work dashboard page and gate
    menu, applied for one OS process and never persisted — promoting rather than
    pinning so the configured remainder stayed behind the picked entry, through
    a two-level numeric picker (group, then entry). Retired with its picker
    because a mechanic that lived one process and appeared nowhere it could be
    learned was not legible enough to be used (supersedes ADR-0196 decision 8).
