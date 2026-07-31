---
status: accepted
---

# Stream-shape adapter capabilities are declared on the preset spec and backed by captured fixtures

Every per-adapter rule that reads a **Captured run** — usage, cost, tool timings, actual model, stream rendering, and the new **Turn** count — moves from a package-level `map[string]func(...)` onto `presetAgentSpec` as a declared capability whose zero value is invalid, validated in `newPresetAgentAdapter`. A capability is either supported or **blind with a stated reason**; "undeclared" is not a state pop can hold. A capability claiming support must ship a trimmed real captured stream under `tasks/testdata/turns/` (and its siblings) plus a golden count, and a table test over `agentCatalogOrder` fails if it doesn't.

This closes a hole [ADR-0160](0160-spend-is-a-cross-set-lens-and-usage-extraction-is-per-adapter.md) named in its own Consequences: "Adding a new agent adapter now carries an obligation that is easy to miss: declaring its usage-extraction rule." ADR-0160 offered visibility as the mitigation — a token-blind run is a named state rather than a silent zero. That works for the *reader* of a spend table and not at all for the *author* of a new adapter, who gets no signal whatsoever. Because the preset table is a package-level `var`, construction-time validation fires on every build of every test binary: a missing decision is now a compile-time-ish failure, not a quiet blind column six weeks later.

## Why a map was the wrong shape, and why a nil func is no better

Absence from a map is indistinguishable from a decision not to support. Both read as "no entry." Moving to a struct field does not fix this on its own — a nil `Extract` func is the same ambiguity wearing a field name. Hence the explicit kind:

```go
type AgentTurnCapability struct {
    Kind    TurnRuleKind // turnRuleUnset (zero) is never valid
    Extract func([]streamEventRecord) TurnCount // required iff Supported
    Reason  string                              // required iff Blind
}
```

`Reason` is doing real work. Reaching "blind" costs a sentence naming what the stream lacks, which is the difference between opencode's honest *"parts carry no message boundary"* and a shrug. Blindness stays cheap to declare and impossible to declare thoughtlessly.

**`renderOneStreamEvent` is the worst of the six and the reason this is urgent.** Its `default` arm does not declare blindness — it emits `Type: "raw"` carrying the raw JSON string, so five of six adapters render as JSON soup that *looks like output*. That is strictly worse than a map hole: a missing entry produces nothing, while this produces something wrong at a glance. Under a declared capability the trace lens refuses with a named reason instead.

## Why fixtures, and why only for this family

Declaration proves a decision exists. It cannot prove the decision is *true*. Nothing stops someone reading the sample lines in `agent_kimi_live_test.go` — a *renderer* test, which only shows what that renderer chose to handle — and declaring `Supported` on a rule never run against a real captured stream. The validation passes. The number is fiction, and a fictional turn count is worse than a blind marker because it ranks.

Requiring a captured fixture makes the evidence the test input: there is no path to a supported rule without pasting real stream lines into the repo. It also pins rules against upstream drift — when cursor changes `model_call_id`'s shape, CI notices before an audit does.

This standard applies to stream-shape capabilities only. Invocation-shape capabilities ([ADR-0166](0166-invocation-shape-capabilities-move-onto-the-preset-spec.md)) are claims about a CLI's arguments, where a fixture would prove nothing; there, the declaration is the whole gate. The asymmetry is the subject of these two ADRs rather than a footnote in one.

## Evidence behind the three turn rules

Sampled from a store of 156 cursor, 70 pi and 52 claude captured runs:

- **claude** — `type=="assistant"` events, deduped by `message.id` (consecutive events repeat identical usage).
- **cursor** — distinct `model_call_id`, present on `assistant` *and* `tool_call` events. One run: 67 model calls against 270 `tool_call` events.
- **pi** — `type=="turn_end"`, always role `assistant`. `message_end` is unusable: it mixes 11 assistant, 15 toolResult and 1 user.

codex, kimi and opencode have no captured run in any store on this machine and are **turn-blind** until one exists. codex is additionally doubtful on inspection: its stream is *items* (`agent_message`, `reasoning`, `todo_list`), so counting `agent_message` counts only turns where codex spoke prose, undercounting exactly the silent grinding turns an audit cares about. opencode emits `part`-level fragments with no visible message boundary at all.

**Peak input is the sum, not the field.** The per-call figure is `input + cache-read + cache-write`; the uncached `input` field alone is near-meaningless in a cache-heavy stream (pi reports `input: 6` against `cacheRead: 9115`; claude `185` against `51917`). A column reading `input` would rank runs by noise. cursor reports usage only as a whole-run total on `result` and so has no peak at all — an absence in the data, not a gap in pop.

## Considered Options

- **Add turns as a capability and leave the other five maps alone.** Rejected: the entire value is that adding an adapter forces a decision, and this forces one decision out of six while requiring the ADR to explain why turns are special when they aren't.
- **Migrate Tier 1 now, the rest later.** Rejected: the validation panic can only be switched on once, when every capability is declared. A half-migration validates six fields and stays silent on the others — the same forget-silently hole in a new shape.
- **Write kimi's turn rule from its live-renderer fixture.** Rejected despite being the strongest of the three unsampled candidates (`tasks/agent_kimi_live_test.go:68`: kimi flushes an assistant message as one line). A renderer's silence about an event is not evidence the stream lacks it.
- **Count tool calls as a proxy for turns.** Rejected on evidence: 67 model calls against 270 tool events on one cursor run. Parallel tool batches make it a different metric, not an approximation.
- **Keep `renderOneStreamEvent`'s raw-JSON default as a graceful fallback.** Rejected: it is not graceful. It is a wrong answer that resembles a right one.

## Consequences

Adding an adapter now fails to compile its own test binary until every stream-shape stance is declared, and fails its table test until each supported stance ships a fixture. That is the intended cost.

Three adapters (codex, kimi, opencode) gain named blind markers where they previously had silent absence, so spend and trace output will visibly widen its "we don't know" surface before it narrows. Dropping one captured run per adapter into `tasks/testdata/` converts a blind column into a demanded rule — the suite then *requires* the rule be written, which turns "run the smoke script sometime" into a mechanical gate.

`cmd/pane.go`'s `resolveTopicRecipe` switches on `claude`, `ollama`, `cmd` and `sh` — a topic-recipe namespace that merely overlaps on the string `"claude"`. It is deliberately **not** an adapter capability and must not be folded in later.
