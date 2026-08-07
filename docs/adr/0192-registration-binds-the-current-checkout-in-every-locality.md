---
status: accepted
supersedes: "[ADR-0170](0170-to-tasks-defaults-to-managed-auto-drain.md) — the managed default is retired outright"
relates: "narrows [ADR-0181](0181-registration-default-routes-on-checkout-locality.md) — decisions 3 and 6 are overtaken; the verb, the config-blind locality rule and the auto-drain independence clause stand"
---

# Registration binds the current checkout in every locality

## Context

ADR-0170 made `--managed --auto-drain` the unconditional registration default.
ADR-0181 narrowed it to the trunk case: from the **Trunk worktree** the default
still forks a managed worktree, while from a linked worktree it registers plain,
bound to the checkout the human is standing in.

The locality branch fixed the wrong half. What a human wants when they break work
down is for the work to happen *here* — and that is as true standing on trunk as
it is standing in a worktree. The branch made the answer depend on where they
happened to be, so the same act produced two venues, and the trunk case silently
relocated the set into a checkout the human had not asked for. Isolation is a
thing to ask for, not a thing to receive by default.

Two facts shape the rest of the fix:

- **Requesting isolation was quietly turning unattended draining off.** Not in
  Go — `--managed` and `--auto-drain` are independent in `runTaskRegisterWith`
  (`cmd/tasks.go:413-450`), `SetTaskSetManagedIntent` never touches the consent
  bit, and `cmd/tasks_checkout_test.go:297` pins the independence. The loss is at
  the prose layer: the default table is scoped to "with no invocation arguments
  at all" and the keyword table maps `managed` / `isolated` → `--managed` without
  saying the default's `--auto-drain` survives it
  (`integrate/issue-tracker.md:112-120`, restated at
  `integrate/skills/pop/to-tasks/SKILL.md:190-192`). A publisher reading
  literally supplies `--managed` alone. The keyword table was fixed for one
  keyword at a time; the next keyword would have made the same mistake.
- **Draining trunk unattended is the hazard the managed default existed to
  prevent** (`integrate/issue-tracker.md:141-143`). Making here-by-default
  universal makes that the common path, deliberately.

## Decision

**Registration binds the current checkout, in every locality. A managed worktree
is provisioned only when explicitly asked for.**

1. **The default is plain register plus `--auto-drain`, everywhere.** ADR-0181's
   locality branch (its decision 3) is retired: `trunk` and `worktree` now
   produce the same flags. Registering eagerly binds the set to the current
   checkout the moment it registers, as the `worktree` branch already did.

2. **`managed` / `isolated` is the only route to a managed worktree.** It is
   unchanged in shape — it forks from the Trunk worktree and binds there — but it
   is now the sole path rather than an override of a detection.

3. **Auto-drain is a standing invariant, not a keyword-table entry.** It is on
   unless `no-drain` / `manual` turns it off, and **no other keyword affects
   it** — `managed` / `isolated` explicitly retains it. This is stated once in
   *Semantics*, where a publisher other than `to-tasks` will read it; the keyword
   table carries only the flag each keyword adds. ADR-0181's decision 4 (auto-drain
   is independent of `--managed`) is the clause this generalises, not one it
   replaces.

4. **`pop tasks register` warns when it binds an auto-drained set to trunk.** In
   Go, in `runTaskRegisterWith`, on the exact hazard condition: the consent bit is
   set, **Checkout locality** is `trunk`, and `--managed` was not passed. The
   predicate names no keyword, so `no-drain` and `managed` registrations are quiet
   without being special-cased. It is in Go rather than in the doc so it fires for
   every publisher — `pop map fan-out`, `to-spec`, a human at the shell — and so
   it cannot drift from the binding it is warning about, which is the failure mode
   ADR-0181 was written to close.

5. **`pop tasks checkout` survives with no consumer.** ADR-0181 built the verb for
   the locality branch, which is now gone, and decision 4 above is not a caller —
   it is the same `IsLinkedWorktree` predicate reached from inside the register
   path. The verb stays anyway: it is a correct, config-blind read of the current
   checkout, and its `--json` payload answers questions a human and a future
   caller both have. ADR-0181's decisions 1 and 2 stand unamended.

6. **The trunk-less fallback is deleted.** "The default asked for `--managed`, no
   trunk resolves, retry plain and warn" (ADR-0181 decision 6) can only fire where
   the default asks for `--managed`, and no default does now. An *explicit*
   `managed` / `isolated` against a repo with no resolvable trunk is refused as-is,
   as it already was; `--trunk <path>` names one.

## Considered Options

- **Keep the locality branch, flip only the trunk case's venue.** Identical
  behaviour to this decision, reached by leaving the branching machinery in place
  with both arms equal. Rejected: a branch whose arms agree is a branch waiting to
  be re-split, and it leaves the doc explaining a distinction that no longer makes
  one.
- **Daemon refuses an unattended drain on trunk, faulting the set as needs-bind.**
  Safer, and it puts the venue choice in front of a human at the moment it
  matters. Rejected: it reintroduces the friction this decision removes, one door
  further along — the human who typed nothing gets a fault instead of a worktree.
- **No warning at all; trunk is just another checkout.** Coherent, and it avoids a
  warning that now fires on the common path and will be tuned out. Rejected on
  balance: the state is genuinely consequential — an unattended agent committing
  and spawning panes on the branch you are standing on — and it is worth one line,
  once, at the moment `managed` is still typeable.
- **Warn from the publishing skill instead of from Go.** Rejected: it is exactly
  the drift surface of ADR-0181's context — a rule restated in a markdown body,
  diverging from the Go it imitates — and it would not fire for a human running
  `pop tasks register` by hand.
- **Fix the `managed` keyword bullet to say "keeps `--auto-drain`".** The minimal
  repair. Rejected: it fixes one keyword and leaves the shape that produced the
  bug, which is a table of keyword→flag mappings read as exhaustive.
- **A second ADR for the auto-drain invariant.** Rejected: the invariant only
  needs writing down because the default moved. One decision, one number.
- **Retire ADR-0181 outright.** Rejected: its verb, its config-blind locality
  rule, and its auto-drain independence clause are all still law. Only the policy
  that consumed the verb is being reverted.
- **Coin a term for "auto-drained set bound to trunk".** Rejected: a transient
  condition one warning mentions, not a concept other decisions hang off.

## Consequences

- The common path now drains in the checkout the human is standing in, trunk
  included. An unattended drain on trunk commits and opens panes on the current
  branch — accepted deliberately, announced once by the registration warning.
- A set registered on trunk arrives at drain **already bound to trunk**, so
  `RouteDrainCheckout` step 1 (existing binding) wins outright. The comment at
  `tasks/binding/route.go:243-248` — "the Queue faults an unbound, no-intent set
  as needs-bind before dispatch, so it never routes here to land on the trunk" —
  describes a path that is no longer the one trunk sets take. The reachability it
  denies is now the default.
- Managed worktrees become rarer and deliberate: they exist when someone typed
  `managed` or `isolated`, which makes their presence readable as intent.
- `pop tasks checkout` has no caller in the tree. It is retained as a read verb;
  if nothing adopts it, retiring it is a later, separate decision.
- The dashboard's drain-target picker default cursor moves from "new managed
  worktree" to the current checkout (`dashboard/dashboard.go:1835-1841`), so the
  two surfaces that express this default agree.
- Registrations that bind alongside another set in one checkout stay reachable and
  stay reference-counted (ADR-0116) — unchanged from ADR-0181, now reachable from
  trunk as well.
