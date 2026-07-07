---
fragment: VERIDEMP
generation: 0009
branch: grill-verify-after-hitl
---

+ Verification idempotency after PASS
  Once **Agent verification** returns PASS within the current **verification episode**, no subsequent automatic **Verifier** invocation may run — including on drain re-entry at DONE after terminal HITL completion, on HEAD drift from unrelated checkout work, or when another **Task set** advanced the same checkout. The cached PASS is authoritative; the drain's terminal verify path becomes a cache lookup only. Automatic re-verification is warranted only when no PASS exists in the episode: first arrival at the terminal zone, a prior non-PASS verdict (NEEDS-HUMAN or exhausted remediation cap), or **Verification invalidation** after the set leaves the terminal zone. Explicit human force (`pop tasks verify`, HITL gate Re-verify) remains available.
  avoid: SHA-gated re-verify, post-HITL verify loop, verify on HEAD move

~ Post-HITL verification pass
  The structural second touch of `drainVerifyPhase` when a drain continues after terminal HITL completion moves the set to DONE. It is not a separate verification policy — it reuses the same cache-first path as the pre-HITL pass. When a PASS exists in the episode it must be a no-op (no agent spawned); only a missing or non-PASS verdict may invoke the **Verifier**.
  avoid: second verify, post-approval verification

~ Verify verdict disposition
  How each three-way **Verify verdict** drives what happens next. **PASS** immunizes: no further automatic **Verifier** runs in the episode (**Verification idempotency after PASS**). **FIXABLE** spawns a **Remediation task**, **Verification invalidation** clears the cache, and re-verify is mandatory after remediation drains — a deliberate loop, not a failure retry. **NEEDS-HUMAN** (or exhausted remediation cap) parks at VERIFY-FAILED; the prior non-PASS verdict warrants re-verify on the next terminal drain attempt. Explicit human force (`pop tasks verify`, HITL gate Re-verify) sits outside this automatic disposition.
  was: The cached result of **Agent verification** for a **Task set** at a specific work SHA, held in the **Drain** store: PASS (proceed to approval or Done), FIXABLE (findings an agent can resolve), or NEEDS-HUMAN (only a human can resolve). A verdict is stale once the work SHA moves, which returns the set to needing verification.
