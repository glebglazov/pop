# Remediation depth counts auto runs since the last human remediation

Previously the human Remediate disposition bypassed the depth-cap check but its spawned task still counted toward the cap (depth = count of all `NN-remediation` manifest entries), so a human remediating N times silently exhausted the auto-remediation budget.

Decision: Remediation tasks carry an **origin** (auto = Verifier-spawned, human = disposition-spawned). Remediation depth is the count of **consecutive auto-origin tasks since the last human-origin one**, derived from the manifest — no stored counter, the same derivation idiom as crash backoff from drain history. A human remediation therefore resets the auto budget: human intervention is a fresh grant of trust, the same philosophy as a park-clear. The loop stays human-bounded because each reset costs a human action.

Additionally, the human Accept note now survives verdict invalidation on the remediation path (both origins), feeding forward into the next Verifier run — previously only the scope-growth path preserved it; the asymmetry was an oversight, not a decision (see [ADR-0103](0103-human-verdict-disposition-is-accept-or-remediate.md)).

## Considered Options

- **Auto-only lifetime cap, no reset** — after N auto remediations ever, only humans could remediate; stricter than the loop-protection intent.
- **Total-churn cap for all origins** — treats the cap as manifest-growth protection and refuses human remediation at cap; rejected, the cap exists to bound the unattended loop.
- **Status quo** — human actions silently consuming auto budget is surprising coupling.
