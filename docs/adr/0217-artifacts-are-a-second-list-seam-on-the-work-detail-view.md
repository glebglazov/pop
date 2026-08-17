# Artifacts are a second list seam on the Work detail view

A **Work kind** may publish **Artifact**s about a container — documents a human
reads rather than acts on — through a seam of its own, `Artifacts` plus
`ArtifactActions`, and the **Task set detail view** shows them as an **Artifact
view** it toggles into with `v`. Artifacts are not **Work item**s. The **Document
peek** already reachable with `l` opens on an artifact's path unchanged, and
copy-name (`y`) and copy-path (`p`) are the verbs on a row.

ADR-0214 shipped **Code review** and routed the **Review artifact** to a human as
a pointer on four surfaces. Three of them work: the HITL sign-off gate prints a
summary line and offers a numbered entry that pages the document through
`$PAGER`, and the Assist prompt names the path with an explicit instruction to
open it. The fourth does not. On the dashboard the pointer is a **Detail
sections** block — prose, by the definition of that term, which is exactly why it
has no verb — so the operator sees an absolute path they cannot open, peek, or
copy. The document was reachable from every surface except the one people live
in.

## Considered options

**Artifacts as `work.Item`s.** The cheapest change by far: `Work item` already
carries an absolute `File`, and the peek already reads any item's file, so a
review would become peekable with no new seam at all. Rejected on the dispatch
key. `Item.Status` is not decoration — it is the machine token a kind keys
`ItemActions` off, which is how a task completed in another pane stops being
offered "complete". An artifact has no status, so it would carry an empty token
that every `ItemActions` implementation has to special-case, plus permanently
blank `Status`, `StatusLabel`, `Blocked` and `BlockedBy` columns in a renderer
that hard-codes them. The two lists disagree on their columns, their verbs, their
sort key *and* their dispatch key; that is four disagreements, which is a second
list, not a widened first one. It would also make `Work item` mean something
other than "one advanceable thing inside a container", the term the **Work
supervisor** depends on.

**A generic N-facet seam** — `work.Facet{Title, Columns, Rows, Actions}` that `v`
rotates through, of which tasks and artifacts are two instances. Rejected as
vocabulary for a third list nobody has asked for. Two named lists cost less to
read and leave the generic shape reachable the day a third arrives; inventing it
now means every kind implements a registry to publish two things.

**A global artifacts page** — a page C beside Task sets and Routines, listing
every artifact on the machine newest first. Rejected because it has no cursored
container to scope to, and the sort the operator actually wants ("newest review
of *this* set") is per set. A machine-wide artifact feed is a coherent feature; it
is a different one.

**An `$EDITOR` handoff on an artifact row** (`O`), originally part of this design.
Rejected for now on ADR-0158: an uppercase row verb hands off and *quits the
dashboard*, and exempting one verb from that is precisely the per-verb
inconsistency ADR-0158 exists to remove. The `Document peek` already reads the
document without leaving, and `p` puts the path where an editor or an agent can
take it. Left open deliberately — if reading in `$EDITOR` proves necessary, it
arrives as a handoff that quits, or as a Routine-style tmux window
(`routine/refine_spawn.go`), not as an in-place `tea.ExecProcess`.

**Humanised row labels** (`Code review`, `Spec`) instead of filenames. Rejected
because every prior review is its own row: three rows reading `Code review`,
distinguished only by a timestamp column, are worse than three filenames that
already carry their instant — and the filename is what `y` and `p` hand you.

## Consequences

- **The seam is two methods and may return nothing.** A kind that publishes no
  artifacts needs no code: `v` is offered, and hinted, only when the focused
  container yields at least one row. That is one rule evaluated per container,
  not per kind — so a Map (which publishes none today) hides the toggle for free,
  and a freshly planned Task set with no spec, no reviews and no progress does
  not offer a toggle into a blank table.
- **The list is a closed known list, not a directory dump.** For a Task set:
  every `reviews/*.md`, `spec.md`, and `progress.txt`. The manifest is excluded
  because the detail view *is* the manifest rendered; task markdown because it is
  the other list; captured runs because they are gzipped JSONL that neither peeks
  nor opens, and `pop tasks stream` already serves them with its own pager. A
  stray file nobody recognises is a validation concern and belongs with the
  existing orphan-markdown rule, not in a display surface.
- **One total order, newest first, over a family that does not timestamp itself
  uniformly.** A review takes its instant from the instant in its own filename —
  which is why that instant is in the filename — and everything else from its
  modification time, read off the directory listing the load already performs, so
  no extra `Stat` enters a fanned-out read path. Modification time is truthful for
  both remaining members: `progress.txt` moves on every transition and `spec.md`
  on every re-plan. Sorting undated rows to the bottom instead (the rule **Work
  container creation date** uses) was rejected: it would permanently bury the spec
  under every review for no reason a reader could infer.
- **`progress.txt` is an artifact.** It is the only place a verification verdict
  is recorded as prose — there is no verification report file; a verdict is a
  store episode, a `progress.txt` block, and on FIXABLE a remediation task — so
  omitting it would leave the narrative of a set that went sideways unreachable
  from the surface built to reach documents.
- **This narrows a consequence of ADR-0214 without overruling it.** The detail
  section stops displaying the review's path and becomes a count, the newest
  artifact's type, and its instant — enough to know a review exists and that `v`
  is worth pressing. The path itself is one keystroke away with a verb on it,
  which is a strict improvement on prose you cannot act on. ADR-0214's
  load-bearing surfaces are untouched: the HITL gate preamble and paging entry,
  and the Assist prompt's named path, all still carry the pointer in full.
- **The CLI detail view changes with it.** `pop tasks status` prints the same
  block under the same title, because `ReviewSectionTitle` exists precisely so the
  two surfaces "cannot drift into naming the same thing differently" — letting
  them drift now would waste the guard already in place.
- **`pop tasks artifacts <set>` lists the same rows, and `--show <name>` prints
  one verbatim**, the CLI twin of the peek. `pop tasks review --show` stays as the
  shorthand for the latest review; this is a superset, not a replacement.
- **`v` means "switch view" at two levels and needs no arbitration.** The shell
  withholds its **Work page** toggle whenever a detail view is open, so the letter
  is genuinely unbound there. It stays lowercase because ADR-0158's case rule
  governs row verbs only, and navigation keys are named exempt.
- **Copy-path (`p`) lands on task rows too, not just artifacts.** A task item had
  no path verb at all — the container's `p` copies the checkout — and the reason
  an artifact row needs one applies unchanged to the task markdown beside it.
  Both are offered from the row menu and from inside the peek.
