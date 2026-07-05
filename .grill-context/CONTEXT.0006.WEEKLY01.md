---
fragment: weekly01
generation: 0006
branch: master
---

~ opencode-go provider quota
  The opencode.ai workspace rolling-window allowance exhausted when running `opencode-go/*` models — whether the window is five-hour or weekly. Surfaced as a stable headless error regardless of which **Agent preset** fronts the provider.
  was: The opencode.ai workspace rolling-window allowance (e.g. a five-hour limit) exhausted when running `opencode-go/*` models. Surfaced as a stable headless error regardless of which **Agent preset** fronts the provider.
  avoid: pi quota, opencode preset quota, separate weekly quota concept

~ opencode-go quota signal
  One of the stable headless substrings that gate **Agent quota detection** for **opencode-go provider quota**, matched case-insensitively: `5-hour usage limit reached` or `weekly usage limit reached`. The full diagnostic line (including `429`, relative reset hint, and upsell URL) is kept as the pause reason; only a recognized substring gates detection.
  was: The stable headless substring `5-hour usage limit reached` in provider output, matched case-insensitively. The full diagnostic line (including `429`, relative reset hint, and upsell URL) is kept as the pause reason; only the substring gates detection.
  avoid: 429 prefix requirement, usage limit reached alone, case-sensitive match, separate weekly signal term

~ opencode-go quota reset
  When the diagnostic includes `Resets in <N>min`, pop derives `PauseResetAt` as now plus N plus the **Quota assurance offset** (two minutes). When it includes a compound hint such as `Resets in <H>hr <M>min`, the same relative sum applies over hours and minutes. When the reset phrase is absent or unparseable, pop falls back to a signal-specific backoff plus the assurance offset: one hour for the **5-hour usage limit reached** signal, one day for **weekly usage limit reached**. Wired for both `pi` and `opencode` through `agentQuotaResetAt`.
  was: When the diagnostic includes `Resets in <N>min`, pop derives `PauseResetAt` as now plus N plus the **Quota assurance offset** (two minutes). Wired for both `pi` and `opencode` through `agentQuotaResetAt`. Absent pattern leaves `ResetAt` zero.
  avoid: exact N only, absolute clock parsing for opencode-go, configured agent_quota_retry_after as opencode-go reset fallback

+ Implement quota fallback message
  When **Implement** runs a multi-preset **Agent fallback** list and one preset hits **Agent quota pause**, pop prints a dim line naming the exhausted preset and that it is trying the next — mirroring Verifier's `quota-paused; trying next` wording — before invoking the next preset. The provider diagnostic remains the pause reason; no separate weekly-specific banner.
  avoid: silent agent fall-through, verifier-only quota messaging
  under: Agent quota detection
