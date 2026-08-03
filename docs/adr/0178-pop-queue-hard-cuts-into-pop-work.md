---
status: accepted
---

# `pop queue` hard-cuts into `pop work`, and the Queue vocabulary is rewritten in place

## Context

`pop queue` names a scheduler that no longer matches its own scope. It already
fires Routines as well as draining Task sets, its dashboard was renamed to the
**Work dashboard** when Maps joined the table (leaving `pop queue dashboard` as
a hidden alias), and this map's ticket 05 generalises the supervisor further
into a Work supervisor advancing any kind with AFK-advanceable units.

The word is load-bearing in five places at once:

- the CLI (`pop queue run|status|log`, plus the hidden `dashboard` alias),
- the `queue/` package — a grab-bag holding the dashboard TUI, the supervisor,
  Task-set drain control, and Routine dispatch,
- the on-disk lock directory `~/.local/share/pop/queue/` and the tmux window
  `pop-queue`,
- the `[queue]` config section (`poll_interval`, `agent_quota_retry_after`,
  `crash_retry_delays`),
- ~13 `Queue *` glossary terms and ~15 ADRs.

Ticket 03 (`work` as a kind-agnostic seam), ticket 05 (the `Advancer` split) and
ticket 06 (the paged dashboard) each move large amounts of this code. Renaming
before them means renaming twice.

## Decision

`pop queue` is deleted and its surface lands under `pop work`, as a hard cut with
no aliases, in a single slice set sequenced **after** tickets 03, 05 and 06.

**1. Verb map.** `pop work daemon | status | log`, joining the existing
`dashboard | show-path`. `run` becomes `daemon`; the hidden `pop queue
dashboard` alias is deleted with the rest.

**2. The name `daemon` does not change the behaviour.** The supervisor stays
foreground, single-instance, `SIGINT`-stopped. No `start`/`stop`/service verbs
are added by this cut. The glossary entry says "foreground despite the name" and
drops "background service" from its avoid-list, keeping "Monitor daemon" — pop
has two daemons and the glossary must tell them apart.

**3. The cut goes past the CLI.** The tmux window becomes `pop-work`, the
supervisor's output prefix becomes `work:`, and the lock directory becomes
`~/.local/share/pop/work/`. Since a running pre-cut daemon holds the old lock
path invisibly to a post-cut binary, the new daemon checks **both** paths for
liveness and refuses to start if either is live; the old-path check is deleted
one release later.

**4. `queue/` ceases to exist.** It splits four ways: the TUI (`dashboard.go`,
`render.go`, `livepane.go`) to `dashboard/`; the supervisor (`supervisor*.go`,
`queue.go`, `status.go`, `log.go`, `run_output.go`) to a top-level
`supervisor/`; Task-set drain control (`draincontrol.go`, `abandon.go`,
`bind_worktree.go`, `fold.go`, `deferral.go`, `representative.go`) to `tasks/`;
Routine dispatch (`routines.go`) to `routine/`. It cannot merge into `work/` —
ticket 03 forbids that package importing any kind.

**5. `status` and `log` generalise; Maps stay out.** `pop work status` reports
what the daemon can advance: two sequential tables, Task sets then Routines,
with **no Map rows** — even though the dashboard's page A renders Maps from the
same shared row builder. `pop work log` covers Routine fires and skips alongside
Drain events. Maps never auto-advance, so a supervisor status that listed them
would be reporting rows it can never act on.

**6. Config hard-cuts too.** `[queue]` becomes `[work.daemon]` — daemon-tuning
keys under a daemon-scoped table, leaving `[work]` free for later non-daemon
keys. A leftover `[queue]` table produces an unknown-section finding rather than
being read as a deprecated alias.

**7. Historical ADRs are rewritten in place.** The ~15 ADRs carrying Queue
vocabulary are retitled, `git mv`'d, and their bodies re-worded — **words only,
never decisions**: what was decided then still reads as decided then, in current
terms. Only ADRs where "queue" is a *term* are touched; incidental prose is left
alone. This is cheap because cross-references are by number (`ADR-0027`), never
by path — exactly one path reference exists repo-wide.

**8. Slice order.** Package split → CLI verb move and alias deletion → config
section → lock-path handover → glossary and ADR corpus rewrite last, so the
documentation describes a tree that already exists.

**9. Independent of the Map rename.** Ticket 02's `pop wayfinder` → `pop map`
cut ships before ticket 03; this one lands after 06. Two glossary passes, two
ADR passes, no coupling — the map rename is not held back to bundle them.

### Superseded vocabulary

| before | after |
|---|---|
| Queue (the concern) | Work supervision |
| Queue daemon | Work daemon |
| Queue scope | Supervision scope |
| Queue journal | Work journal |
| Queue run output / baseline / delta | Daemon run output / baseline / delta |
| Queue status summary | Work status summary |
| Queue window | Work window |
| Queue backoff | Drain backoff |
| Queue pinned quota backoff | Pinned quota backoff |
| Queue failed recovery | Daemon failed recovery |
| Queue dashboard | *(entry deleted with the alias)* |
| Queue dashboard two-line mode / status suffixes | Work dashboard two-line mode / status suffixes |
| `pop queue run` / `status` / `log` | `pop work daemon` / `status` / `log` |
| `[queue]` | `[work.daemon]` |
| `~/.local/share/pop/queue/` | `~/.local/share/pop/work/` |
| tmux `pop-queue` | tmux `pop-work` |

The head term is **Work supervision**, not **Work**: ticket 03 claimed bare
"Work" for the model (Work kind, Work container, Work item). A scheduler named
after the thing it schedules would collide with it on every sentence.

## Consequences

Muscle memory breaks at once and by design: `pop queue run` is an unknown
command, `pop queue dashboard` is gone, and a tuned `[queue]` config section
stops applying (surfaced as a finding, not silently). One user, one machine,
and an alias kept "for now" is an alias kept forever.

Rewriting the ADR corpus trades an immutable dated record for an internally
consistent one. The decisions survive verbatim; only their words move. A reader
of `git log` on `docs/adr/` will see a rename commit touching fifteen files at
once, which is the cost of not maintaining a translation table in perpetuity.

Deleting `queue/` finishes what tickets 03 and 06 start: after the split, no
package is named for a concept the glossary no longer has, and the dashboard,
the supervisor, and the two kinds' write-paths sit in packages named after what
they are. The split is mechanical but wide — it touches nearly every test file
in the largest package in the tree.

The both-paths lock check is the one piece of transitional code this cut adds.
It exists for a single release and has a deletion date; without it, an upgrade
mid-drain can run two supervisors against one store.
