---
status: accepted
---

# Queue dashboard verify verb is an un-locked Verifier force, UI-gated on quiescence

## Context

The Queue dashboard surfaces NEEDS-VERIFY and VERIFY-FAILED sets but offers no
way to run the **Verifier** on them without launching a full drain (`i`). A
dedicated verb is wanted. Draining a NEEDS-VERIFY set already reaches the verify
phase (no eligible AFK tasks → straight to the Verifier), so a separate verb
earns its place only by being the *lighter, explicit* force — just the Verifier,
no drain lifecycle, lock, target picker, or terminal recording — routed through
the existing `pop tasks verify` command.

## Decision

Add a `v` **verify** verb to the `a` action menu, shown only on NEEDS-VERIFY and
VERIFY-FAILED rows. On selection it spawns a pane (via `spawnDrain`, inheriting
the `@pop_set` pane-per-set tagging) running
`pop tasks verify <set> --task-runtime-path <row.runtimePath>`. It fires
immediately — the pane is the feedback.

Four decisions carry the weight:

- **Pin the checkout via `--task-runtime-path`.** `pop tasks verify` resolves the
  runtime path from the repo root by default, so a bound set would be judged at
  the wrong SHA. The flag already flows through `taskResolveInput` →
  `RuntimeOverride`; it is registered on `taskVerifyCmd` and fed the row's runtime
  path, exactly mirroring how the drain pins `implement`. Empty runtime path (no
  resolvable checkout) omits the flag and defaults to project root, as the drain
  does when no worktree is ready.

- **No lock, no spawn intent.** A verify pane is not a drain: it records neither a
  Runtime execution lock nor a spawn intent nor a DrainPane, so no `●`
  live-drain indicator lights and `p` (preview) does not reach it. The pane is
  ephemeral; its verdict surfaces on the next poll's `ApplyVerifyVerdicts`
  re-derivation with no extra refresh code.

- **UI-gated on quiescence, not command-gated.** A plain verifier force
  (`runAndStoreVerdict`) does an unguarded verdict upsert — unlike the
  accept/remediate dispositions, it has no checkout-quiescence transaction. So the
  verb is *hidden* when a live drain holds the set, letting the running drain
  verify itself (ADR-0104's spirit) rather than adding a new gate to the command.

- **Run the Verifier only.** The verb does not surface the Accept/Remediate
  dispositions (ADR-0103); those stay on the CLI and the gate prompts.

## Considered options

- **Add a command-side quiescence gate to plain verify.** Rejected: the plain-force
  race with a live drain is two agent verdicts last-writer-wins onto the same
  `(repo, set, work_sha)` key — benign versus the human-authored-PASS overwrite
  ADR-0104 guards; hiding the verb in the UI is cheaper than a new gate.
- **A top-level `v` key** instead of a menu verb. Rejected: spends a scarce
  top-level key and diverges from the house row-verb pattern (see OPEN-QUESTIONS).
- **Record a DrainPane so `p` reaches the verify pane.** Rejected: verify is not a
  drain, and recording one would light the live-drain indicator on a row no drain
  holds.

Builds on [ADR-0104](0104-out-of-band-mutators-require-checkout-quiescence.md)
(quiescence for out-of-band verdict mutators) and
[ADR-0111](0111-queue-dashboard-retires-drain-column-for-in-progress-and-a-live-drain-indicator.md)
(the live-drain indicator this verb is careful not to light).
