---
status: accepted
---

# pop owns the wayfinding lifecycle; `pop wayfinder` becomes `pop map`

## Context

A Map arrived as a folder of markdown and a skill that edited it. Ticket state
lived in hand-parsed `Status:` / `Type:` / `Blocked by:` header lines, claiming a
ticket was a header edit, and the only pop-side surface was four read verbs
(`show`, `status`, `archive`, `unarchive`) plus an `archive` side-file. Nothing
registered: a Map could not be pointed at, counted, or archived the way a Task
set is, because there was no row saying it exists.

Two consequences pushed this open. First, a kind can only be advanced
unattended if **pop**, not a skill, knows how to advance one item — so leaving
the lifecycle in a skill's prose blocks the Work supervisor before it starts.
Second, the wayfinder overlay had swapped in `grill-with-docs`, whose
commit-on-close rule was written for standalone grilling; carried into
wayfinding it made every grilling session write into the repository under study.
One session on the source Map did exactly that and had to be reverted by hand.

## Decision

- **pop owns the lifecycle; skills own only the HITL conversation.** pop owns
  registration, the frontier pick, claiming, resolution and arrival; a skill
  owns the interview and the prose it produces. Three depths were weighed — a
  thin store (pop owns state mutation only), an orchestrated lifecycle, and a
  full unattended drain of Decision tickets. Orchestrated lifecycle chosen: it
  is the precondition for a kind ever being AFK-advanceable, and a thin store
  leaves the lifecycle in prose, which is exactly the drift the Map manifest
  exists to kill. The unattended drain is the acknowledged successor, not this
  decision.
- **`pop wayfinder` hard-renames to `pop map`, with no alias.** Kind nouns
  everywhere; "wayfinder" stays the *skill's* name. The four read verbs carry
  over unchanged in behaviour, and the mutating verbs land in the same family.
- **Maps register, explicitly.** Charting ends with `pop map register <map-id>`,
  which validates the Map's `index.json` and writes its `work_containers` row.
  Registration reports every problem it found, not the first, and is
  re-runnable, so a malformed Map is a fix loop at charting rather than a
  surprise three sessions later. A lazy row-on-first-act was rejected as a
  hidden second registration path: with one entry point there is exactly one
  place a Map can become Work pop looks after.
- **Registration is plain, never managed.** No worktree is provisioned for a
  Map and `register` takes no `--managed` flag, because there is nothing for a
  checkout to hold.
- **Wayfinding writes nothing into the repository.** The repo is a
  read-and-experiment platform for the life of a Map: ADRs and glossary
  fragments are drafted, unnumbered, inside the Map's own folder, and the slice
  that implements a decision mints them into the repo in the same commit as the
  code. Prototypes live in the Map's `prototypes/` directory and escalate to a
  worktree only when they must compile against the codebase. This is a return
  to upstream wayfinder's shape (nothing lands on trunk until handoff); the
  commit-during-grilling coupling was pop-introduced.
- **Archival is the registry's `archived` bit.** The `wayfinder-archive.json`
  side-file folds into that bit on an ordinary read and is deleted, so a Map is
  hidden through the same mechanism a Task set is. The fold registers what it
  archives — the bit only exists on a row — and that is the one place other
  than `register` that writes one, precisely because refusing would silently
  restore an archived Map to the default views.

## Considered Options

- **Thin store: pop owns ticket-state mutation, the skill keeps the flow.**
  Rejected: the flow is where the drift lives. A manifest with a prose
  lifecycle around it is the same hand-edited Map with more files.
- **Full unattended drain of Decision tickets now.** Rejected as premature, not
  wrong: every Decision ticket is opened by a human today, and the seam that
  would carry an unattended advance does not exist yet.
- **Keep `pop wayfinder` as an alias.** Rejected: the same discipline as the
  `pop queue` cut. An alias means two names for one family in help output,
  completion and every doc, forever, to spare a rename nobody has muscle memory
  for yet.
- **Register a Map lazily on its first mutating verb.** Rejected: a second,
  invisible registration path. It also removes the moment where the manifest is
  validated, which is the only reason registration is worth a verb.
- **Let `archive` register the Map it archives.** Rejected for the same reason,
  with one exception: the legacy fold, which is migrating a decision the human
  already made rather than making one.
- **Keep archival in the side-file.** Rejected: every kind's archived bit would
  then live somewhere different, and the dashboard would need a per-kind reader
  for a bit that means exactly one thing.
- **Keep `grill-with-docs` for wayfinding tickets and fix the ordering
  problems it creates** (fold-before-spawn, sets forking the Map's branch,
  incremental folds). Rejected: all three are descendants of the repo write.
  Removing the write removes all three.

## Consequences

- `pop map register` is the boundary between charting and worked Map. Every
  consumer downstream of it reads the Map through its manifest, so registration
  is where a Map that cannot be read that way is caught.
- A Map charted before this decision registers only after its fold has minted a
  manifest; a Map the fold declined must be fixed by hand, which is the same
  fix list `register` prints.
- `archive` and `unarchive` refuse an unregistered Map, naming `pop map
  register`. That is a behaviour change from the side-file, which would archive
  anything with a folder.
- The wayfinder overlay shrinks a lot and stops being a set of deltas over
  upstream, which makes upstream drift-diffing harder. Accepted cost.
- Decisions live outside the repository for the life of a Map, so a repo-only
  reader cannot see them until handoff. Consistent with the Work store already
  being per-machine.
