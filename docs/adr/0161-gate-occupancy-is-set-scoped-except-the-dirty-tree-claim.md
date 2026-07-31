---
status: accepted
---

# Gate occupancy is set-scoped, except the dirty-tree claim

A **Checkout gate hold** now exists in two scopes rather than one. The default — every HITL, verify-fail, and clean Failed gate — is **set-scoped**: keyed `(runtime_path, set_id)`, it occupies only its own set for **Checkout quiescence** ([ADR-0104](0104-out-of-band-mutators-require-checkout-quiescence.md)). The exception is the **checkout-scoped claim**: a Failed-gate park over an uncommitted tree, which keeps blocking admission checkout-wide as [ADR-0135](0135-admission-to-a-checkout-is-gated-on-the-checkout-claim-union.md) established, now enforced by a partial unique index on `(runtime_path) WHERE claim = 1` instead of by the table's blanket `runtime_path` primary key. Quiescence is asked per set: an out-of-band mutation of set A is refused only by a live foreign hold *on set A*. `ErrGateHoldHeld` — after this change reachable only for a double-drain of one set — is returned rather than discarded at both gate call sites.

## Why

Observed 2026-07-31 on the inline-trunk checkout `~/private/Dev/personal/pop`. Set `2026-07-30-fold-rebase-and-session-prefix` parked at its HITL gate at 18:23:39Z, finishing its Drain row 0.64ms before registering the hold — ADR-0067 and ADR-0135 working exactly as specified. The human never answered the menu; the pane sat blocked on `read` for fourteen hours, and the set was archived meanwhile, removing every surface that would have surfaced it.

Set `2026-07-31-tasks-spend-lens` then drained the same checkout — correctly admitted, since a HITL hold claims nothing — failed verification, and reached its own verify-fail gate. There `RegisterCheckoutGateHold` hit the `runtime_path` uniqueness constraint against a live foreign owner and returned `ErrGateHoldHeld`, which both call sites discarded with `_ =`. The gate ran holding nothing (silently losing the ADR-0100 protection it was supposed to have), and then Accept refused: quiescence read the one row for the path, found a foreign set, and told the human to "resolve it at the interactive gate" — which is precisely where they were standing.

The mis-keying is the root. What a non-claiming hold protects is *this set's disposition from being written twice*; it was keyed on the checkout only because the claiming case, which genuinely is checkout-wide, shares the table. One uniqueness constraint was serving two different scopes, so a prompt open on one set could veto a verdict on another.

## Considered options

- **Make non-claiming holds block drain admission too.** Rejected — it is the option ADR-0135 already named and rejected ("a human reading a menu must not stall the queue"). It would have prevented the two sets sharing a checkout, at the cost of the queue liveness that decision bought deliberately.
- **Drop the non-claiming hold entirely; gates hold nothing.** Tempting, and truest to ADR-0067's "every wait for human input releases it." Rejected: the claiming arm keeps the table and its liveness sweep alive regardless, so this removes a lock without removing a mechanism, and it leaves a human's considered verdict racing an out-of-band `--accept` with no interlock at all.
- **Optimistic concurrency instead of a hold** — carry the work SHA into the verdict write and fail the compare-and-set on a race. Rejected for the same table-stays-anyway reason, plus it converts prevention into a late failure at the exact moment a human has finished typing a rationale into a prompt.
- **Age out long-held gate holds.** Rejected: a parked human is a legitimate state of unbounded duration. The problem was never the fourteen hours; it was that those hours blocked an unrelated set and left no trace of where the prompt was.

## Consequences

- Amends ADR-0104 and ADR-0135: **Checkout quiescence** is no longer a property of a checkout but a question asked about a checkout *and a set*. The claim union is unchanged.
- Schema migration: `checkout_gate_holds` moves from `runtime_path` primary key to `UNIQUE(runtime_path, set_id)` plus a partial unique index on `(runtime_path) WHERE claim = 1`. The at-most-one-claiming-hold invariant moves from a comment into the schema.
- Two sets parked at gates on one checkout is now representable, and correct. Neither blocks the other's disposition; a dirty Failed gate still blocks both from *executing*.
- `ErrGateHoldHeld` becomes a real error at the gate call sites. Its only remaining trigger is two live processes draining one set, which should be loud.
- Liveness remains the true release — a dead owner's hold is ignored and replaceable — so the explicit release call stays tidiness rather than a correctness requirement. Unchanged, but now relied on knowingly.
- Does not address discoverability: a hold can still be held by a process the human has lost track of. That is [ADR-0162](0162-archiving-never-reaches-a-running-process.md).
