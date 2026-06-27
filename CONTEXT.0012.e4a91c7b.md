---
fragment: e4a91c7b
generation: 0012
branch: master
---

~ Component opt-out
  Declining an optional **Integration component** by removing it from the global `skills` list in **Integration runtime config** (the middle config layer). Set by `--no-<component>` or `pop integrate remove`; cleared when bare `pop integrate <agent>` drops the runtime override and the merged config re-inherits pop defaults. **Integration baseline** in user config outranks runtime — editing `skills` there solidifies the set. Opt-out is global: declining pane applies to every agent, not one.
  was: A per-agent, persisted subtractive override below **Integration baseline**: a declined **Integration component** recorded so integrate and **Integration refresh** treat it as excluded until cleared. Set by `--no-<component>`, `pop integrate remove`, or cleared by bare `pop integrate <agent>` re-asserting that component. Runtime opt-outs apply only where the baseline still includes the component; a baseline omission solidifies exclusion.
  under: Agent integrations

+ Integration skill alias
  The short name for an optional **Integration component** in the merged `skills` config array: `"pane"` → pane skill, `"tasks"` → task planning skills. Config and **Integration runtime config** use aliases; reasoned integrate output and `--no-*` flags use catalog component ids (`pane-skill`, `task-skills`). Unknown aliases are a config error.
  avoid: component shorthand, skill name
  under: Agent integrations

~ Integration baseline
  The global `skills` array of **Integration skill alias** values declaring which optional **Integration components** pop may install (e.g. `["tasks", "pane"]`). Pop ships embedded defaults; user declares intent in `config.toml`; CLI mutations land in **Integration runtime config**. Resolved by **Config merge order**. Status wiring is never listed. The baseline is a contract: pop must install every listed component on every integrated agent once each **Agent install path** exists.
  was: The global `skills` array declaring which optional **Integration components** pop may install (e.g. `["tasks", "pane"]`). Pop ships embedded defaults; user declares intent in `~/.config/pop/config.toml`; CLI mutations land in **Integration runtime config**. Resolved by three-layer merge (pop defaults → runtime → user), last layer wins per field. Status wiring is never listed. The baseline is a contract: pop must install every listed component on every integrated agent once each **Agent install path** exists.
  avoid: default skills, integration policy
  under: Agent integrations

+ Integration runtime config
  The middle layer in pop's three-layer config merge: `$XDG_DATA_HOME/pop/config.runtime.toml`, written by integrate commands (`--no-*` shrinks `skills`; **Bare integrate** clears this file's overrides). Pop embedded defaults load first; user `~/.config/pop/config.toml` wins last. Integrate reads the merged result — no separate preference store.
  was: The middle layer in pop's three-layer config merge: a TOML file under the data directory (`$XDG_DATA_HOME/pop/config.toml`) written by integrate commands (`--no-*` shrinks `skills`; bare integrate clears runtime overrides). Pop defaults embed first; user config at `~/.config/pop/config.toml` wins last. Integrate reads the merged result — no separate preference store.
  avoid: runtime settings, persisted opt-out json, integrations.toml
  under: Agent integrations

+ Bare integrate
  `pop integrate <agent>` with no component flags: installs status wiring for the named agent(s) plus every optional component in the merged **Integration baseline**, with no prompts. Clears **Integration runtime config** overrides (restores pop defaults unless user config constrains `skills`). Re-adds globally opted-out components unless solidified in user config.
  avoid: wizard path, default install flags
  under: Agent integrations

~ Config merge order
  How pop resolves effective configuration: embedded pop defaults, then **Integration runtime config** (`config.runtime.toml`), then user `config.toml` — each layer overrides the previous for the fields it sets. The merge mechanism is global (all config keys); v1 only integrate writes the runtime file, for `[integrations]` only. Integrate, refresh, and Doctor consume the merged config.
  was: How pop resolves effective configuration: embedded pop defaults, then **Integration runtime config**, then user config — each layer overrides the previous for the fields it sets. Integrate, refresh, and Doctor all consume the merged config.
  under: Agent integrations

+ Agent install path
  Where pop lands a file-based **Integration component** for one agent (e.g. claude's skills directory, opencode's flat agent file). Each agent may need a different shape (directory symlink vs single file). A component is installable for an agent only once pop implements that agent's path; until then **Doctor** reports the gap and integrate records a reasoned skip — not a degraded partial install.
  avoid: agent support matrix, supported agents list
  under: Agent integrations

~ Integration conflict overwrite
  Destroying an unowned entry that blocks a pop **Integration component** requires an explicit `--overwrite-conflicts` on integrate; plain integrate and **Integration refresh** skip and name that command. The only integrate prompt is `Overwrite <path>? [y/N]` during that flow (or `--yes` to skip it). Pop-owned reinstalls and opt-out removals never prompt.
  avoid: conflict prompt, overwrite wizard
  under: Agent integrations

~ Integration refresh
  Reconciling installed **Integration components** to the merged **Integration baseline**: re-renders pop-owned artifacts, installs any listed component that is missing on an already-integrated agent, and skips components not in the merged baseline. Never prompts; never installs over an **Integration conflict**; never removes opted-out components unless the merged baseline still lists them and they were only declined in **Integration runtime config** (cleared by **Bare integrate**). Runs on binary-revision-gated picker launch and on `pop integrate --update-existing`.
  was: Reconciling installed **Integration components** to the state pop now expects: it re-renders by resolved name (not just content), so prefix/name changes are applied and stale old-named entries pruned; it installs any default component that is missing and not opted-out; and it leaves uninstalled agents alone. Runs on the binary-revision-gated picker-launch path and on `pop integrate --update-existing`. Never prompts; never re-adds or updates an opted-out component (see **Component opt-out**).
  under: Agent integrations

- Integration wizard
  was: The re-entrant interactive flow of `pop integrate <agent>`. It shows each **Integration component**'s current state, explains what the component brings before asking, and may be re-run at any time to add or remove components. Non-interactive runs require explicit component flags; without them they fail rather than installing a default.
