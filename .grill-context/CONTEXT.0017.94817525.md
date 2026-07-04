---
fragment: 94817525
generation: 0017
branch: master
---

+ opencode-go provider quota
  The opencode.ai workspace rolling-window allowance (e.g. a five-hour limit) exhausted when running `opencode-go/*` models. Surfaced as a stable headless error regardless of which Agent preset fronts the provider.
  avoid: pi quota, opencode preset quota

~ Agent quota detection
  Identifying from Agent output handling that a task attempt stopped because the agent allowance is exhausted. Detection relies on a stable headless signal; a signal from a shared provider (e.g. opencode-go) may be matched once and wired into every Agent preset that can emit it. A detected quota pause stops implement cleanly without retrying, leaves the task Open, preserves partial runtime changes, and does not append a progress record. It is not a Failed, Skipped, or Interrupted task. Proactively reporting remaining allowance is the separate **Agent quota reporting** concern.
  was: Identifying from Agent output handling that a task attempt stopped because the agent allowance is exhausted. Detection is preset-specific and relies on a stable headless signal. A detected quota pause stops implement cleanly without retrying, leaves the task Open, preserves partial runtime changes, and does not append a progress record. It is not a Failed, Skipped, or Interrupted task. Proactively reporting remaining allowance is the separate **Agent quota reporting** concern.

+ opencode-go quota signal
  The stable headless substring `5-hour usage limit reached` in provider output, matched case-insensitively. The full diagnostic line (including `429`, relative reset hint, and upsell URL) is kept as the pause reason; only the substring gates detection.
  avoid: 429 prefix requirement, usage limit reached alone, case-sensitive match

+ opencode-go quota reset
  When the diagnostic includes `Resets in <N>min`, pop derives `PauseResetAt` as now plus N plus the **Quota assurance offset** (two minutes). Wired for both `pi` and `opencode` through `agentQuotaResetAt`. Absent pattern leaves `ResetAt` zero.
  avoid: exact N only, absolute clock parsing for opencode-go

+ Quota assurance offset
  A fixed two-minute buffer added on top of a provider-stated relative reset window when deriving `PauseResetAt`, so agent fallback and pinned-agent cooldown fire slightly after the provider's own countdown rather than on its exact edge.
  avoid: retry-after, cooldown grace, reset buffer

+ pi quota scan scope
  For opencode-go provider quota on the `pi` preset, detection scans the full raw capture line-by-line — including plain non-JSON stdout lines — not only structured `errorMessage` fields.
  avoid: errorMessage-only detection

+ opencode quota scan scope
  Same as **pi quota scan scope**: the `opencode` preset scans the full raw capture line-by-line for the shared opencode-go provider matcher, not only JSON `error` diagnostics.
  avoid: error-event-only detection
