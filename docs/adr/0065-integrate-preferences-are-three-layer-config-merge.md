---
status: accepted (amends [0064](0064-integrate-installs-all-components-by-default.md))
---

# Integrate preferences are a three-layer config merge

Optional **Integration components** resolve from merged config, not a wizard or a separate JSON opt-out store: embedded pop defaults, then `$XDG_DATA_HOME/pop/config.runtime.toml` (CLI-written), then user `~/.config/pop/config.toml` (wins). The global `[integrations] skills` array lists **Integration skill alias** values (`"pane"`, `"tasks"`); `[integrations] skills_prefix` sets the installed skill name prefix (default `pop-`, empty for bare names — see [ADR-0063](0063-skill-install-names-are-a-configurable-prefix.md)); both fields merge with the same layer precedence. Status wiring is always installed and never listed. `--no-*` removes an alias from the runtime layer; **Bare integrate** clears runtime overrides and reinstalls the merged baseline. Positive opt-in flags (`--pane-skill`, `--task-skills`) are removed with a hard error. The **Integration wizard** is retired; the only integrate prompt destroys an unowned **Integration conflict** entry during `--overwrite-conflicts`. **Integration refresh** reads the same merged baseline and may add missing listed components. Closing **Agent install path** gaps (codex skills, opencode multi-file task skills) is follow-up work — the baseline is a contract, not a permanent support matrix.

## Why

ADR-0064 default-on opt-out assumed a per-agent JSON preference store and deprecated positive flags softly. In practice the wizard still ran, `--overwrite-conflicts` was ignored on that path, and "not supported" masked missing install plumbing. Folding CLI mutations into `config.runtime.toml` reuses config merge instead of a bespoke preference layer; global `skills` matches how users think about the toolkit; user config solidifies intent by editing the same field the CLI touches at runtime.

## Considered Options

- **Per-agent runtime opt-outs in JSON or TOML.** Rejected: the user wanted global skill selection; per-agent config tables add merge complexity without matching the mental model.
- **User config as the only store; CLI edits `~/.config/pop/config.toml` directly.** Rejected: conflates scratch opt-outs with version-controlled intent; chezmoi-managed configs would fight integrate.
- **Keep the wizard, default prompts to Yes.** Rejected: still prompts on pop-owned reinstalls; ADR-0064 already retired this path.

## Consequences

- `config.Load` gains a global three-layer merge; v1 only integrate writes `config.runtime.toml`, initially for `[integrations]` only.
- `--pane-skill` / `--task-skills` become hard errors immediately (stricter than ADR-0064's deprecated no-op).
- Chunk 1 ships claude/pi/cursor fully; codex/opencode task/pane gaps remain until **Agent install path** follow-up closes them.
- Doctor and integrate report missing install paths as debt, not permanent "not supported."
