---
status: accepted
relates: "supersedes [ADR-0186](0186-status-submenu-is-kind-owned-and-a-maps-status-is-operator-writable.md)'s placement of the status submenu inside the action menu, retires the `a` opener [ADR-0224](0224-the-selection-area-sits-at-the-foot-and-menus-are-bottom-chrome.md) decision 4 anchored, and keeps [ADR-0200](0200-mute-is-a-timed-human-set-not-now-on-a-work-container.md) decision 4's mute submenu while moving where it opens"
---

# Every menu opens from the top level and the action menu becomes the Run menu

## Context

The **Work dashboard**'s `a` action menu had grown two menus inside it. `s` opened the
**Status submenu** (ADR-0186), `m` opened the mute submenu (ADR-0200 decision 4), and both
sat as list items among ordinary verbs. Reaching a Task set's `archive` was `a` `s` `x`;
reaching a mute window was `a` `m` and then a date.

Three things went wrong at once, and none of them is visible from inside a single ADR:

- **The menu had no subject.** "Actions" was a name for the set of everything, so status
  writes, clipboard copies, mute windows and drain-the-set all shared one list and one
  key. A list with no subject cannot be ordered, and this one was ordered by accretion.
- **`x` archive was in it twice.** `Actions` carried `VerbArchive` on `x`
  (`tasks/setkind/verbs.go:102`) *and* `StatusActions` carried the identical verb on the
  identical key. Two entries, one write.
- **`u` was contested.** Unmute and unbind-worktree both claimed `u`
  (`verbs.go:87`, `:92`), and the comment there records the resolution: on a row that is
  both muted and bound, `u` clears the mute and unbind is "one Enter away". A key that
  means different things depending on row state is a key the fingers cannot learn.

Copy had the mirror-image problem. `y` copy-name was flat at top level *and* an item in
the menu; `p` copy-path was menu-only and appeared only on a bound row. There was no way
to copy a set's definition folder at all.

## Decision

**Every menu on the Work dashboard opens from the top level, and no menu contains
another.** The action menu is split by subject into four, and what remains of it is
renamed for the residue.

1. **Four **Top-level menu** openers.** `r` **Run menu**, `s` **Status menu**, `y` **Copy
   menu**, `m` **Mute menu**. Each is a **Frame** reserved block at the foot, exactly as
   ADR-0224 decision 4 established — the mechanism does not change, only how many menus
   use it and where they open from.

2. **The Run menu is what is left, and the name is the residue.** Drain, verify, fold,
   assist, shell, bind/unbind worktree, auto-drain, unpark for a Task set; the frontier
   and assist verbs for a Map; fire, preview, edit, refine, pause, runs for a Routine.
   Everything in it starts something or governs what starts. "Action menu" is retired as a
   term: it named the set of everything, and the set of everything is what we just split.

3. **`a` is unbound and silent.** A flash saying "the action menu is now `r`" was
   considered and rejected: a signpost is a second thing to carry and remove, and the
   footer already names every opener. The cost is real and accepted — `a` is in six ADRs
   and in the fingers of everyone who uses this dashboard.

4. **Archive lives only in the Status menu.** `VerbArchive` leaves `Actions` on both kinds
   that carried it. The verb is no further away for it: `a` `s` `x` becomes `s` `x`, which
   is shorter, and the duplicate that could drift is gone.

5. **Unmute moves into the Mute menu, which ends the `u` contest.** On an already-muted
   row the Mute menu offers `u` clear-mute beside its windows. In the Run menu, `u` means
   unbind worktree unconditionally. Mute and unmute being one concept in one place is the
   point; freeing `u` is the dividend.

6. **A new `Kind.CopyActions` seam, mirroring `StatusActions`.** The dashboard owns the
   menu render and dispatch falls through `Kind.Perform` by verb id, exactly as the status
   opener works. A Task set returns name (`n`), set definition path (`y`) and, when bound,
   worktree path (`w`); a Map returns name and folder; a Routine returns name and last
   report path. **The set definition path is a new capability** — nothing copied it
   before. `y` `y` is keyed for the fingers, not the mnemonic: it is the one people reach
   for. `Actions` loses both `y` and `p` on every kind.

7. **A menu its target does not offer is a flash, not a dead key.** A **Routine** has no
   status (`routine/verbs.go:103` returns nil) and cannot be muted (`ref.Kind.Mutable`),
   so `s` and `m` on page B say so. Under the old shape the absence explained itself — the
   verb simply was not in the list. At top level it cannot, and a key that appears broken
   is worse than one that answers. This follows the Map's `I` precedent
   (`dashboard.go:1305`), where a real report beat a dead key.

8. **The footer surfaces actions and nothing else.** `mainHint` drops `j/k move`,
   `gg/G top/bottom`, `tab select` and `l/enter detail` — movement and marking do not earn
   the line — and leads with the four openers in the order they are reached for:
   `r run ▸ · s status ▸ · y copy ▸ · m mute ▸ · / search · f filters · v routines ·
   C-h help · h/esc quit`. With rows empty the openers are suppressed; there is nothing to
   act on.

9. **The detail view follows in key and word only.** Its opener becomes `r` and its label
   "run"; `ItemActions` keeps its contents exactly as they are, and its flat `y`/`p` copy
   keys stay flat — one item needs no menu. The detail view was already one level deep,
   so nothing there is being fixed; this is only preventing `a`/"actions" from surviving
   in one corner as a word that means nothing anywhere else.

10. **The Selection path is unchanged in kind.** Each opener behaves at top level exactly
    as it did inside `a`: singular or plural by whether a Selection is active, with the
    plural menu showing the intersection of what every marked row offers and declares
    plural on `work.Action.Modes` (ADR-0215). `CopyActions` items declare `Modes` like
    every other action — copy-name is plural, the paths are not.

## Considered alternatives

**Keep one `a` menu and allow nesting.** This is what exists. It works, and the cost is
that the depth of a verb is a fact about the menu's history rather than about the verb.
Two nested menus is where it stopped only because nobody added a third.

**Rename the menu but keep `a`.** Cheaper for the fingers and dishonest in the documents:
a "Run menu" on `a` is exactly the kind of stale mnemonic that sends the next reader
grepping for a meaning that is not there.

**Move status and copy out but leave mute nested.** Rejected in the first round. A rule
that holds for one submenu and not the other is not a rule, and "the Run menu holds only
workflow verbs" would be false while a date picker lived in it.

## Consequences

- `work.Kind` gains `CopyActions`, so `work/conformance_test.go` pins a fourth list per
  kind and every kind must answer.
- The glossary term **Action menu** is retired in favour of **Run menu**, and **Status
  submenu** becomes **Status menu** — it is no longer sub-anything. Six ADRs continue to
  say "action menu"; they are history and are not rewritten.
- `a` becoming inert is the one change here that will be felt as a regression before it is
  felt as an improvement.
