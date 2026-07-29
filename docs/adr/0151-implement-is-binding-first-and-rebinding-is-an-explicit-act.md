---
status: accepted
---

# Implement is binding-first and rebinding is an explicit act

> **Relates:** amends [ADR-0147](0147-managed-worktrees-are-provisioned-eagerly-at-the-operator-s-request.md) (replaces the routing clause it carried forward from ADR-0072) and extends [ADR-0146](0146-set-scoped-commands-resolve-their-runtime-path-binding-first.md) to `pop tasks implement`.

A foreground `pop tasks implement` targeted the **current checkout** unconditionally: a bound set was silently re-pointed to wherever the command ran. ADR-0146 had already ruled the opposite for every other set-scoped command — a set has one work checkout, and where a command was invoked from is not part of the set's identity — leaving implement as the sole cwd-first verb.

That inconsistency was not theoretical. The Queue spawns drains as `pop tasks implement <set> --task-runtime-path <checkout>` into a per-set tmux pane, in the trunk's session; `EnsureTaggedPane` reuses an existing tagged pane and only sends keys, never correcting its cwd. A set whose pane was created while it drained on the trunk, and which was later bound to a managed worktree, therefore had its own routing rebind it **back to the trunk** — the pin was checked for conflicts but never consulted when deciding to rebind, because that decision compared the binding against cwd. Work then landed on trunk, the managed binding was downgraded to adopted, and every later tick routed to trunk. On a managed binding the rebind additionally reached the confirm-gated worktree deletion, i.e. an unattended drain prompting a human to delete the worktree it was supposed to run in.

Decision: **implement resolves binding-first, and moving a set off its binding requires `--force-rebind`.**

- Bound, invoked elsewhere: drain **at the binding** and print where. Refuse when the current repository is not the set's, as the Assist session already does. Validate the bound checkout first — the missing/unregistered-worktree refusal stops being Queue-only.
- Bound, with `--force-rebind`: re-point to the current checkout. If the set has at least one done task, confirm first (`Started`) — a rebind resumes the drain in a checkout that lacks that work, so it would most likely restart from the wrong task; non-interactive requires `--yes`. The confirm-gated teardown of the vacated managed checkout ([ADR-0116](0116-managed-worktree-teardown-is-reference-counted.md)) is asked separately, after.
- Unbound: bind the current checkout, unchanged — with nothing to lose, the session the human is sitting in is the right answer.
- `--in-worktree` on a bound set keeps refusing without `--force-rebind`; with it, it provisions and retargets, replacing today's "run `unbind-worktree` first". Both it and the plain rebind pass through one authorization seam, so the policy and the prompt ordering exist once.

## Considered Options

- **Refuse when invoked outside the binding.** Correct but hostile, and the same objection ADR-0146 already recorded: the binding exists so the human doesn't have to stand in it. Hitting `implement` from the dashboard's session is the common case, not a mistake.
- **Make `--task-runtime-path` authoritative for routing** (pin implies no rebind) and leave the cwd-first default. Rejected: it repairs the Queue's spawned drains while leaving the trap armed for humans, and keeps implement disagreeing with every other set-scoped command.
- **Fix the pane cwd only.** Rejected as the primary fix — it patches the one path that happened to be observed, and a stale pane is one of several ways to invoke implement from the wrong directory.
- **Name the flag `--force`, matching `bind-worktree --force`.** Rejected: implement has other destructive axes (dirty-runtime strategy, gates) that a bare `--force` would accrete.

## Consequences

- The habit of "cd to a checkout, run implement, it follows you" is gone; the flag is the replacement, and the progress prompt makes the expensive case loud.
- A bound set can no longer be moved by accident, so the Queue's pin is defence in depth rather than the only guard. Two adjacent defects are fixed alongside it: the pin-versus-binding comparison is canonicalized (unresolved symlinks produced a spurious `ErrRuntimeOverrideConflict` that hard-failed the drain), and a reused pane's cwd is corrected at spawn.
- Nothing consults a `worktree` manifest key any more (ADR-0115 stopped seeding it, ADR-0147 made `--managed` provision and bind eagerly). The registered-intent field remains only as the Queue-side healing path for sets registered under the old lazy behaviour, and foreground routing does not read it.
