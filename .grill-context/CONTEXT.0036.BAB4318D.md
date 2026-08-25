---
fragment: BAB4318D
generation: 0036
branch: master
---

+ Top-level menu
  A **Work dashboard** menu opened directly from the row list rather than from inside
  another menu. Every menu on the surface is one: the dashboard has four openers — `r`
  **Run menu**, `s` **Status menu**, `y` **Copy menu**, `m` **Mute menu** — and no menu
  contains another. A menu whose target offers it nothing is not a dead key: it flashes
  what the kind cannot do, because a top-level key that appears broken is worse than a
  verb that was simply absent from a list.
  avoid: submenu, nested menu, second-level menu
  under: Dashboards

+ Run menu
  The **Top-level menu** on `r` listing the ways to set a row's work running — a Task
  set's drain, verify, fold, assist, shell, bind/unbind worktree, auto-drain and unpark; a
  Map's frontier and assist verbs; a Routine's fire, preview, edit, refine, pause and runs.
  It is what remains of the old action menu once status, copy and mute became menus of
  their own, and the name is that residue: everything in it starts something or governs
  what starts. It renders as bottom chrome through **Frame**'s reserved block, its top rule
  naming what it acts on (`run · orders-api`, `run · 6 selected`). `a`, the key it was
  opened with for six ADRs' worth of releases, is left unbound and silent.
  avoid: action menu, actions, verb menu, command palette
  under: Dashboards

- Action menu

+ Status menu
  The **Top-level menu** on `s` holding a row's status writes, supplied by its kind
  through `Kind.StatusActions`: a Task set's complete, open (reopen), skip, archive and
  unarchive; a Map's reopen, abandon, archive and unarchive. It is the only place archive
  lives — the duplicate `x` the old action menu carried beside it is gone, and the verb is
  no further away for it. A **Routine** has no status to write, so `s` on one says so.
  avoid: status submenu, state menu, verbs menu
  under: Dashboards

- Status submenu

+ Copy menu
  The **Top-level menu** on `y` holding what a row can put on the clipboard, supplied by
  its kind through `Kind.CopyActions`: a Task set's name (`n`), set definition path
  (`y`) and, when bound, worktree path (`w`); a Map's name and folder; a Routine's name
  and last report path. The set path is a new capability — nothing copied a set's
  definition folder before it. `y` `y` is the common one and is keyed for the fingers
  rather than for the mnemonic.
  avoid: yank menu, clipboard menu
  under: Dashboards

+ Mute menu
  The **Top-level menu** on `m` holding the mute windows a row can take, and — on an
  already-muted row — the `u` that clears one. Pulling unmute in here ends the contest
  for `u` in the old action menu, where unmute and unbind worktree both claimed it and
  only the first won on a row that was both muted and bound: in the **Run menu**, `u` now
  means unbind and nothing else.
  avoid: mute submenu, snooze menu
  under: Dashboards

~ Work dashboard search
  The query opened with `/` on either **Work page**: a committed, case-insensitive
  **substring** match that narrows the rows the active **Work view preset** already
  selected. It reads four fields — a row's ID, its **Project**, its kind's **Type word**s
  and its worktree (both the destination label and the checkout path) — OR-ed together,
  and splits on spaces into terms that are AND-ed, so each term must match somewhere but
  different terms may match different fields (`way pop` is Maps in project pop). A row's
  status text is deliberately not among them: status is **Work view preset** vocabulary,
  and asking it twice in two grammars invites the two answers to disagree. Stateful —
  Enter applies it and returns the full keymap, so navigation and every action key work on
  the narrowed rows; the term persists across polls, preset changes and page switches, and
  is shown in the page header beside the preset. Reopening `/` starts from an empty
  buffer, so applying an empty query clears the search; Esc abandons the edit and restores
  the term that was in force. Per-page and session-only.
  was: The name query opened with `/` on either **Work page**: a committed,
  case-insensitive substring match over a row's project or ID that narrows the rows the
  active **Work view preset** already selected. Stateful — Enter applies it and returns
  the full keymap, so navigation and every action key work on the narrowed rows; the term
  persists across polls, preset changes and page switches, and is shown in the page header
  beside the preset. Reopening `/` starts from an empty buffer, so applying an empty query
  clears the search; Esc abandons the edit and restores the term that was in force.
  Per-page and session-only.

+ Type word
  One of the words a **Work kind** answers to in a **Work dashboard search**, supplied by
  the kind itself through `Kind.TypeWords`: a **Task set** answers to `task-set`, `set`
  and `tasks`; a **Wayfinder map** to `map`, `wayfinder` and `wayfinding`; a **Routine**
  to `routine`. A Map's `wayfinding` is there because that is the word on screen — it
  reaches the table as the Map's STATUS cell label, and a human types what they read
  rather than what the enum calls it. A Task set has no type word anywhere on screen, and
  that is accepted: it is the default kind of page A, the one nobody needs to search for.
  avoid: kind word, type filter, kind alias
  under: Dashboards
