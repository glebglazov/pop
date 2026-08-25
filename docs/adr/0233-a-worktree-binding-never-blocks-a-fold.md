---
status: accepted
amends: [ADR-0229]
---

# A Worktree binding never blocks a Fold; only an unrequested act does

> **Relates:** amends [ADR-0229](0229-fold-is-an-act-on-a-checkout-performed-on-a-scratch-branch.md). Everything that ADR decided about the *mechanism* stands whole — fold's subject is a checkout, the rebase rewrites a **Fold scratch branch**, the fast-forward is the one irreversible act, recovery has exactly two regimes read from git alone. What changes is one clause about *eligibility*: that a bound row must answer through its set.

## Context

ADR-0229 gave `pop worktree fold` and the **Worktree picker**'s Fold action a
gate: offered on rows with "no live non-archived binding", because "a row that
*is* bound keeps answering through its set, so the picker never becomes a route
around a set's status gate." In practice that reads as pop refusing a plain git
operation on the strength of a record pop wrote itself. A **Worktree binding**
is bookkeeping — which set is using which checkout — not a fact about the
repository. Nothing about it prevents rebasing a branch onto trunk and
fast-forwarding trunk; the rebase would succeed identically with the row
unbound.

The refusal also had a second cost. `FoldCheckout` has no *tail*: the sign-off
and binding release live in `Fold`'s `completeFoldSetTail`. So the advice the
refusal gives ("use `pop tasks fold <set>`") was the only route by which a bound
checkout could ever be landed, and the gate existed to protect a set's status
gate rather than the repository.

Meanwhile the surviving refusals had no stated principle behind them. They were
a list — detached, dirty, claimed, already-contained, ambiguous scratch — with
no rule saying why those and not others, and therefore no way to judge the next
candidate.

## Decision

**1. A live Worktree binding raises a confirmation, never a refusal.** Fold asks
the **Bound-checkout fold confirmation** and obeys the answer. The eligibility
clause in ADR-0229 is withdrawn: `pop worktree fold` and the picker's Fold
action are offered on every row.

**2. The line between refusing and asking is the Unrequested-act refusal
rule.** Fold refuses only where it cannot proceed without performing an act
nobody asked for — inventing a branch for a detached HEAD, stashing a dirty
checkout or trunk, waiting out or evicting a **Runtime execution lock** holder,
guessing at an unclassifiable **Fold scratch branch** — or where there is no act
to perform at all, as with the **Trunk worktree** itself or a branch already
contained in trunk. Everything else fold simply does, after saying what looks
strange about it. Tested against all ten of fold's refusals, the bound checkout
is the only one that changes side.

This is the reusable half of the decision; "bound checkouts confirm" is its
first application. It also explains why a *dirty* trunk still refuses even
though a bound trunk never did: the obstacle there is uncommitted work pop would
have to move on someone's behalf.

**3. The confirmation is uniform across every non-foldable status.** "In
progress" is not a status in pop's model, so the rule names states instead:
anything outside **DONE** and **Awaiting-approval** — READY, BLOCKED, DEFERRED,
FAILED, NEEDS-VERIFY, VERIFY-FAILED, MALFORMED — gets the same confirmation,
which *names the status*. NEEDS-VERIFY keeps no privileged refusal: "verify
first" is advice, and advice reaches the human through the prompt without
becoming a veto. A graded rule would reintroduce pop's judgement through a side
door.

**4. A confirmed fold releases only a foldable set's binding.** For a DONE or
Awaiting-approval set the fold runs the sign-off and `binding.Delete` — the
release *is* what finishing means, and withholding it strands the set (its
branch now equals trunk, so `pop tasks fold` afterwards meets the
already-contained refusal, with no verb to clear the binding). An unfinished set
keeps its binding: the fold does not require the release, so performing it would
be exactly the unrequested act decision 2 forbids. It would also move the set's
home — an unbound set's next drain *provisions a fresh managed worktree* rather
than adopting the old one, leaving the original an orphan.

**5. The release skips teardown.** ADR-0229 and the verb's own contract promise
the checkout verb deletes nothing on success, so a binding released here does
not run **Managed-worktree teardown reference count**. "Fold W into trunk" is
not "fold W and remove it".

**6. Several bound sets are listed individually, and decision 4 applies per
set.** A checkout can hold more than one binding (`liveReferents` returns a
list). The confirmation names each set with its status and says which bindings
will be released, rather than collapsing them to a count.

**7. `--yes` answers the confirmation, and the fold prints the override.**
`--yes` is not an extra-danger opt-in; it is the entry ticket for any
non-interactive channel, since `confirmYesNo` fails without it on a non-TTY. A
caller passing it has said "don't ask me", and pop does not second-guess which
question was meant. Nothing stops fold leaving a record, so it states
unconditionally that it landed a bound set of status X without asking. The
interactive path is unaffected: the picker spawns a bare `pop worktree fold`
into a tagged pane, with a real stdin and no `--yes`.

## Considered options

- **Keep the refusal, add a `pop tasks unbind` to escape it.** Rejected: it
  makes a new verb the price of a gate that should not have been there, and
  leaves the bookkeeping-vetoes-git shape intact.
- **Release every binding on fold, foldable or not, and let the human rebind
  with `pop tasks bind-worktree`.** Considered seriously and rejected under
  decision 2 — it is an unrequested act, and it silently relocates an
  in-progress set on its next drain.
- **Grade the confirmation by status, keeping NEEDS-VERIFY and MALFORMED as
  refusals.** Rejected per decision 3. MALFORMED is the weakest case for
  admitting, since pop cannot read the manifest and so cannot say what is being
  overridden; it is admitted anyway for one rule rather than nine.
- **Give the confirmation its own flag so `--yes` cannot reach it.** Rejected
  per decision 7, against the recommendation of the grill: it would make `--yes`
  mean different things to different questions.
- **A second noun for the partial landing, leaving Fold to mean the finished
  case.** Rejected: this is ADR-0229's already-rejected "separate Worktree fold
  concept" arriving by another road. "Finished" was always a property of the
  **Task-set fold** specialization, not of the act, so **Fold** widens instead.

## Consequences

- ADR-0229's "the picker never becomes a route around a set's status gate" is
  **withdrawn**. It *is* such a route now, deliberately, with a confirmation in
  place of the gate. The rest of ADR-0229 is untouched and remains the reference
  for fold's mechanism and recovery.
- `FoldCheckout` gains a conditional tail. It stays a primitive — the tail runs
  only for a resolved foldable set — but it is no longer true that only `Fold`
  settles set business. The tail's position relative to the **Fold boundary** is
  unchanged, so ADR-0229's convergence story covers it as-is.
- The refusal wording that pointed at `pop tasks fold` disappears; the behaviour
  it advised becomes what the verb does.
- `LiveBoundSetIDs`' habit of dropping a binding whose key yields no set id
  stops mattering. That branch is unreachable: keys are `repoKey + NUL + setID`,
  neither half can contain a NUL, no `Put` path passes an empty id, and
  `modernc.org/sqlite` round-trips embedded NULs. Only a hand-edited row would
  produce one, and this decision deliberately makes no provision for it.
- Nothing in the trunk-side story changes, because there never was a trunk-side
  binding check. Trunk may carry any number of bound sets; the only questions
  fold asks of trunk are cleanliness and the lock.
