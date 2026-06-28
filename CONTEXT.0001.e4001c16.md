---
fragment: e4001c16
generation: 0001
branch: docs/topic-derivation-pipeline
---

~ Topic
  A normalized lowercase kebab slug (≤5 words) naming the subject of an Agentic pane, single-sourced as a per-pane tmux property any tmux surface can display. pop fills it in stages — an instant seed from truncating the prompt, then optionally a higher-quality agent-derived value, and optionally re-derived as the conversation drifts. A Note still outranks it in display.
  was: A normalized lowercase kebab slug (≤5 words, e.g. `debugging-auth-middleware`) naming the subject of an Agentic pane. It is single-sourced as a per-pane tmux property, so any tmux surface — not just pop's dashboard — can display it, and it is reusable in custom tmux labels. pop derives it once per pane via Topic recipes and normalizes the result; a Note still outranks it in display.
  under: Pane status

~ Topic recipe
  One step in pop's ordered Topic-derivation list. A step is either a truncate step (cheap, local, no model — produces a seed) or an agent step (a curated agent-CLI invocation — produces a final Topic). Each step declares a guard for when it may run against the current Topic's provenance, and may carry its own appended arguments and timeout. pop owns the prompt and output normalization but links no model SDK and holds no API keys — auth lives in the CLIs.
  was: A pop-curated invocation of an agent CLI (local or remote) that pop runs to derive a Topic. pop tries configured recipes in order and uses the first non-empty result, so a failed or rate-limited agent falls through to the next. pop owns the recipes, prompt, and output normalization but links no model SDK and holds no API keys — auth lives in the CLIs.
  under: Pane status

+ Topic seed
  A provisional Topic written instantly by the truncate step, before any model runs, so a pane has an immediate subject. An agent step may overwrite a seed; a final Topic or a Note may not be overwritten by it.
  avoid: provisional topic, draft topic
  under: Pane status

+ Topic provenance
  Whether a pane's current Topic is a provisional seed or a final value. It is the gate every derivation step is checked against — the basis for seed-then-refine and for opt-in regeneration.
  avoid: topic kind, topic state
  under: Pane status

+ Topic regeneration
  Re-deriving a pane's Topic on later prompts so it tracks the conversation's drift, instead of freezing after the first derivation. Opt-in per step.
  avoid: refresh, re-derive
  under: Pane status
