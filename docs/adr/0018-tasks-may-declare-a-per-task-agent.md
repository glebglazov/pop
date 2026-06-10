# Tasks may declare a per-task agent in the Manifest

A task entry in `index.json` may carry an optional `agent` key holding an **Agent preset**-shaped value (e.g. `claude --model opus4.8`), letting a planning workflow pick the agent and model for an individual task. The agent for an attempt resolves by precedence: explicit `--agent-cmd` > explicitly-passed `--agent` > the task's `agent` key > the default `claude`. A bare defaulted `--agent` does not override a task key. An unknown preset in the key is a contract fault that makes the Task set **Malformed**; the opaque `--agent-cmd` form is not permitted in a Manifest.

## Why

Planning is where the "which model fits this task" judgment is made — a heavy refactor wants Opus, a mechanical edit wants something cheap. Encoding that per task in the definition lets a drain run each task on its intended agent without per-invocation babysitting. The key reuses the `--agent` string form (see ADR-0017) so one resolution path serves both flag and Manifest.

Explicit CLI wins so a human keeps a blanket escape hatch (force the whole set onto a cheap model, or onto an opaque command) without editing the Manifest. But the override must be *explicit*: a bare defaulted `--agent claude` must not stomp a planner's per-task choice, or the key would be dead whenever claude is the default — hence the resolution keys on `Flags().Changed("agent")`, not on the resolved value.

Recognized presets only, validated at manifest load: a Task set surfaces as Malformed at discovery rather than failing mid-drain, and the Manifest never holds an opaque, plain-output command. Allowing the `--agent-cmd` form to be persisted would silently strip **Agent quota detection** and the **Captured attempt stream** from every attempt on that task, contradicting what a durable, replayable definition is for.

## Considered Options

- **Task key wins over explicit `--agent`.** Rejected: removes the human's blanket override; the planner's stored choice should not be unoverridable from the CLI.
- **Per-Task-set (top-level) agent instead of per-task.** Rejected: per-task is strictly more expressive and a set-wide need is just every task sharing one key; a top-level default can be added later if that proves tedious.
- **Allow the opaque `--agent-cmd` form in the Manifest.** Rejected: durable opaque commands forfeit structured telemetry on every attempt and cannot be validated or replayed.
- **Bare model name as the value.** Rejected: the `--agent` string form already expresses both agent and model and shares the flag's resolution path.

## Consequences

Persisting a per-task agent in the definition is distinct from the rejected "persist extra args in user config" — the Manifest is the task definition, not a global user preference. Validation must shell-split the value and check the first token against the known presets at manifest load.
