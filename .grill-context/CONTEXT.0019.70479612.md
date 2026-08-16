---
fragment: 70479612
generation: 0019
branch: master
---

+ Newest-first identifier order
  The single ordering pop applies wherever it offers **dated identifier**s for a
  human to choose from — every shell-completion candidate list of Task sets or
  Maps, plus the `pop tasks status` and `pop map status` tables. It is a reverse
  lexicographic sort of the identifier string itself, so the most recently dated
  Task set or Map is offered first. Because the `YYYY-MM-DD` prefix is
  fixed-width and leading, sorting the folder name and sorting the date agree,
  and no parsing is involved. Completion returns it with cobra's `KeepOrder`
  directive, without which the shell re-sorts the list alphabetically and the
  order never reaches the human. **Sequence identifier**s are excluded and stay
  ascending.
  avoid: recency sort, created_desc (that is the Work view preset value), reverse alphabetical

+ Sequence identifier
  The `NN-slug` identifier form pop gives the members *inside* a container — a
  Task set's task files (`01-layout-module.md`) and a Map's Decision tickets
  (`01-session-directory-shape`). Its leading number encodes intended order of
  work, not recency, so every surface offering one keeps it ascending. This is
  the deliberate counterpart to **Newest-first identifier order**: pop reverses
  dated identifiers and never reverses sequence identifiers, so picking a
  container starts at the newest and walking its contents starts at the first.
  avoid: task number, ticket number, ordinal id

~ Task set priority
  A numeric value used to choose between ready Task sets. Newly registered Task
  sets start at priority `0`. Higher priority wins; equal-priority Task sets are
  broken by **Newest-first identifier order**, so the more recently dated set is
  preferred. Unattended supervisor dispatch is the exception — it re-sorts on
  registration order itself and is unaffected.
  was: A numeric value used to choose between ready Task sets. Newly registered Task sets start at priority `0`. Higher priority wins; equal-priority Task sets retain registration order.

~ Next task
  Selecting and executing one task from the highest-priority Ready Task set.
  Non-runnable Task sets are reported and skipped; among Ready Task sets, equal
  priority is broken by **Newest-first identifier order**.
  was: Selecting and executing one task from the highest-priority Ready Task set. Non-runnable Task sets are reported and skipped; among Ready Task sets, equal priority retains registration order.

~ Status table
  The non-interactive summary printed by `pop tasks status` after discovery
  refresh. **Archived Task set**s are excluded from the default table; when at
  least one exists, a quiet footer reports the archived count and the `pop tasks
  status --archived` command that lists them, so filed-away work stays
  discoverable. `--archived` instead renders only the Archived Task sets. In the
  default table, Missing Task sets appear first as stale registrations, followed
  by Done Task sets. The partition is what the table is for, so it survives
  intact; within each group **Newest-first identifier order** breaks ties.
  Remaining discovered Task sets then appear in scheduler order: descending
  priority with newest-first identifier order for ties, so the user can read the
  active schedule top-to-bottom to understand which Ready work will be selected
  first. That top-to-bottom reading is load-bearing — the interactive picker
  behind a bare `pop tasks implement` walks this same order and takes the first
  Ready row, so the table and the pick can never disagree. The automatically
  selected Ready Task set is marked explicitly. Before execution, the actual
  implement target is also marked; when an explicit Task set override differs
  from the automatic selection, the table shows both markers on their respective
  rows. The checkout note describes where a whole-set **Implement** would run by
  default: the bound checkout when the set has a **Worktree binding**, otherwise
  the **current checkout** (a **Default binding** is recorded there on first
  drain; a **Worktree directive** routes only **Work supervision**, not a
  foreground Implement). Single task-file runs are still current-checkout
  operations. An interactive tasks dashboard is deferred until the table
  workflow is exercised.
  was: The non-interactive summary printed by `pop tasks status` after discovery refresh. **Archived Task set**s are excluded from the default table; when at least one exists, a quiet footer reports the archived count and the `pop tasks status --archived` command that lists them, so filed-away work stays discoverable. `--archived` instead renders only the Archived Task sets. In the default table, Missing Task sets appear first as stale registrations, followed by Done Task sets. Remaining discovered Task sets then appear in scheduler order: descending priority with stable registration order for ties, so the user can read the active schedule top-to-bottom to understand which Ready work will be selected first. The automatically selected Ready Task set is marked explicitly. Before execution, the actual implement target is also marked; when an explicit Task set override differs from the automatic selection, the table shows both markers on their respective rows. The checkout note describes where a whole-set **Implement** would run by default: the bound checkout when the set has a **Worktree binding**, otherwise the **current checkout** (a **Default binding** is recorded there on first drain; a **Worktree directive** routes only **Work supervision**, not a foreground Implement). Single task-file runs are still current-checkout operations. An interactive tasks dashboard is deferred until the table workflow is exercised.
