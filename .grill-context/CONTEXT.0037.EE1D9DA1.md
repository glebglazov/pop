---
fragment: EE1D9DA1
generation: 0037
branch: master
---

+ Work lift
  The Work dashboard lifting the **Work container** rows its launching pane is
  attributed to out of the ordered list and rendering them above it, below only the
  **Selection area** when one is open, marked `▸` in the prefix column the cursor
  already shares. Named for what is lifted rather than for what caused it: the rows are
  Work, and a pane is tmux's word for the thing pop merely read the facts from. It is
  not a mark the human made — **Pinned action menu** and **Following** are the manual
  senses, and "pin" is left to them. Granted only by the active **Work view preset**'s
  `lift = true` — of the shipped roster, `active` alone — never by roster position;
  under any other preset the attributed rows keep their sorted position, unmarked.
  **Pane work attribution** itself is preset-independent and still computed either way.
  Applied after the sort resolves rather than as a sort term, so it can raise a Map row
  above the whole task-set block — which no comparator can do, rows never being ordered
  across kinds — while leaving every ordering rule beneath it untouched. Re-derived on
  every rebuild from pane facts read once at launch. `pop work status` builds with
  empty pane facts and never lifts.
  avoid: pane pin, pin, pinned row, pane-seeded cursor, sticky row, follow mode, cursor sync, reveal, jump-to-row
  under: Language

- Pane pin

~ Work view preset
  A named, self-contained answer to "which rows, in what order" on the Work read
  surfaces — selected one at a time from `[[work.dashboard.tasks.presets]]` (or the
  shipped roster when undeclared). Declares optional `label`, `status`, `unfolded`,
  `archived`, `muted`, `created_within`, `sort` (`created_desc` | `created_asc` |
  `status`, defaulting to `created_desc` when unset), `unanswered` (`admit` |
  `drop`, defaulting to `admit` — see **Unanswered filter field**), `lift` (boolean,
  defaulting to false — the sole grant of the **Work lift**; roster position grants
  nothing, so a user roster keeps the lift only by declaring it), and one `hide`
  clause. `pin` is a permanent silent alias for `lift`, so a config written before the
  rename still loads; it warns about nothing and is never the spelling pop writes. Of
  the shipped roster only `active` declares `lift = true`. The first resolved entry is
  the default; positions `1`–`9` are digit shortcuts in the **Work dashboard filter
  menu**. Session-only on the dashboard; `pop work status --preset <name>` names one by
  name.
  avoid: view filter preset, inclusion preset, dashboard filter preset
  was: The same term with the field spelled `pin` and no alias, granting the
    **Pane pin**.

~ Pane work attribution
  Which Work containers the pane a read surface was launched in belongs to — the
  question every other derivation asks backwards, from a known container to its pane. It
  is a ladder over what the pane can show, in three passes, strongest first. **Tags**:
  the pane's own `@pop_*` tag naming a Task set, a **Decision ticket** (which resolves to
  its Map) or a Routine, then the **Work session** stamp naming a Map — these mean "this
  pane *is* one pop opened for that work", so they are unambiguous and first-hit across
  kinds. **Neighbourhood**: where the pane is merely *standing* — the live **Drain**
  whose checkout contains the pane's directory, then the checkout itself plus the Task
  sets bound to it, ordered by **Bound-checkout lift order**; also first-hit across
  kinds, so a bound checkout always beats what follows. **Repository**: the last pass,
  reached only when nothing above answered — every Task set in the containing
  repository's **Task storage**, bound or not, plus that repository's Maps, keyed on the
  git common directory (**Repository identity**), which is what makes a sibling worktree
  answer with the trunk's work. Alone among the passes it **merges** rather than
  first-hits: its meaning is plural — the work of mine that lives here — where a tag
  means one thing, so every kind holding work in the repository contributes, in kind
  precedence order and, within a kind, in the active sort's order. Directories are
  matched canonically and by containment, deepest checkout first. Routines answer only
  the tag pass: being project-scoped and short-lived, a locality rung would answer with a
  project's whole routine list for any shell in the repo. The ladder is answered
  kind-side behind the **Work seam**, obtained by type assertion the way an advanceable
  kind is, and resolved while the snapshot is being built. The pane's facts (pane id,
  session, directory, every tag, the session stamp) are read once at launch in one
  `display-message` round-trip and carried, never re-read — and the containing
  repository's common directory is resolved once beside them, at launch, for the same
  reason: the pane's directory cannot change, so asking git per rebuild would fork every
  poll for an answer that was already settled. The ladder therefore compares strings and
  forks nothing. Its one consumer today is the
  **Work lift**, and it never announces itself: a pane attributed to nothing — or to a
  row the active view excludes — says nothing at all and widens nothing.
  avoid: pane ownership, current work detection, pane-to-set lookup, cursor memory
  was: The same ladder in two passes and first-hit throughout, whose weakest rung was the
    checkout the pane stands in plus the Task sets bound to it — so a pane in a sibling
    worktree of a bound checkout was attributed to nothing, and a repository's unbound
    sets and its Maps could not be reached by locality at all.

- Bound-checkout pin order

+ Bound-checkout lift order
  Which of several Task sets bound to one checkout leads the **Work lift** when a bare
  shell in that checkout attributes to all of them: the **Checkout claim** holder while
  something is live there, then the set drained most recently, then the topmost bound row
  under the active sort. Since every candidate lifts, this only decides which one leads —
  being wrong costs second place, not a wrong answer. Scoped to the neighbourhood pass
  alone: the **Repository** pass below it needs no such rule, ordering by kind precedence
  and then the active sort. "Checkout" is the generic here and deliberately not
  "worktree", which pop reserves for a *linked* checkout — the pass fires on a **Trunk
  worktree** at least as often.
  under: Language
