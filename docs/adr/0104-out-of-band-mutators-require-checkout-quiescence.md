# Out-of-band mutators require checkout quiescence

The standalone `pop tasks verify --accept/--remediate` path writes verdicts (last-writer-wins upsert) and appends to the manifest with no lock and no hold, while the in-drain verify-fail gate parks the runtime lock and registers a Checkout gate hold before prompting. A live drain mid-verify on the same checkout could silently overwrite a human-authored PASS or race the manifest append.

Decision: out-of-band mutations require **checkout quiescence** — no live drain (PID+ProcStart liveness) and no Checkout gate hold on the checkout — and are refused with a clear error otherwise. The quiescence check and the write happen in a single store transaction, the same pattern as `StartDrain` mutual exclusion, so no window exists between check and write. The drain owns the checkout while it runs; humans mutate when it is quiescent.

## Considered Options

- **Gate hold for the mutation duration** — blocks recovery-turn acquisition but not a live drain; closes only half the race.
- **Drain-style claim (BeginDrain equivalent)** — strongest exclusion, but heavy for a millisecond verdict upsert and pollutes drain history with non-drain rows.
- **Keep last-writer-wins** — human verdicts silently lost; the scope-growth check is the only backstop for the manifest race.

Builds on [ADR-0100](0100-quota-recovery-is-checkout-scoped-in-implement.md) (gate holds) and [ADR-0103](0103-human-verdict-disposition-is-accept-or-remediate.md) (the dispositions this governs).
