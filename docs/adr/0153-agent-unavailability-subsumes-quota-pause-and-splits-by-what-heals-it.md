---
status: accepted
supersedes:
  - ADR-0043 (in part — advancement is no longer quota-only)
---

# Agent unavailability subsumes quota pause and splits by what heals it

> **Relates:** generalizes the **Agent quota pause** verdict of [ADR-0043](0043-agent-fallback-owned-by-implement.md) and [ADR-0034](0034-reset-aware-agent-cooldown.md); folds the Verifier's missing-binary skip of [ADR-0086](0086-agent-verification-is-a-pre-approval-drain-phase.md) into the same class; gates the recovery wait of [ADR-0100](0100-quota-recovery-is-checkout-scoped-in-implement.md) on the time-healing kind only; leaves the retry schedule of [ADR-0099](0099-task-attempt-retry-backoff-is-blocking-in-process.md) untouched but unreachable for this class.

On 2026-07-29 the `cursor` CLI was logged out. Task set `2026-07-29-managed-worktree-ahead-of-set`, task `01`, ran three attempts at 11:33, 11:34 and 11:39 — each lasting half a second and emitting one line, `Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable.` Pop spent 1.5 seconds working and six minutes sleeping in **Task attempt retry delay**, marked the task Failed, and stopped the drain. The set sat idle for three hours until a later drain ran it on `pi` and it passed first try. Every ingredient of the right answer was already present: a configured fallback list with a working agent in it, and a deterministic signal on the very first attempt saying this preset could not work.

The gap is that **Agent fallback** advanced on exactly one verdict. ADR-0043 was explicit that this was a choice — *"advancement is quota-only … failure-based or per-attempt agent diversity is explicitly out of scope and could layer on later."* This is that layer, and it is deliberately narrow: it does **not** make ordinary task failure advance the agent list. It generalizes the verdict instead.

## Decision

`AgentQuotaPause` becomes one case of an **Agent unavailability** verdict — "this **Agent preset** cannot do the work at all", as distinct from an attempt that ran and failed. Every kind shares one behaviour: abandon the remaining **Task retry cap** for that preset immediately and hand the turn to the next preset in the **Agent fallback** list. The kinds differ only in their **Agent unavailability recovery**, and that axis decides everything else:

- **Time-healing** — **Agent quota pause**. Carries a reset instant. Writes the machine-global cooldown and, once the list is exhausted, parks into **Agent quota recovery wait**. Unchanged from ADR-0034/0043/0100.
- **Human-healing** — **Agent authentication failure** and a missing binary. Carries no instant, writes no cooldown, and must never enter the recovery wait: polling cannot resolve a logged-out CLI. When every configured preset is human-healing unavailable, implement exits `ExitSetup` with the provider's own diagnostic and the naming of the preset. The task **stays Open** — nothing about it failed, and a Failed record would both lie and poison the ADR-0040 prior-attempt digest fed to the next run. The drain terminal stays `finished`; this is a clean stop, not an abnormal teardown, so it never feeds **Queue backoff**.

Detection runs on two channels, because authentication can lapse at any moment including mid-drain:

- **Passive** — parse the capture we already consume, exactly as **Agent quota detection** does, per ADR-0034's rule that a signal is read from the stream in hand and never queried ahead. Confirmed for `cursor` only; other presets await a captured logged-out sample.
- **Active** — an **Agent availability probe**, an adapter capability alongside `AssistanceCapability()`, run lazily the first time a preset is reached in an **Implement run** and memoised one-way for that run. Shipping for `cursor` (`cursor-agent status --format json` → `isAuthenticated`), `claude` (`claude auth status` → `loggedIn`) and `codex` (`codex login status`). `pi` and `opencode` expose no status readout and ship none.

The two channels divide cleanly: the probe catches "already logged out when we got here", the passive signal catches "logged out since". Only an explicit positive counts as authenticated — a non-zero exit, unparseable output or a probe timeout reads as *unknown*, and unknown proceeds to invoke the agent. A probe is never permitted to block real work on its own parse failure, and a probe writes no **Captured run**, since it is not an agent invocation.

The Verifier walks the same class: its existing PATH skip becomes the missing-binary kind, and an all-unavailable list now hard-errors rather than returning empty output for `ParseVerdict` to render as NEEDS-HUMAN — a verdict which claims a verifier judged the work when none ever ran. `pop doctor` gains the auth check the **Agent catalog** glossary has always deferred to it, giving the human the fix-it surface for the hard error.

## Considered options

- **Keep a sibling verdict beside `AgentQuotaPause` rather than a parent.** Rejected. The park-versus-error split is not an ad-hoc rule; it falls out of whether a recovery carries an instant. Two sibling types would state that rule twice and let them drift.
- **Adopt the model in the glossary but implement the new kinds as a sibling type, converging the code later.** Rejected: it leaves `CONTEXT.md` describing a unification the code does not have, which is the failure mode the glossary exists to prevent.
- **Probe every configured agent up front, once per Implement run.** Rejected. The agent list is an ordered fallback and usually only its head runs, so this pays for probes on agents never reached.
- **Probe before every attempt.** Rejected as constant re-exec for a condition that changes rarely; the passive channel already covers a mid-run lapse.
- **Remember unauthenticated presets durably, like the quota cooldown store.** Rejected. A human fixes auth out of band at any instant, so a durable record would keep skipping a preset that is already logged back in. The in-run memo costs nothing and dies with the process.
- **Treat an unauthenticated agent as a quota pause and let the recovery coordinator wait it out.** Rejected outright — the wait would never end, holding a **Recovery waiter** and a checkout indefinitely.
- **Mark the task Failed on the hard error, as today.** Rejected: the disposition belongs to the environment, not the task.
- **A `crashed` drain terminal so Queue backoff throttles re-spawns.** Rejected. It mislabels a clean stop, and the cost it would save is one probe exec per queue tick — no agent tokens.
- **Make probing configurable (`probe_timeout`, an enable/disable toggle).** Rejected for now: under unknown-⇒-proceed a probe has no failure mode worth a knob.
- **Also advance the agent list when a preset exhausts its retries** (the surviving asymmetry: the Verifier does this, implement does not). Explicitly out of scope. That asks a different question — whether a repeated failure is the agent's fault or the task's — and answering it wrong lets one doomed task burn every configured agent's quota.

## Consequences

- The quota path is load-bearing and this refactor touches it: the cooldown store, the **Agent quota recovery coordinator**, and `drainTerminal` all now read a kind rather than a boolean. The compensation is that the recovery wait becomes impossible to enter with no reset instant, which was previously only a convention.
- Implement gains a fall-through it never had for a missing binary. Today that case is worse than a bad login: `Runner.Start` failing returns `ExitOperational` and kills the whole drain without trying another preset.
- A **Captured run** for this class records an `agent_unusable` outcome carrying the provider's raw line, rather than today's `failed` / `agent exited with status 1` — which is why `pop tasks stream` gave no hint of the real cause on the run that motivated this ADR.
- Logged-out output shapes are unconfirmed for every probe; only logged-in samples exist. The unknown-⇒-proceed rule is what makes that safe — a wrong guess degrades to exactly today's behaviour, one wasted half-second attempt, with the passive channel behind it.
