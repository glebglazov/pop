---
fragment: B547153B
generation: 0022
branch: master
---

+ Turn cap
  The maximum number of **Turn**s one implementation attempt may spend before
  the agent stops itself. It bounds the innermost of pop's four nested loops —
  turns inside a single agent invocation, not attempts (`max_tries`), not a
  **Drain**, not an **Implement run** — and it is a bound on runaway iteration,
  a proxy for cost rather than a cost bound, because **Peak input** shows turns
  are not fungible. Implement only: the **Verifier** runs uncapped. A
  hand-written `--max-turns` in an augmented **Agent preset** spec wins and pop
  then emits nothing.
  avoid: max turns, step limit, iteration cap, cost cap, ralph limit, attempt cap
  under: Agents

+ Turn-cap exhaustion
  The **Attempt outcome** of an implementation attempt whose agent stopped
  because it reached its **Turn cap** — recognised from the adapter's declared
  exhaustion signal, never inferred from a turn count. It consumes a try and
  enters the retry carry-forward digest, because the next attempt needs to know
  the work was cut short; contrast **Effort model skip**, which consumes no
  attempt and stays out of the digest. Work the agent already committed is left
  in place.
  avoid: max-turns error, truncated attempt, turn timeout
  under: Agents

+ Turn-cap enforcement capability
  The **Adapter capability** declaring whether an adapter can be *told* to cap
  turns on the command line, and with which flag. Invocation-shape, so no
  fixture backs it (ADR-0166). Only claude declares it Supported
  (`--max-turns`, print mode only); opencode and kimi are Blind because their
  cap is reachable only from their own configuration file, and cursor, codex and
  pi have no cap at all.
  avoid: max-turns support, turn support
  under: Agents

+ Turn-cap exhaustion capability
  The **Adapter capability** declaring whether pop can *recognise* that a run
  ended at its **Turn cap**. Stream-shape, so it is fixture-backed (ADR-0165).
  Separate from **Turn-cap enforcement capability** because the two answers
  differ per adapter: an adapter may accept a cap whose exhaustion leaves no
  machine-readable trace.
  avoid: max-turns detection
  under: Agents

+ Repo override runtime layer
  The pop-written layer of `config.runtime.toml` holding repo-scoped settings
  keyed by **repository identity**, so every worktree of a repository reads one
  value. It sits below hand-authored `[repo."<path>"]` blocks, which always win
  (ADR-0150), and it is what `pop config repo set` writes. It deliberately
  diverges from the runtime layer's existing keys (`[workbench.preferred]`, the
  repo trunk), which key by exact checkout because they describe a checkout
  rather than a repository.
  avoid: project config, per-project override, .pop/config.toml (for this)
  under: Configuration

+ Repo config write command
  `pop config repo set` — the only way pop writes a repo-scoped setting,
  targeting the **Repo override runtime layer** for the repository the current
  checkout belongs to. Its settable keys are derived from the config schema by
  reflection, as `repoScopeLegalKeys` already derives the readable ones, so the
  command cannot drift from what the config accepts. `spend-audit` recommends
  invocations of it and never runs them.
  avoid: pop config project set, pop config set --repo
  under: Configuration
