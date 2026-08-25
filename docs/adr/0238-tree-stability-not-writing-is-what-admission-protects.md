---
status: accepted
---

# Tree stability, not writing, is what admission protects

## Context

Admission to a checkout was gated on a single coarse predicate in `StartDrain`:
`state='running' AND ((repo=? AND set_id=?) OR runtime_path=?)`
([store/drains.go](../../store/drains.go)). Everything that took a **Drain** row
was excluded from everything else, and everything that did not take one was
excluded from nothing. That produced two opposite errors at once.

On the permissive side, [ADR-0123](0123-work-dashboard-verify-verb-is-an-unlocked-verifier-force-ui-gated-on-quiescence.md)
gave the standalone `pop tasks verify` "no lock, no spawn intent" on the
reasoning that a verify pane is not a drain, and `pop tasks review`
([ADR-0214](0214-code-review-is-a-drain-step-that-maintains-a-living-document.md))
inherited the same freedom. Both run an agent over the tree — the Verifier runs
tests, the Reviewer reads files — so either can be handed a tree another set's
drain is actively rewriting. A verdict produced that way describes a state that
never existed.

On the restrictive side, the same predicate refused an **Assist session** while
a drain was live, even though assist reads only **Task storage** and takes no
claim at all (its **Checkout gate hold** is registered non-claiming). A human
wanting to look at a set was refused for the benefit of nobody.

The two mistakes share one cause: "writes to the tree" was standing in for the
property that actually matters.

## Decision

The criterion for exclusive admission to a **Runtime path** is whether an
operation needs the tree to hold still for its duration — a **Tree-stable
operation** — not whether it writes to it.

Tree-stable, and therefore exclusive: an AFK task attempt, the **Verifier**
(including standalone `pop tasks verify` and the dashboard `v` verb), the
**Reviewer** (including standalone `pop tasks review`), and **Fold**. This
supersedes ADR-0123's "no lock, no spawn intent": the lighter surface stays
lighter in every other respect — no drain lifecycle, no terminal recording — but
it takes the claim.

Not tree-stable, and therefore always admitted: operations touching only **Task
storage** — the **Work dashboard**, `pop tasks show`, and an **Assist session**'s
inspection. Assist's *mutating* verbs (Accept, Remediate, Fold) are tree-stable
and acquire like anything else.

The **Runtime shell** is a deliberate exception: tree-mutating in effect, left
unlocked. A claim a human can hold indefinitely by leaving a tab open would
stall the checkout with no failure to attribute it to, and pop cannot enforce
what a human types at a prompt anyway. Its banner says so.

The old predicate's two disjuncts are separated: the `runtime_path` arm is the
**Checkout claim** (tree stability), and the `(repo, set_id)` arm becomes a
named **Set claim** (one drain of a set across all checkouts). They are
different resources, they queue separately, and they report separately.

## Considered options

- **Keep "writes to the tree" as the criterion.** Rejected: it is precisely the
  rule that let ADR-0123 ship an unlocked Verifier, and it cannot express why a
  read-only Reviewer must not run during a drain.
- **A reader-writer lock — many readers or one writer per checkout.** Rejected
  after examination: once the Verifier and the Reviewer are both classified
  tree-stable, the shared mode has almost no members, and the one real reader
  (assist's inspection, over Task storage) contends for a different resource
  entirely. A mode on a lock nobody takes in shared mode is machinery without a
  user.
- **Leave assist refusing during a live drain.** Rejected: assist claims nothing
  and the concurrent-disposition risk it was guarding is already covered
  precisely by **Checkout quiescence**, per set.

## Consequences

- Supersedes ADR-0123's locking decision. The dashboard `v` verb can now wait
  where it previously fired immediately, so it gains an **Admission indicator**
  like any other queued command.
- Amends [ADR-0135](0135-admission-to-a-checkout-is-gated-on-the-checkout-claim-union.md):
  the claim union grows two members (standalone verify and review) and the
  refusal predicate splits into two named resources.
- Assist becomes startable in every case that is not physically impossible — the
  set exists, there is a TTY, there is a checkout. The live-drain refusal, the
  archived-set refusal and the malformed-manifest refusal are gone from the
  entry path; a malformed manifest is now something assist opens *to show you*.
- An idle **Runtime shell** can still let two sets rewrite one tree. Accepted,
  and unchanged from today.
