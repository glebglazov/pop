# Code review is a drain step that maintains a living document

Pop needed a way to judge whether a Task set's changeset adheres to the coding
standards of the human and the repository — a different question from whether it
met its acceptance criteria, which Agent verification already answers. We added
**Code review** as a step in the drain loop, running after the verify phase and
before the terminal switch: a fresh Reviewer reads the resolved `code-review`
convention and the previous Review artifact, and writes the current one. It
reaches no verdict, gates nothing, and spawns no work. The whole output is a
single living document a human may act on or ignore.

This **overrules a ruling recorded in
[ADR-0207](0207-commit-conventions-resolve-at-plan-time-into-planned-subjects.md)**
(itself already superseded by ADR-0211 on its main subject): "Code review is out
of scope; if wanted later, it is a final task `/to-tasks` appends to the set, not
a new pipeline mechanism." That is now wrong on its central claim. Re-review is a
normal act — you review, you change the code, you review again — and a task
cannot be re-run without being re-created. Encoding review as data forces a human
to manufacture a new task every time they want a second opinion on the same set.

## Considered options

**Review as a task appended by `/to-tasks`** — ADR-0207's ruling, and the option
most consistent with pop's house style of closed pipelines and open data. It
buys per-ticket granularity for free (five tasks against five tickets get five
review tasks, each committing under its own subject) and adds no mechanism at
all. Rejected on re-runnability, above. It also cannot guarantee independence: a
review task drains through `[work.implement].agents` like any other, so the agent
that wrote the code can review it.

**Review as a mode of Agent verification** — one step, two prompts. Rejected
because the two answer different questions with different authority. Verification
checks claims against acceptance criteria and its verdict gates status;
review has no criteria to check against, only prose standards, and gates
nothing. Folding them would put two facts in one verdict slot — the mistake the
Verification mark exists to avoid.

**Review with a PASS/FIXABLE verdict that auto-spawns Remediation tasks** — the
initial design, and the reason review was first placed *before* verification.
Rejected: automatic remediation from a standards opinion means the drain
rewrites code on aesthetic grounds with no human in the loop, and it creates a
re-arming loop the Verification episode rule would have to be carved up to
break. Removing the verdict removed both problems, and with it the reason for
the early placement.

**Pop shipping a default coding standard** — rejected in favour of a Convention
recipe that derives the standard from the repository's own idiom, linters and
docs. The standard belongs to the codebase, not to the tool; this is the same
instinct that makes the `commits` recipe skip pop-generated commits so pop never
learns its own accent.

## Consequences

- **The step runs after the verify phase**, immediately before the terminal
  switch. Verification may spawn a Remediation task and move the tree; a document
  written before that would describe a changeset that no longer exists at the
  moment the human reads it.
- **The review runs in the drain's own pane, not one of its own.** A pop pane is
  a detached process — `EnsureTaggedPane` sends a shell command and returns
  without waiting — so a Reviewer spawned into one would let the drain reach the
  terminal switch before the document existed, losing the ordering the bullet
  above buys. The in-drain review is therefore an in-process call, streaming into
  the drain pane exactly as the implement and verify phases do; only its captured
  telemetry is separated, under a `review` phase label. The general rule, of
  which verify was already an instance: **an in-drain phase streams into the
  drain's pane, and a dashboard verb spawns a tagged pane of its own.**
  `TagVerify` panes belong to `LaunchVerify`, the dashboard's on-demand verb, not
  to the drain's verify phase. A dashboard Review verb would take a `TagReview`
  pane on the same grounds.
- **The Reviewer is not given diff bodies.** It gets the commit range and the
  Work diff view for orientation and reads the changed files itself, so the
  prompt stays bounded regardless of set size. Pop's review prompt must say so
  explicitly — a Reviewer that judges naming and structure from a `--stat` table
  is judging nothing.
- **Each review supersedes rather than appends.** The Reviewer reads the previous
  Review artifact and writes the current one; every reader takes the latest.
  Prior documents are retained under the set's `reviews/` directory, and latest
  is resolved by timestamp.
- **The artifact is a Task artifact and never enters an Implementation commit.**
  Getting it into a PR is an explicit human act: `pop tasks review <set> --show`
  prints to stdout and piping is the user's business. Pop does not author
  repository content here.
- **`pop tasks review <set>` runs a review** — symmetric with `pop tasks verify`
  — and may be run by hand against any set with at least one done AFK task and a
  non-empty commit range, including mid-drain, where a standards correction is
  worth most. Automatic review fires only at AFK quiescence in an armed Review
  episode.
- **A Review episode needs no carve-outs.** Because review spawns no work it
  cannot re-arm itself, so it mirrors the Verification episode rule directly:
  reviewing disarms automatic re-review, and new done-AFK work re-arms it.
- **`code-review` joins the closed Convention kind enum**, which ADR-0211's stack
  was already built to hold. `[work.review]` carries `enabled` (off by default),
  `agents` and `effort`, and nothing else — there is no remediation depth because
  there is no remediation.
- **The human meets the artifact at the HITL sign-off gate**, by a pointer and a
  paging verb rather than inlined content, with the same pointer in the Task set
  detail view and — the load-bearing one — named in the Assist prompt, so an
  assistance agent discovers and reads it without any new plumbing.
- **The pipeline stays closed.** This adds one hard-coded phase, one directive
  enum, one store table, one config group and one convention kind. It does not
  add a step registry, and users still cannot declare pipeline steps: their
  flexibility surface is the Convention stack's prose, not pop configuration.
