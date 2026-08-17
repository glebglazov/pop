---
name: spend-audit
description: Audit drain spend on a task set — rank runs by turns and peak input, trace the dearest attempt at argument granularity, classify what wasted tokens, and route the fix. Manual-only; run when a drain you care about looks expensive.
disable-model-invocation: true
---

<!--
No upstream base: this skill is pop-original, auditing pop's own captured run
store for a drain that looked expensive. Human-opened: naming the waste bucket
and choosing the remedy are judgment calls for the human running the audit —
the skill "walks the procedure; it does not let the model start an audit on
its own" (see below) — so it must stay something a person opens against a
specific drain, never something triggered against every task set.
-->

# Spend audit

A spend audit answers one question: **why did this drain cost so much, and what do we change?**

Pop supplies facts and threshold-marked suspects. **Naming the waste bucket and choosing the remedy are yours** — this skill walks the procedure; it does not let the model start an audit on its own.

## Pop surfaces this skill drives

| Surface | What it gives you |
| --- | --- |
| `pop tasks spend --sort tokens` | Ranks recent task sets (bare) or breaks one set down per task. **Always pass `--sort` explicitly** — the bare default is `recency`, which answers "what have I been doing", not "what was expensive" (ADR-0218). Read the **`turns`** and **`peak-in`** columns first — they surface long-grinding runs and peak context pressure, not just token totals. Turn-blind and peak-blind runs show **`—`**, never zero; do not treat blind as cheap. The spend cell reads `tokens (~$notional)`: the parenthesised figure is a **modelled** list price, not money spent, and a **`(—)`** means the model has no rate, not that the run was free. The **`agent`** and **model** columns appear when a set mixes them — check both before comparing numbers across rows. Use `--json` when you need machine-readable `turns`, `peak_input_tokens`, `notional_cost_usd`, and blind-run counts. |
| `pop tasks stream <TASK_SET>/<task> --tool-detail` | Replays one task's attempts and deepens the timing breakdown to **argument-level tool facts**: repeated identical invocations, unbounded file reads, largest payloads, error loops, image reads. Without `--tool-detail`, replay stays terse (that output also prints during live drains). Pop may mark a run **`suspect:`** when peak-in exceeds 200k or turns exceed twice the set's median — relative thresholds only. **Pop does not classify waste buckets** ("search thrash", "missing repo docs", …); you do, from the facts. Render-blind agents refuse tool detail with their stated reason instead of partial facts. |

Run both commands from the repository whose drain you are auditing. They read pop's captured run store; they capture nothing and mutate nothing.

## When to run

Open this skill deliberately against a drain you care about — a set that finished expensive, retried heavily, or shows suspect markers during replay. Do not run it speculatively across every set.

## Procedure

Work the four steps in order. Stop when you have a routed fix the human agrees with.

### 1. Rank — find runs worth tracing

```sh
pop tasks spend --sort tokens
```

State the sort — do not inherit the default, which is recency. Use `--sort cost` instead when you are arguing about money rather than throughput, remembering that rate-blind rows sort last as a named block and are not cheap, merely unpriceable. Add `--all` when the question spans projects rather than this repository.

Scan **`turns`** and **`peak-in`** alongside the token and notional-cost columns. High turns with moderate spend often mean thrash; high peak-in with few turns often means one enormous context load.

When you already know the task set:

```sh
pop tasks spend <TASK_SET>
```

Breaks the set down per task (verification runs are separate rows). Pick the row that best matches your concern — usually the highest peak-in or turns among implement attempts, not verify-only rows.

If the set mixes agents, use the **`agent`** column before comparing numbers across rows.

Respect blind markers: a row showing **`—`** for turns, peak-in or notional cost cannot be ranked on that axis. Note blind runs (`blind` column / JSON `*_blind_runs`) but do not infer zero.

### 2. Trace — open the dearest attempt at argument granularity

Take the task (and agent, if mixed) from step 1 and replay with tool detail:

```sh
pop tasks stream <TASK_SET>/<task.md> --tool-detail
```

Read attempts top to bottom. For each attempt:

- Note **`suspect:`** lines — pop's relative flags only, not a verdict.
- Read the **tool-detail sections** under the timing breakdown:
  - **repeated** — same tool with identical arguments invoked more than once
  - **unbounded reads** — whole-file reads where a range would do
  - **largest payloads** — where result bytes dominated
  - **errors** — repeated failing invocations (loops)
  - **image reads** — vision payloads on text tasks

If tool detail refuses (render-blind agent), record the refusal reason and fall back to the name-level timing breakdown only. Do not invent argument-level facts.

Use `--last` when only the final attempt matters; omit it when retries are part of the story.

### 3. Classify — name the waste bucket (human judgment)

From the rank and trace evidence, **you** name what kind of waste this is. Pop deliberately stops at facts and suspect markers; bucket labels are not emitted by either command.

Common buckets (examples, not an exhaustive taxonomy):

| Bucket | Signals in the trace |
| --- | --- |
| Search / grep thrash | High **turns**, long **repeated** grep or search invocations, many small reads |
| Whole-file read waste | **unbounded reads**, large **largest payloads** on Read without offset/limit |
| Missing repo context | Agent rediscovers layout, conventions, or APIs already documented; repeated exploratory reads across many paths |
| Error loop | **errors** section with the same invocation retried many times |
| Oversized task | Single attempt with extreme **peak-in** and many tool types — the task may be too large for one drain |
| Vision on text | **image reads** where the task did not require visual judgment |

A run can match more than one bucket. Pick the dominant one for routing, or split fixes when two buckets need different remedies.

If evidence is thin (blind axes, refused tool detail), say so and narrow the audit to what you can see rather than guessing.

### 4. Route — choose where the fix lives

| Bucket (examples) | Route the fix to |
| --- | --- |
| Missing repo context | **Repo instructions** — `AGENTS.md` / `CLAUDE.md`, `CONTEXT.md`, `docs/agents/navigation.md`, or an ADR the agent should read first |
| Wrong exploration strategy | **Attempt prompt** — the task file's "What to build", orientation links, or acceptance criteria; tighten what to read before acting |
| Task too large / unfocused | **Task sizing** — split the task set, narrow scope, add a prerequisite task, or reduce parallel surface area |
| Tool misuse (ranges, duplicates) | **Attempt prompt** first (explicit read constraints, "do not re-run X"); repo docs second if the pattern is recurring |
| One task iterated far past its peers, repeatedly | **Configuration** — recommend bounding the repository's implementation turns: `pop config repo set turn_cap <N>`, with `<N>` your suggested bound. Recommend it; do not run it yourself |
| Agent / adapter blind spot | Note the blind axis or refusal; fix may belong in pop's adapter capabilities, not the repo — escalate separately from repo changes |

Present the human a short summary:

1. **Which run** (set, task, attempt, agent if mixed)
2. **Facts** (turns, peak-in, suspect markers, top tool-detail findings)
3. **Your bucket label** — explicitly yours, not pop's
4. **Proposed route** — which file or task change, with a concrete draft when the fix is repo-side; when the route is configuration, the exact `pop config repo set` command instead of a draft

Wait for confirmation before editing repo instructions or task files, or before the human runs a proposed configuration command — the skill only names it.

## Done

The audit is complete when the human accepts a routed fix (or explicitly defers). Re-run `pop tasks spend` on the same set after the next drain if you need to verify the numbers moved, passing the same `--sort` you used the first time — two rankings taken under different sorts are not a comparison.
