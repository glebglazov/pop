---
status: accepted
amends: [ADR-0229, ADR-0156]
---

# A fold that is rebased but not landed resumes at a landing gate

> **Relates:** amends [ADR-0229](0229-fold-is-an-act-on-a-checkout-performed-on-a-scratch-branch.md) — its subject, its scratch-branch mechanism, its one irreversible boundary and its read-state-from-git rule all survive whole; what changes is that git's four signals resolve into **four** scratch states rather than three. Amends [ADR-0156](0156-fold-rebases-onto-trunk-and-folds-awaiting-approval-through-a-looping-conflict-prompt.md) by taking the post-resolution beat out of the Fold conflict prompt.

ADR-0229 says a **Fold scratch branch** found at preflight means one of three things: parked, residue, or ambiguous-and-refused. In practice there is a fourth, and it is the one a human is most likely to create. `offerFoldPostResolveVerify` runs after `git rebase --continue` completes and before the fast-forward, and a **Verifier** run inside it holds that window open for minutes. Interrupt it there — which is what a human does, because the offer is a bare `Verify set? [y/N]` whose *both* answers continue the fold, so wanting out leaves only `Ctrl-C` — and the checkout is left standing on the scratch ref at the rebased tip, with no rebase in progress, the real branch still at its pre-fold tip and trunk unmoved. Classified against ADR-0229's three states that is *ambiguous*: the next fold refuses by name and tells the human to inspect, delete or rename a ref fold itself created, one commit short of the finish. Reproduced against real git.

Decision, in three parts.

**Git tells this state apart from the other three.** A scratch ref that trunk is an ancestor of, and that the real branch does not reach, is a fold that got past its rebase and stopped before the **Fold boundary**. That is one more `merge-base --is-ancestor` over refs already being read, so the recovery model stays what ADR-0229 made it: read from git, never journalled. The rejected alternative is unchanged from ADR-0229's own list — a progress journal in pop's data dir is a second source of truth about git that can go stale.

**The post-resolution beat becomes the Fold landing gate.** The `[y/N]` grows into a menu — **land now** (Enter default), **verify set**, **abandon**, **exit** — printing the three tips the choice turns on: trunk, the set branch at its pre-fold tip, and the scratch ref at the rebased tip. It is a sibling of the **Fold conflict prompt**, not a rename: that prompt's subject is a rebase that stopped, this one's is a rebase that succeeded, and its items (agent assistance, resume) mean nothing once there is nothing to resolve. It is reached from exactly two places — after a conflict resolution completes the rebase, and on re-entry into the rebased state — so an interrupted fold lands back where it was interrupted. It deliberately does **not** appear on a clean fold: a rebase that met no conflict has nothing to report, and stopping there would make every attended fold interactive for no answer.

**An ambiguous ref keeps its refusal and gains an escape.** With the rebased state named, `foldScratchAmbiguous` shrinks to refs pop genuinely cannot account for. Those still refuse to be guessed at, but the refusal now prints the same three tips and offers the acts pop could take — discard as residue, reset and fold from scratch, exit — with **no default**, so nothing happens without a typed choice. **Unrequested-act refusal** is about pop not choosing, not about the human having to leave the tool to act on their own answer.

`--yes` and any non-interactive re-entry land a rebased-not-landed fold rather than refusing: the fast-forward is precisely the act that was requested, and the rebase it follows was already approved by a human at a TTY.

## Consequences

- The conflict prompt's own `Verify set` item is left as it is, and is now known to stamp its verdict on a mid-rebase detached HEAD (`verifyWorkSHA` reads the checkout's HEAD). The sound moment to verify a folded result is the landing gate; repairing or removing the mid-rebase one is a separate decision.
- `Ctrl-C` during the gate's Verifier run returns to the gate rather than ending the process, as an **Assist session**'s interrupt during an **Admission wait** already does. The recovery path is still load-bearing: it is what survives `kill -9`, a closed pane, or a crash.
- ADR-0229's "one of three things" sentence and its ambiguous-is-refused clause are amended, not retired. Nothing about the boundary, the convergence rule or trunk-is-never-unwound moves.
