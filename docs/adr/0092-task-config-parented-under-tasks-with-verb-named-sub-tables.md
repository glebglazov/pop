---
status: accepted
supersedes:
  - ADR-0043 (in part — only the `[workload] default_agents` config-key name; fallback ownership stands)
---

# Task config is parented under `[tasks]` with verb-named phase sub-tables

Task-execution config moves out of the `[workload]` parent — a name the struct comments already kept only "for backward compatibility" — into **`[tasks]`**, matching the `pop tasks` command family. Within it, the two phases get **verb-named sub-tables** mirroring their subcommands: `[tasks.implement]` (whose `agents` is the ordered worker fallback list, formerly `[workload] default_agents`) and `[tasks.verify]` (the Verifier's `enabled` / `agents` / `effort` / `max_remediation_depth`, formerly `[workload.verify]`). `[workload.git]` becomes `[tasks.git]`, and the per-preset settings map `[workload.agents.<name>]` becomes **`[tasks.presets.<name>]`** so "agents" no longer means two different things one nesting level apart (an ordered fallback *list* under a phase, versus a *map* of per-preset settings).

The whole old `[workload]` tree is retained as an **honored deprecated alias**: the new key wins, the old fills gaps and emits a load-time warning, exactly like every other renamed key in `config/config.go`. Removal is gated on beta-tester sign-off in CLEANUP.md, not a version. The `includes` whitelist (ADR-0037) accepts both `workload` (deprecated) and `tasks`.

## Why

`pop tasks` is the command; `implement` and `verify` are **sibling subcommands** of it, so the config section names should track the verbs a user actually types (`pop tasks implement` → `[tasks.implement]`). This also honours the already-agreed CLEANUP.md decision to rename `[workload]` — that decision assumed a *flat* rename to `[tasks]`; this ADR keeps the target name but restructures the internals in the same move rather than shipping a flat rename now and a second breaking reshuffle later.

## Considered Options

- **Parent `[implement]` with role sub-tables `[implement.worker]` / `[implement.verifier]`.** Rejected: nesting the verifier *under* implement misstates the CLI — verify is a sibling command (and a standalone `pop tasks verify`), not a part of implement. `implement` cannot outrank `tasks` as the parent when `pop tasks` is the command family.
- **Role nouns `[tasks.worker]` / `[tasks.verifier]`.** Tempting because "worker" is informal code vocab (ADR-0040) and Verifier is a domain role — but config sections named unlike the CLI verbs drift from what the user types. Verb names win once `tasks` is the parent.
- **Keep the per-preset map as `[tasks.agents.<name>]`.** Rejected: leaves `agents` meaning both an ordered fallback list (`[tasks.implement].agents`) and a settings map, the exact ambiguity this reorganisation exists to remove.
- **Hard, silent rename (drop `[workload]` with no read-compat).** Rejected: contradicts the deprecation-with-warning convention every other renamed key in this file follows, and a silently-dropped `[workload.git]` re-arms the GPG-presence hang `TaskGitConfig` exists to prevent, with zero signal.

## Consequences

- Verifier `agents` entries (and `[tasks.implement].agents` entries) are full **Agent presets** — preset name plus optional args — so pinning `claude --model opus` bypasses the **Effort ladder** entirely (model *and* its bundled reasoning), the same two knobs implement's `--agent` exposes.
- The `includes` merge for `[tasks]` follows the same first-wins whole-block rule as `[workload.verify]`/`[workload.git]` already do (ADR-0037), so no precedence change for a single `.agents` field.
- ADR-0086/0087 (`[workload.verify]`) and ADR-0037 (whitelist/field-merge enumeration) were written against the old parent and stay as historical records; map their `[workload.*]` names to `[tasks.*]` through this ADR rather than rewriting them. The living references — the `TaskConfig`/`VerifyConfig` doc comments, the `[queue] agents ignored` warning string, README, and CONTEXT.md's **Agent fallback** term — are corrected as part of the implementation. ADR-0043's ownership decision is untouched — only its config-key name is superseded here.
