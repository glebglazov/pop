---
status: accepted
---

# Queue dashboard retires the DRAIN column; live drains surface as an IN-PROGRESS refinement plus a live-drain indicator

## Context

The `pop queue dashboard` carried a dedicated DRAIN column holding four values — `picked up` (a live, PID-alive Runtime execution lock), `parked`, `config error: <msg>`, and blank. Separately, the STATUS column already refined a READY set to "IN PROGRESS" when it had ≥1 done task. This split two closely-related facts across two columns and let "IN PROGRESS" and "picked up" drift apart in the reader's head, while spending a whole column on state that is blank for most rows.

## Decision

Retire the DRAIN column. Its facts redistribute:

- **`picked up` (a live drain)** surfaces two ways. (1) It joins the existing "started" trigger so `READY` refines to **IN PROGRESS** when the set is started (≥1 done) **or** a live drain holds it. (2) A **live-drain indicator** — a leading `●` in the house working colour — marks any row whose Runtime execution lock is PID-alive, across every status, signalling that `p` (preview drain) can reach a pane.
- **`parked`** and **`config error: <msg>`** become STATUS suffixes (` · parked`, ` · config error: <msg>`), joining the existing ` · auto-drain` / ` · orphaned` suffixes.

The IN-PROGRESS refinement applies to READY only. A live drain that coincides with a non-READY status (AWAITING-APPROVAL with a paused agent, NEEDS-VERIFY, BLOCKED) keeps its real label — "needs-you" outranks "a process is alive" — and shows the live-drain indicator so the pane is still discoverable. Sort keeps a "running" tier that floats live-drain rows to the top, and the header "N running" tally counts live-drain rows (fixing an over-count that previously tallied any non-blank DRAIN, including parked/config-error).

## Considered options

- **A RUNNING status of its own** (live drain → RUNNING label). Rejected: it either masks needs-you states when it dominates, or needs a precedence rule as intricate as the refinement one, for a label that duplicates what the indicator already says.
- **A single overloaded IN PROGRESS, live rows identified by sort tier only.** Rejected: you can't tell a live row from a crashed-mid-set row by reading it.

## Consequences

- The "In Progress" glossary term is redefined (live-drain trigger added); "Picked-up Task set" is unchanged as a fact but no longer surfaces via a column.
- Sort-tier, header-count, and auto-drain-suffix-silencing (ADR-0108) logic re-key on a structured live-drain bool instead of the `Drain == "picked up"` string that no longer exists.
