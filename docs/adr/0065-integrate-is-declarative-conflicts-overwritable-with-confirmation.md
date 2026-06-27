---
status: accepted (amends ADR-0064, revisits ADR-0063)
---

# Integrate is declarative; conflicts are overwritable only with confirmation

An explicit `pop integrate <agent>...` run reconciles each named agent to the declared component set: declining a default component (`--no-pane-skill` / `--no-task-skills`) now both records the [Integration opt-out](../../CONTEXT.md) and **removes** the component if it is currently installed, where before `--no-*` only suppressed a fresh install. Removal touches **pop-owned artifacts only** and needs no prompt. A new `--overwrite-conflicts` flag (valid on explicit install only, never with `--update-existing`) lets pop resolve an [Integration conflict](../../CONTEXT.md) — the one thing it otherwise refuses to touch — by hard-deleting the unowned entry and linking its own. Because that destroys a file the *user* made, it **asks before each overwrite (default no)**; `--yes`/`-y` assumes yes for unattended runs, and with no TTY and no `--yes` it skips-and-reports rather than blocking or destroying. `pop integrate` also becomes **variadic** (one or more named agents, uniform flags), and both the explicit and refresh paths print every reconcile outcome with its reason by default (`--verbose` adds no-ops; `POP_LOG` stays the deep trace).

## Why

The reworked integrate was silent about *what it did and why* — refresh decisions only reached `POP_LOG` — and `--no-X` was non-declarative: it recorded a decline but left the already-installed skill loaded by the agent, contradicting the user's mental model ("`--no-pane-skill` should mean it's not there"). Making explicit install declarative closes that gap symmetrically with [ADR-0064](0064-integrate-installs-all-components-by-default.md)'s bare-run "re-assert the full set". Separately, [ADR-0063](0063-skill-install-names-are-a-configurable-prefix.md) dropped a `--force` whose job (force re-render past byte-equality) reconcile already did; the real unmet need is overwriting an *unowned conflict*, a genuinely different operation pop had no way to perform. We add it under a self-documenting name (`--overwrite-conflicts`, not a reloaded `--force`) and gate it behind per-item confirmation because it is the only integrate path that deletes user data.

## Considered Options

- **Keep `--no-X` consent-only; removal solely via `pop integrate remove`.** Rejected: leaves the "I said no but it's still there" surprise on the common path.
- **Make refresh declarative too (remove opted-out / overwrite conflicts in the sweep).** Rejected: the picker-launch auto-refresh would then silently delete agent skills with no command typed that turn. Deletion and overwrite must trace to a deliberate, explicit run; refresh stays add/update/skip-with-reason only.
- **Reuse the name `--force`.** Rejected: it was just documented as removed (ADR-0063) for a different meaning, and "force what?" is vague. `--overwrite-conflicts` names the exact, narrow job and pairs with the glossary term.
- **Overwrite without a prompt (flag is the consent).** Rejected: the conflict can be a directory the user actively maintains; a destructive default deserves a per-item confirm. The flag opts into the *flow*; the prompt (or `--yes`) is the per-item gate.
- **Back up the overwritten entry instead of hard-deleting.** Rejected: stray `.bak` files/dirs beside skills are their own mess (and may get scanned by the agent). The safety guarantee is **visibility** — a loud per-item report of exactly what was destroyed — not recoverability.
- **A `--all` / multi-agent selection policy.** Rejected in favour of plain variadic agents: naming the agents keeps every target explicit, with no fuzzy "which agents count" contract, and preserves `--overwrite-conflicts`'s "only agents you named" scope.

## Consequences

- Two deletion paths with different risk: opt-out removal (pop-owned, reversible by re-running, no prompt) and conflict overwrite (user-owned, destructive, prompted). The invariant is **prompt iff about to destroy something pop does not own**.
- `--overwrite-conflicts` is rejected with `--update-existing`; conflicts found during refresh are skipped and reported with the exact `pop integrate <agent> --overwrite-conflicts` command that would resolve them.
- New flags: `--overwrite-conflicts`, `--yes`/`-y`, `--verbose`. Default output widens (per-outcome reasoned lines on both paths); steady-state runs stay quiet because pure no-ops are suppressed unless `--verbose`.
- Amends ADR-0064: explicit `pop integrate` is non-prompting *except* for the conflict-overwrite confirmation, the single deliberate exception for destroying user data.
