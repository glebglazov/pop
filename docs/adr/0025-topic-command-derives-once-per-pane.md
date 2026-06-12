# Topic command derives once per pane

A configured `topic_command` runs **at most once per pane**: the first prompt with no existing Topic derives one, and every prompt after that is a no-op that keeps it. We chose this over re-deriving each prompt because the LLM call is the user's cost and latency, a Topic is meant to be the pane's stable *subject* (not a running echo of the latest prompt), and a frozen-but-good Topic plus a manual-note override is more useful than a fresh-but-expensive one. This narrows the per-prompt scope ADR-0024 implied.

The guard is purely `prevTopic == ""`: no new config key (ADR-0001 — monitor stays config-light) and no new per-pane state. Truncation (the no-`topic_command` fallback) is unchanged — it still runs every prompt, since it makes no model call and costs nothing.

## Considered Options

- **Time throttle (`topic_min_interval`).** Re-derive at most every N seconds so the Topic tracks conversation drift with bounded cost. Rejected: adds a config key and a per-pane timestamp for a freshness benefit we judged marginal — panes are short-lived and a manual note already overrides.
- **Every Nth prompt.** Rejected: needs a per-pane counter and still updates on a schedule unrelated to whether the subject actually changed.
- **Once, but only on a *successful* command derive** (let a truncation-fallback Topic still allow LLM retry). Rejected: needs a per-pane "was LLM-derived" flag to fix a narrow failure-poisoning case; not worth the state.

## Consequences

- The very first prompt of a session sources the Topic, even when it's a poor subject ("read /tmp/handoff.md"); the Topic then sticks for the pane's monitored lifetime. A user-authored note overrides it (ADR-0024).
- If the first derive fails/times out, the truncation fallback sets the Topic and the command is **never retried** for that pane — a single early failure freezes a truncated Topic. Accepted as the cost of zero state.
- Retiring and recreating the pane (or a new agent reusing a retired pane id) clears the Topic, so `prevTopic == ""` again and the command re-derives — restarting the pane is the way to refresh a stale Topic.
- `prev_topic` in the `topic_command` JSON payload is now always empty in practice (the command only ever runs when there is no previous Topic). The field stays in the published contract (additive-only, ADR-0024) but carries no signal.
