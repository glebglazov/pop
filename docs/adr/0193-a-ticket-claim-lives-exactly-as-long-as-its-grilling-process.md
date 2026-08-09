---
status: accepted
relates: "extends [ADR-0182](0182-frontier-fan-out-panes.md) — the pane owns the claim; this decides when that ownership ends. Reuses the pane-liveness predicate of [ADR-0158](0158-dashboard-verbs-split-by-whether-they-hand-off-and-say-so-in-the-key-case.md)"
---

# A ticket claim lives exactly as long as its grilling process

## Context

ADR-0182 made the **Grilling pane** the owner of its own **Ticket claim**, so a
claim ages against the pane actually doing the work. It left the ending of a
claim to two rules: **Ticket resolution** releases it, and otherwise a four-hour
TTL expires it and the next `pop map next` steals it.

The TTL is the wrong instrument, and a live incident showed it. On map
`2026-08-09-cv-builder` the human claimed ticket `02`, then closed the grilling
session in its first minutes:

```
map|2026-08-09-cv-builder|02|pane:%33|2026-08-09T11:57:48Z
```

Pane `%33` is gone — live ids had already moved past it to `%34`. Nothing about
that fact reaches pop. `Frontier` skips anything not `TicketOpen`, `applyClaims`
marks `02` claimed, so `pop map next`, `pop map fan-out` and the dashboard's
`i`/`I` all pass it over; `pop map claim` refuses because the caller's own owner
string (`pid:…`) is not `pane:%33`. The ticket was unreachable by every door for
three hours and twenty minutes, waiting out a timer that describes nothing real.

The same Map showed the second shape of the failure: pane `%29`, tagged ticket
`01`, still exists and runs a bare `zsh` — the agent died, the pane did not. A
rule that only asks "does the pane exist?" calls that claim live forever.

Everything downstream of the claim is already idempotent. `SpawnFrontier` tops
up, and `openMapPane` reuses a pane only when `liveGrillingPane` reports a
running process, respawning into an idle one otherwise. The claim row was the
only part of the spawn path that could not be re-run.

Elsewhere in pop this is already settled doctrine: every **Checkout claim**
source is liveness-backed (owner PID + start token) so a dead owner's claim is
swept rather than wedging the checkout. Ticket claims were the exception.

## Decision

1. **A claim is live iff a grilling process is running in its owner.** For a
   pane owner that is `liveGrillingPane`'s existing predicate — the pane is
   readable *and* `pane_current_command` is not a bare shell. For a `pid:` owner
   it is a `kill -0` probe. Anything else is dead and its ticket is back on the
   **Frontier**.

2. **The pane-reuse predicate and the claim-liveness predicate are one
   predicate**, not two that agree by inspection. Reclaiming a ticket and
   respawning its pane are then incapable of disagreeing, which is what makes
   re-running a spawn verb the whole recovery story.

3. **The owner string carries the pane's pid**: `pane:%33/28405`. tmux reuses
   pane ids across server restarts, and with no TTL a stale id that happens to
   match a live pane would wedge a ticket permanently rather than for four
   hours.

4. **No tmux server means every pane claim is dead.** No panes, no work in
   flight; the frontier reopens whole. The alternative — "cannot tell, hold the
   claims" — reinstates exactly the wedge this ADR removes, in the one situation
   where the human has visibly ended everything.

5. **The TTL is deleted**, along with `WorkClaimTTL`, `WorkClaim.Expired` and
   the expiry-steal path. Liveness is the only rule; two rules for ending a
   claim is one more than the domain has.

6. **A reclaim is reported once, and is not called a steal.** `ClaimResult`
   carries the claim it took over — `reclaimed ticket 02 from dead pane
   %33/28405 (claimed 11:57Z)` — because the prior session may have left
   half-written drafts, and that is the one cost of reclaiming. A dead pane is
   not a competitor, so the "stole it from another window" wording goes with the
   TTL.

7. **No release verb.** No `pop map release`, no new dashboard key. Killing the
   agent in a pane is already how a human ends a session, and rule 1 notices.
   Adding a verb would be a second way to do one thing, and the whole point of
   the design is that the recovery path is the ordinary path.

8. **Pre-format owner strings (`pane:%33`, no pid) are probed by rule 1 without
   the pid check.** No migration and no fold beyond that: the rows are
   short-lived by construction, and the worst case is one existence-only probe.

9. **A grilling session reads the ticket's existing artifacts before its first
   round.** `grill-with-map` says where drafts go and never says to look for
   ones already there, so a reclaimed ticket would re-grill clean and mint a
   second `adrs/<8hex>-*.md` beside the abandoned one. Idempotence has to hold
   for what the claim was protecting, not just for the claim.

## Considered and rejected

- **Shorten the TTL.** Picks a smaller wrong number. The information — is the
  session alive? — is free to read, so no timer approximating it is worth
  keeping.

- **Existence-only pane probe** (the first shape of the rule). Fixes ticket `02`
  and not ticket `01`: an abandoned pane sitting at a shell keeps its claim
  forever once the TTL is gone.

- **An explicit release verb as the primary path** (offered, then withdrawn
  during design). It leaves the human doing bookkeeping for a fact pop can
  observe, and every stuck-claim report would be a request for a keystroke
  rather than a re-run.

## Consequences

- Claim liveness costs one `list-panes -a -F '#{pane_id} #{pane_current_command}'`
  per Work load — a set, memoized for the load, never a probe per claim row.

- A claim held by a pane whose agent is genuinely still running cannot be broken
  without killing that agent. This is intended: that is a session, not a leak.

- An agent that legitimately drops to a shell mid-session loses its claim, and
  the ticket returns to the frontier. Accepted — a shell prompt in a grilling
  pane is indistinguishable from an abandoned one, and reclaiming is cheap now
  that decision 9 makes a respawn resume.
