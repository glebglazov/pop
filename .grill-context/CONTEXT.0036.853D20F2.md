---
fragment: 853D20F2
generation: 0036
branch: master
---

+ Tree-stable operation
  An operation that requires the checkout tree to hold still for its duration,
  whether or not it writes to it. This — not "does it write" — is the criterion
  for exclusive admission to a **Runtime path**. An AFK task attempt qualifies
  because it edits the tree; the **Verifier** qualifies because it runs tests
  another set's drain would break; the **Reviewer** qualifies because files
  changing underneath it yield a review of a state that never existed, despite
  its read-only agent posture (ADR-0221) and its **Review artifact** landing in
  **Task storage**. The standalone `pop tasks verify` and `pop tasks review`
  therefore take the claim like any drain, superseding ADR-0123. Its complement
  is not "read-only" but **Task storage**-only: the **Work dashboard**,
  `pop tasks show`, and an **Assist session**'s inspection contend for nothing
  and always run. The **Runtime shell** is the deliberate exception — tree-
  mutating in effect, left unlocked because a claim a human holds by walking
  away stalls the queue with no failure to blame.
  avoid: write operation, mutating operation, exclusive operation

+ Admission wait
  What a human-initiated **Tree-stable operation** does instead of refusing when
  the checkout is held: it blocks until a window opens, unbounded, rather than
  handing the retry back to the human. The wait line is actionable rather than a
  spinner — it names the holder and where to reach it (set, **Claim reason**,
  PID, controlling tty, drain pane), because the resolution is almost always to
  go and answer the prompt that is still open. No timeout: a bound would return
  the waiting to the human at the least predictable moment. Non-interactive
  callers keep today's behaviour — the **Work daemon**'s **Spawn deferral**, a
  script's non-zero exit — because the rule is that a *human* never re-runs a
  command, not that no code path ever reports busy.
  avoid: lock retry, busy error, blocking refusal

+ Admission queue
  The per-**Runtime path** line of waiters under **Admission wait**, ordered by
  strict registration FIFO and blind to **Task set priority**: of two sets
  already waiting, the one that asked first goes first. Priority still decides
  which Ready set the **Work daemon** picks next; it does not reorder a queue
  that has already formed. Distinct from **Recovery turn ordering**, which is
  priority-then-FIFO among **Recovery waiter**s.
  avoid: drain queue, admission order, priority queue

+ Set claim
  The sibling of **Checkout claim**, keyed on (**Repository identity**, task set)
  rather than on the tree: at most one drain of a given set across all
  checkouts. Named because under **Admission wait** it becomes something a
  command waits on and must therefore report — "waiting for set A (drained
  elsewhere)" is a different line from "waiting for checkout /path". Formerly
  the unnamed first disjunct of `StartDrain`'s refusal predicate.
  avoid: drain uniqueness, per-set lock

+ Admission grant
  The acquisition that ends an **Admission wait**: a waiter holds nothing while
  it waits and takes its whole lock-set — **Checkout claim** and **Set claim**
  together — inside one transaction, or keeps waiting. All-or-nothing is what
  keeps a never-refusing queue deadlock-free, since a waiter that holds no
  partial lock cannot participate in a cycle. On grant the operation re-derives
  its target's status: work that finished while it waited is a clean zero exit,
  not an error.
  avoid: lock acquire, turn taken, incremental acquisition

+ Admission indicator
  The **Work dashboard** row marker for a set with a queued command awaiting an
  **Admission grant**, sibling to the `●` **Live-drain indicator** and rendered
  beside the derived **Task set status** rather than replacing it. Waiting is an
  execution fact, so it never becomes a status value — the same separation that
  retired the IN-PROGRESS drain column in ADR-0111.
  avoid: WAITING status, queued status, blocked status
