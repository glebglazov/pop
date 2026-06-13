# Archive is a reversible Task-state flag

Status: accepted (extends ADR 0012's per-repository Task state and ADR 0020's whole-set Multi-task selection)

Done Task sets never leave the **Status table** — they pile up at the top of `pop tasks status` and stay in `pop tasks timings` completion forever, even after the work is long finished. There was no way to say "I'm done with this set, get it out of my way" short of deleting its directory. Users wanted a declutter affordance: review the finished sets, file them away, and have them stop surfacing in status and completion — most of the time, every Done set at once.

## Decision

Add `pop tasks archive` / `pop tasks unarchive`, operating on whole **Task sets**, backed by an `archived` flag on the set's `RegisteredTaskSet` entry in **Task state**.

- **A flag, not a file move.** Archiving flips a boolean in `state.json`; the set's directory, **Task manifest**, task statuses, **Progress record**, and **Captured attempt stream**s are untouched. Archiving is therefore non-destructive and fully reversible, and an archived set's artifacts remain exportable and inspectable in place. It is a registration-metadata decision like **Task set priority** — not a derived **Task set status**, not a task-status transition — so it appends no **Progress record**, mirroring `set-priority`'s state-only shape.
- **Action verbs reject; snapshot verbs resolve.** `implement`, `open`, `complete`, `skip`, and `set-priority` refuse an archived target and point the human to `unarchive` first — archived means "deliberately out of rotation," so acting on it is a two-step intent. The read-only snapshot verbs `export`, `show-path`, and `timings` still resolve an explicitly typed archived identifier, because they neither schedule nor mutate the set; archive is a scheduling/visibility concern, and these verbs sit outside it. Automatic selection and draining skip archived sets entirely.
- **Hidden, but discoverable.** Archived sets drop out of the default status table, automatic selection, and every **Task shell completion** surface except `unarchive`. When any archived set exists, default status prints a quiet footer with the count and the `pop tasks status --archived` command; `--archived` renders only the archived sets. `unarchive` completion offers only archived identifiers.
- **A cross-set Multi-set selection.** `archive` with no argument opens an interactive checkbox UI listing every non-archived registered set (all statuses, including Missing and Malformed), with only **Done** sets pre-checked — the cross-set sibling of the within-set **Multi-task selection** from ADR 0020. `unarchive` opens the same UI listing only archived sets, none pre-checked. A bare **Task set identifier** archives/unarchives exactly that set with no picker, regardless of status. `--yes` archives precisely the Done sets non-interactively. A no-argument run with no interactive TTY and no `--yes` is rejected rather than mass-mutating silently, exactly as ADR 0020 rejects a non-interactive whole-set Multi-task selection.

## Considered options

- **Move the set directory off the discovery scan path** (e.g. `tasks/<id>/` → `archive/<id>/`) — rejected. It keeps refresh leaner as archives accumulate, but turns a metadata flip into a filesystem mutation and forces `unarchive`, `export`, `show-path`, and `timings` to special-case a second location. A flag keeps the set in one canonical place and reversibility a one-field write; the per-refresh cost of loading filed-away manifests is negligible at realistic scale.
- **Auto-unarchive on action** — rejected. Letting `implement <archived-set>` silently un-hide and run is fewer keystrokes, but it makes the "filed away" guarantee soft and invites "why did my archived set just run" surprises. An explicit two-step (`unarchive`, then act) keeps the state meaningful.
- **Uniformly reject archived targets for every verb, including export/show-path/timings** — rejected. Simplest mental model (archived = totally inert), but it strands a legitimate need: snapshotting or inspecting a finished, filed-away set without restoring it to the active schedule. **Task set export** is already defined as status-agnostic; forcing an unarchive round-trip just to export contradicts that.
- **A new verb instead of `archive`** (`shelve`/`hide`/`dismiss`) to avoid the word colliding with the **Task set export** tar.gz "archive" — rejected. `archive` is the natural, requested word, and there is no *command* collision (`export` writes the tarball). The glossary instead reserves bare "archive" for the export file and always names the new state in full ("Archived Task set").
- **Pre-check Done + Deferred sets** in the picker — rejected. A **Deferred Task set** has skipped work the glossary frames as "conclude or reopen later"; pre-checking it risks filing away unfinished intent. Only fully-Done sets are pre-checked; Deferred and every other status remain listed and manually checkable.

## Consequences

- **Task state** gains an `archived` field per registered set; the state file format stays at its current version with the field defaulting to `false`, so existing state reads forward without migration.
- A new cross-set **Multi-set selection** bubbletea component is needed; the **Multi-task selection** from ADR 0020 returns task rows within one set and does not cover whole-set rows.
- Every completion surface and the scheduler grow an archived filter; `unarchive` adds an inverse completion that offers only archived identifiers.
- Resolution and completion diverge for archived ids on snapshot verbs: completion narrows (never offers archived), resolution still succeeds — the same split ADR 0012-era completion already applies to done targets.
- The **Status table** grows an `--archived` mode and a conditional archived-count footer.
