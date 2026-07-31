---
status: accepted
---

# Archiving never reaches a running process

**Archive** refuses a Task set with live occupancy — a running **Drain**, a **Recovery waiter**, or an open **Checkout gate hold** — and names the PID, the controlling tty, and the **Queue pane** where known. `--force` archives anyway and releases the hold. Symmetrically, occupancy reporting is never filtered by the `archived` flag: a live gate or drain owned by an archived set still appears in status footers, dashboard, and **Checkout quiescence** refusals. Those refusals gain the same location detail, since the resolution is nearly always "answer the prompt that is still open."

## Why

Archiving is a registration-metadata write. It hides a row from the **Status table**, from automatic selection and draining, and from completion — all statements about *future* admission. Nothing in it reaches a process already running, and on 2026-07-31 that gap cost a checkout: a set was archived while its `pop tasks implement` sat at a HITL gate in a tmux pane, blocked on `read`. The row vanished from every surface the human would consult; the process kept its hold for fourteen hours. When an unrelated set was then refused, the refusal named a set that could no longer be found — correct information from the occupancy layer, invisible to every layer that filters on `archived`.

The two halves are one decision. Refusing at archive time prevents a live pane from being filed away; unfiltered occupancy reporting means that if one is filed away anyway (`--force`, or a gate opened after the archive), it still cannot become an invisible blocker. Either alone leaves the hole open from the other end.

Naming *where* the occupant is parked is what converts the refusal from a dead end into a fix. pop already holds the PID; the tty follows from it, and the pane is recorded at spawn for queue-launched drains.

## Considered options

- **Archive terminates the parked process.** Rejected: killing a human's interactive session to satisfy a metadata write is worse than refusing it, and the gate may hold a decision the human is midway through making.
- **Archive silently releases the hold, leaving the process running.** Rejected: the process would then be at a menu whose disposition can be raced, which is the interlock the hold exists to provide.
- **Report occupancy but let archive proceed.** Rejected as insufficient — it fixes legibility after the fact while still letting a human file away the pane they need to answer. The refusal is the cause fix.
- **Treat an archived set's hold as abandoned and stealable.** Rejected: archived is not dead. Stealing would let another drain mutate the tree the parked human is reading.

## Consequences

- Contradicts the previously unqualified "archiving is always instant, reversible metadata": it can now fail. The failure is actionable and `--force` is always available, and the alternative is an operation that silently strands a process.
- The occupancy check adds a store read to every archive, including the batch path, which must report per-set refusals rather than failing the whole selection.
- Archived sets become visible in exactly one class of surface — occupancy — which is a deliberate exception to the flag's meaning and is stated as such in the **Archived Task set** glossary entry.
- Refusal messages carry a tty and pane, which are machine-local and can be stale if the pane was closed without the process exiting. Liveness of the PID still governs; the location is advisory.
- Companion to [ADR-0161](0161-gate-occupancy-is-set-scoped-except-the-dirty-tree-claim.md): 0161 stops one set's gate blocking another's disposition, this one stops a gate becoming invisible in the first place.
