# Set-scoped commands resolve their runtime path binding-first

The Drain routes to a Task set's **Worktree binding** when the set is bound, but `pop tasks verify` (accept/remediate/re-run) and `pop tasks status` resolved their runtime path from the current working directory instead. An Accept issued from the main checkout therefore wrote a human-authored PASS keyed to *cwd's* HEAD, while the Queue dashboard and daemon derived the same set's status at the *binding's* HEAD — where the stale non-PASS verdict still outranked it. The set stayed VERIFY-FAILED with no way to clear it, and `pop tasks status` could disagree with the dashboard about the same set.

Decision: **every set-scoped command resolves its runtime path from the set's Worktree binding when bound, and falls back to the current checkout only when unbound** — the law the Drain already followed, now named and applied uniformly. A Task set has one work checkout at a time; where a command was invoked from is not part of the set's identity, so it must not select which checkout's HEAD the set is read or written at.

## Considered Options

- **Keep cwd resolution and reconcile on read** — make status derivation prefer the latest PASS regardless of SHA. Rejected: it discards the SHA gate that makes verdicts mean anything, to paper over a routing defect.
- **Refuse set-scoped commands outside the binding** — correct but hostile; the binding exists precisely so the human doesn't have to stand in it.
- **Fix only `verify`** — leaves `pop tasks status` contradicting the dashboard, which is the same defect wearing a different hat.

## Consequences

An unbound set still resolves to the current checkout, so nothing changes for single-checkout work. Commands run from an unrelated repository against a bound set now act on the binding rather than silently on cwd; the **Assist session** refuses outright when cwd's repository identity doesn't match the set's.
