---
status: accepted
---

# `pop tasks spend` is a cross-project lens and a notional cost annotates tokens

Two changes to the **Spend lens**, taken together because the second only became defensible once the first forced the question.

`spend` widens past the current repository: `--all` rolls up every **Task set** registered on this machine, one row each, with a Project column. Its default sort becomes **recency**, meaning the set's latest **Captured run** start time. And every token figure gains a parenthesised **Notional cost** — what those tokens would have cost at published API list price — computed from a vendored **Rate table** keyed on the run's **Actual model**.

This amends [ADR-0160](0160-spend-is-a-cross-set-lens-and-usage-extraction-is-per-adapter.md) on both axes. Its per-adapter usage-extraction rule, its over-count guard, and its insistence that blind runs are counted and named all stand unchanged and are extended rather than revisited.

## Why the scope widens, and why recency displaces tokens

ADR-0160 argued a rollup deserved its own command because "a rollup takes no `TASK_SET` argument, so it is a different noun." Widening across repositories is the same move one level up, and the same reasoning does *not* apply: the row unit is still the Task set and the substrate is still Captured runs, so `--all` is a filter, not a new noun. It stays a flag on `spend`, and the default stays repo-scoped — a lens run inside a checkout that silently reports on other repositories is the kind of surprise ADR-0160 was written to avoid.

The enumeration comes from `store.AllSets`, the registrations already grouped by `def_path`, not from the config-declared project list the supervisor scans. Registration is already the membership act for every other Work surface; it needs no forks and no filesystem walk; and it keeps history for repositories that have since left config, which is exactly what a "where has my spend been going" question wants. The cost is that a set on disk that was never registered is invisible, which is consistent with every other read surface.

Recency displacing total tokens as the default sort is a change in what the lens is *for*. Sorted by tokens it answers "what was expensive"; sorted by recency it answers "what have I been doing." The second is the question a cross-project rollup gets asked, and the first remains one flag away. Because the **Spend audit** procedure depended on the old default, that skill now passes its ordering explicitly rather than inheriting a default that moved underneath it — a procedure that states its own sort is more robust than one that does not, independent of this change.

Recency had to be redefined to survive the widening. It was `sort.Reverse` on the identifier string, a heuristic that holds inside one repository because identifiers are often date-prefixed, and that is meaningless across repositories with different naming habits. It is now the set's latest **Captured run** start time: comparable everywhere, needing no convention, and free — those runs are already being read to compute the row. The identifier heuristic is retired, not kept as a fallback.

## Why a pricing table now, when ADR-0160 declined one

ADR-0160 deferred a pricing table because "prices drift and a stale table lies more confidently than an absent column." That was right, and one more objection has surfaced since that it did not name: **pop drives subscription CLIs.** The **Effort ladder** exists to walk down them and **Agent quota detection** reads a subscription 401. Under a seat or a plan, the marginal dollar cost of those tokens is zero. A table lookup produces arithmetic that is correct and a label that is false.

Three things changed that make the table worth having anyway.

**Per-model attribution exists.** **Actual model** is already derived per run as a declared per-adapter capability. ADR-0160's implicit table would have had to guess which model a run used; this one does not, which makes each figure auditable down to the model that produced it.

**The published data covers the models pop actually runs.** OpenRouter's public models endpoint needs no authentication and carries `prompt`, `completion`, `input_cache_read` and `input_cache_write` as separate rates — a one-to-one match for pop's four-component `TokenUsage`, which matters because cache-read runs an order of magnitude cheaper and a blended rate would be wrong by a lot. Checked against a real configuration, every model in use resolved except Cursor's `composer` family.

**The name can carry the caveat.** Calling it **Notional cost** rather than price or cost states in one word that it is a modelled figure, and lets it sit in the same table as pi's *measured* `cost.total` without the two being confused. Measured cost outranks the estimate wherever both exist.

The display settles what the metric is allowed to claim. Tokens stay the unit of record and the notional figure is parenthesised behind them — `4.2M (~$3.10)`. That keeps ADR-0160's "tokens are the primary unit" intact, keeps a run whose model cannot be priced fully comparable on the axis that always exists, and means a wrong rate degrades an annotation rather than corrupting the headline.

## What happens when the table cannot answer

Three named gaps, each following the precedent that a figure is never quietly understated by inputs it could not measure.

**Actual-model-blind adapters** fall back to the model named in the **Requested agent**, the resolved preset string pop records verbatim at invocation. codex and opencode declare actual-model blindness, and their models are in the table — without the fallback every such run would be unpriceable with its rate sitting right there. The row marks which of the two supplied the key, because a provider fallback can make them differ.

**Models with no cache-write rate** — every non-Anthropic model checked publishes `input_cache_read` alone — are priced at their prompt rate. Those providers do not bill cache writes separately, so this reflects billing rather than assuming it. Discarding three correct rates over one absent field would understate, which is the failure being guarded.

**Models absent from the table entirely** produce a **Rate-blind run**: absent, never zero, counted in the footer, and sorted into a trailing block under `--sort cost` so an unpriceable model can never rank as the cheapest thing in a list. Cursor's `composer` family is the standing case and will not be fixed by a better table — it is seat-included and has no published list price. A human may declare an override rate for it, rendered distinctly from a published one: pop inventing a price is fabrication, but a human stating "bill composer at grok rates, that is my mental model of it" is an assumption its owner can see and revise.

## Considered Options

- **A separate global command rather than a flag.** Rejected: same row unit, same substrate, same question — widening the enumeration is a filter.
- **Global becomes the default and repo-scope becomes the flag.** Rejected: a lens run inside a checkout should answer about that checkout unless asked otherwise.
- **Enumerating from the config project list.** Rejected: a repository dropped from config would erase its own history from a lens whose whole job is history.
- **Keeping the reverse-identifier recency heuristic.** Rejected: not comparable across repositories, and a real instant was already being read.
- **Ranking `--sort tokens` on a subset — input+output, or output alone — to dodge cache-read dominance.** Rejected: every subset is a smuggled relative-price model, which is the objection this ADR takes seriously elsewhere, and silently redefining "total tokens" would break comparison against figures the lens has already printed. The mitigation is display — the four components stay visible, and the model column explains the rest.
- **No table at all, tokens only.** Rejected on the evidence above, but it was the position held for most of this design and it remains correct for anyone without per-model attribution.
- **Calling the figure price or cost.** Rejected: under subscription billing it is confidently wrong about the one thing the label claims.
- **Depending on a Go pricing library.** Rejected: the surveyed packages are thin wrappers over "parse a JSON map and multiply." The maintained artifact is the JSON; the arithmetic is not worth a dependency.
- **Fuzzy or substring matching of model identifiers onto table keys.** Rejected: it is how a cheap model gets priced as a frontier one. Normalisation is a declared per-adapter rule, following ADR-0165's fixture discipline, and an unmatched model is rate-blind rather than guessed.
- **Applying long-context rate overrides.** Rejected: a per-request tier cannot be applied to an aggregate. The resulting understatement is bounded and one-directional, which is the safe direction for a figure already labelled notional.
- **Vendoring only the models pop's ladders name.** Rejected: it couples the snapshot to today's configuration, so adopting a model silently produces rate-blind runs until someone re-vendors.
- **Refreshing the table from a Routine rather than from the lens.** Considered seriously and not taken. A read-only lens that writes a cache on a timer is the shape ADR-0006/0056 keep rejecting, and it makes `spend` non-deterministic. The decision is that a once-a-day refresh with a 3s timeout and a silent fallback is a small enough departure to keep the refresh where the data is used; the fetch sits behind an injected seam so tests never reach the network, and the cache lands in `<data>/pop/`, never the repository.

## Consequences

The first `spend` of a day may stall up to 3s on the refresh. A failed fetch is silent and the vendored snapshot is used, with the footer naming the snapshot's date — so a machine that is offline for a month reports a month-old table and says so.

`--all` shows the most recent 20 sets by default, adjustable with `--limit`, as one flat list. Per-project quotas were rejected because they re-rank a recency sort into something that is no longer recency. Archived sets stay excluded, matching per-repo behaviour. A `def_path` whose storage is gone is skipped from the rows and counted in the footer, so a total never silently shrinks.

`--json` gains `project` and `last_run_at` on every row — present even repo-scoped, where they are merely constant — plus `notional_cost_usd`, `rate_source` and `model_key`. Fields never depend on a flag: a payload whose shape follows an argument makes every consumer conditional for no gain.

A new agent adapter now carries a third easily-missed obligation beside ADR-0160's usage-extraction rule and ADR-0165's fixtures: a model-identifier normalisation rule. Without one its runs are rate-blind — visible and named, which is the mitigation, not a defect.

The **Spend audit** skill states its sort explicitly from now on.

`spend` remains an instrument, not a remedy, and the notional figure does not change that. If anything it sharpens ADR-0160's closing point: a cross-project view of where tokens go is most useful for deciding what to stop doing, and a dollar annotation makes that argument to a reader who does not think in tokens.
