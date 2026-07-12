---
status: accepted
---

# Auto-drain marker is silenced on Picked-up rows; dashboard count is waiting-only

The **Auto-drain** consent bit is a persisted, **sticky, multi-drain** authorization: it stays set across an entire drain → verify → remediation → drain progression and clears only at drain finalization to a terminal disposition ([ADR-0098](0098-auto-drain-clears-at-drain-finalization.md)). This ADR settles how the **Queue dashboard** *displays* that bit, separately from the bit itself.

- **Per-row marker.** The `· auto-drain` suffix on the **Task set status** cell is **silenced while the set is Picked-up** (a live drain is running on it — the DRAIN column already signals the activity, so the suffix is redundant there). The persisted bit is untouched; only its rendering is suppressed.
- **Header count.** The dashboard summary's auto-drain tally counts **waiting** sets only — consented but *not* Picked-up. A set being actively drained is being handled, so it does not inflate "still needs picking up."

Both facts are derived at **render time** from the live row fields (`AutoDrain`, `Drain`) against a single source of truth — `m.snap.Rows`. The row's STATUS cell is composed in the List Cell closure rather than baked into a stored string at build time, so the per-row marker and the header count both recompute from the same rows on one View pass.

## Why

The user's instinct was to **clear the bit when a drain starts** ("we've started processing it, drop the flag"). Rejected: that silently turns Auto-drain into **one-shot**. The daemon fires drain #1, the bit clears, drain #1 ends non-terminal (BLOCKED / NEEDS-VERIFY / VERIFY-FAILED), and now the bit is gone — the daemon will not fire drain #2 to continue the loop, stranding the set mid-progression until a human re-marks it. Auto-drain's whole value is unattended multi-drain progression, so the bit must stay sticky (ADR-0098 already keeps it through exactly those non-terminal states). The real irritation was the marker *nagging* on a set already in motion — a display concern, fixed by silencing the marker, not by discarding consent.

The render-time composition additionally fixes a re-render bug: the toggle handler mutated only the `AutoDrain` bool, so the header count (which read the bool live) updated immediately while the per-row suffix (baked into a stored string, never re-synced) lagged up to a 2-second poll. Composing at render time from `m.snap.Rows` makes any action's effect land on the row and the count together.

## Considered Options

- **Clear the consent bit when the first drain starts.** Rejected — makes Auto-drain one-shot; see Why.
- **Dim the marker instead of dropping it while Picked-up.** Rejected: the DRAIN column already shows the drain; a dimmed suffix is residual clutter with no added signal.
- **Keep the header count as all-consented (including Picked-up).** Rejected: with the per-row marker silenced on Picked-up rows, an all-consented header would disagree with the visible rows; waiting-only keeps header and body telling the same story.
- **Recompose the stored status string in every mutating handler** (instead of composing at render time). Rejected: fragile — every future mutation would have to remember to recompose; a single render-time source of truth cannot drift.

## Consequences

- The header auto-drain count now moves as drains are picked up and finish — a background, daemon-driven signal surfaced on the existing 2-second poll. Accepted; the poll cadence is unchanged.
- Silencing is keyed on **Picked-up**, not on any status; a consented set that is not currently draining still shows `· auto-drain` regardless of its status.
- The consent bit and its dashboard marker are now explicitly two things: mutating the bit (`a` toggle) and rendering it are decoupled, and ADR-0098's finalization-clear remains the only path that clears the bit.
