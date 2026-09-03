---
status: accepted
---

# The fast-pass override reaches who answers, and the close asks what to do next

`grill-with-docs-fast` was starting implementation on its own. The cause is structural, not a wording slip: what stops an ordinary grilling session from acting is one sentence in the pinned upstream `grilling` text — "The _decisions_ are the user's — put each to them and wait" — and the fast composer's marked override negates that sentence by name (ADR-0253 decision 2) while restating nothing in its place. Against an opening message such as "investigate and fix X", "decide the rest yourself" generalises from *decisions* to *the work*. Two rules answer it: the override is bounded to who answers a question, and both composers' close ends by asking what to do with the settled plan. This amends [ADR-0253](0253-a-fast-pass-composer-decides-what-it-can-find-or-reverse.md) decisions 2, 4 and 6; everything else in it stands.

## Decision

1. **The ask/decide filter governs who answers a frontier question, never whether the work starts.** Implementing the change under discussion is not a **Fast-pass decision** at any confidence. The session's product is a settled plan and its artifacts; the user's word starts the implementation. Fact-finding is unrestricted — read and search anything — and the writes stay what they always were: glossary fragments, ADRs, and a prototype a frontier question genuinely needs.

2. **An imperative in the opening message is the subject of the grilling, not a licence to act on it.** "Investigate and fix X" means grill X, settle it, commit it, and ask at the close. This is stated outright because it is the observed trigger and because the session is not being disobedient by its own lights — the user *did* ask for a fix, so a rule that only bounds the override leaves the case ambiguous.

3. **The bounding clause lives in `grill-with-docs-fast`'s body, beside the override it bounds.** `grill-with-docs` needs no copy: it never negates the wait-sentence, so it still gets the guard from the `grilling` text it inlines. An override and the limit of that override are one thought, so they sit under one heading rather than in two files.

4. **`GRILL-SESSION.md`'s close gains a fifth step, shared by both composers: commit, then ask what happens next, and wait.** The two named options are implement it now in this session, or leave it for a separate step (`to-tasks`). Either answer is an instruction the session then carries out — "now" means implementing it in the same session, where the grilled context still lives, rather than handing off to a fresh one.

5. **The commit stays automatic and ungated; only the hand-off is asked.** The order is commit first, then ask, because `to-tasks` forks a worktree from HEAD and an "implement now" answer should start from a clean tree with the plan already at HEAD. The user's answer to a round is what signals the design is settled; a fast pass's reported ledger is not.

6. **The fast pass restates its whole ledger as its own block ahead of the close's ask.** An override of a fast-pass decision then costs no extra turn at the close, exactly as the per-round ledger placement already achieves mid-session. The commit-body ledger under `Decided without asking:` is unchanged.

## Considered Options

- **Restate the wait-sentence in `GRILL-SESSION.md` so both composers carry it.** Rejected: `grill-with-docs` already has it from the inlined `grilling` text, so this makes a second copy of a live rule — the duplication the shared document exists to prevent, and which its own no-duplication rule and `integrate/grill_composition_test.go` both police.
- **Sharpen the pinned `grilling` text instead.** Impossible: that region is a byte-verbatim upstream copy under a drift pin (ADR-0112), and editing it forfeits the diff that makes drift reviewable.
- **Bound the override by a path whitelist** (writes confined to `.grill-context/**` and `docs/adr/**`). Rejected in favour of decision 1's phrasing: the boundary the user cares about is their agreement, not a directory, and a whitelist invites the reading that anything unlisted is fine once the paths are satisfied.
- **Ask before committing, so one question covers both.** Rejected per decision 5: it re-opens the commit-without-asking rule ADR-0253 settled, and leaves the artifacts off HEAD in the branch where the answer is `to-tasks`.
- **Hand "implement now" to a fresh session.** Rejected per decision 4: the whole value of asking at the close is that the grilled context is still live, and a fresh session pays to rebuild it.
- **Soften it to a preference ("prefer not to implement").** Rejected: the override's force comes from being a checkable two-part test, and a speed-optimising session reads a hedge straight past.

## Consequences

- Both composers now end on a question rather than an assumption. `grill-with-docs` changes behaviour too — it used to stop after the commit and leave the next step to the user unprompted.
- A fast pass can no longer close a loop end-to-end unattended. That is the point: the round budget buys fewer questions, not an unattended implementation.
- The fast body carries one rule its sibling does not, which is a legible asymmetry only because decision 3 ties it to the override in the same section.
