---
status: accepted
---

# A pane is attributed to Work kind-side, and seeds the dashboard cursor once

Opening the Work dashboard from a tmux pane that relates to a Work container lands the
cursor on that container's row. The relation — **Pane work attribution** — is a first-hit
ladder answered kind-side behind the Work seam, not a switch in `cmd`; the seeding is one
shot at first render and never chases the human's own navigation afterwards.

## Context

Pop knows a great deal about which pane belongs to which work, and never asks the
question. The parts are all there and none of them are composed: Task-set panes carry
`@pop_set` / `@pop_verify` / `@pop_fold` / `@pop_assist` tags naming a set; a Map's
session carries `@pop_work_kind` / `@pop_work_id`; `LiveDrainByRuntimePath` names the set
draining at a path. No command anywhere asks "what work does this pane belong to?" — every
existing derivation runs the other way, from a known container to its pane.

The consequence is a small, constant tax. The human is sitting in the worktree of the set
they are working on, opens the dashboard, and hunts for the row they were already standing
in. The sort is not stable across rebuilds, so the row is not where it was last time.

The seeding half is nearly free and already has a precedent in the same binary: the
monitor dashboard opens on a chosen row via `WithInitialPaneID`, the generic list widget
already re-anchors selection by key across refreshes, and `defaultDrainCursor` already
seeds a picker from the checkout the dashboard is running in (ADR-0192). The cost is
entirely in the attribution ladder, and specifically in its weakest rung.

## Decision

**1. Attribution is a first-hit ladder over what the pane can show.** Strongest to
weakest: the pane's own tag naming a Task set or a Decision ticket; the Work session stamp
naming a Map; the live Drain at the pane's directory; and finally the checkout the pane
sits in, plus the Task sets bound to it. The top three rungs mean "this pane *is* a pane
pop opened for that work" and are unambiguous. The fourth means only "this pane happens to
sit somewhere work is bound", and it is in deliberately, because it is the rung that fires
for the ordinary editor shell the human opened themselves — which is where they are when
they want this.

**2. The weakest rung breaks ties and says so.** Several sets can be bound to one
checkout, and pop has no per-set recency to rank them: bindings carry no timestamp,
`work.Container` carries no recency field, and `history_entries` is keyed by path, so N
sets on one checkout all write the same history row. The sub-ladder is therefore the
Checkout claim (decisive, but only while something is live there), then the set drained
most recently, then the topmost bound row under the current sort. Whenever there was more
than one candidate, the choice is named in the status line. Placing a cursor is not an
action, so a plausible near-miss costs one keypress, where refusing to place it costs the
feature in the case it fires most often.

**3. The ladder is answered kind-side.** Every rung is kind-specific knowledge — a Map's
session name shape, a Task set's four pane tags, a checkout's bindings — so it is one more
question behind the Work seam, obtained by type assertion the way `Advancer` is. Resolving
it during snapshot build is also where the kind already holds the bindings, live panes and
claim it needs. The alternative, resolving a cursor key in `cmd` before the dashboard
opens, would put a switch over kinds in exactly the layer the seam exists to keep free of
them.

**4. Seeding is one shot at first render.** No pending target that survives, no re-attempt
after a preset change, no retry when a later rebuild reveals a row that was filtered
before. A target that outlives the human's own navigation is a cursor that fights them:
widen a preset twenty minutes later, having deliberately moved somewhere, and the cursor
teleports back to where the session started.

**5. It opens on page A only.** A pane attributed to a Routine resolves to a page-B row and
is simply not seeded, rather than the launch following the answer onto the other page.

**6. Silent when nothing is attributed; loud when the row is not renderable.** An
unrelated shell is the common case, and a "nothing found" line on every launch trains the
human to ignore the status line. But when a container *was* attributed and its row is
hidden — by the active Work view preset or a live filter query — the reason is reported.
A cursor resting at row one with no explanation is indistinguishable from a broken
feature. Note what this does not do: it never widens the preset. The preset is a
deliberate choice, and a launch does not get to overrule it.

**7. No opt-out.** No flag, no config key. The behaviour moves a cursor and nothing else,
so a config key would have to answer ADR-0198's reach question for a preference nobody can
articulate a reason to hold. `gg` is the opt-out.

## Considered Options

**Live follow: a dashboard already open re-points its cursor as the human focuses related
panes elsewhere.** Rejected. It needs a poll of tmux's active pane on every tick, and it
has to decide, every tick, whether to overrule a cursor the human moved deliberately —
which is the same objection as decision 4, paid continuously instead of once.

**Restrict attribution to Task sets.** The original ask. Rejected: the seam has one row
type and one cursor identity, so the work is the same, and stopping short would mean
building the seam and then declining to wire two of its three implementors.

**Refuse to seed when the bound checkout is ambiguous.** Honest, and it never guesses
wrong. Rejected in decision 2 — it disables the feature in its most common case in
exchange for avoiding a one-keypress correction.

## Consequences

- Rungs one to three are almost free: the dashboard already builds a tag-to-set map from a
  single `list-panes -a` call on every poll, and that call already returns the pane id. Two
  tags (`@pop_ticket`, `@pop_routine`) need adding to its format string.
- Pane facts are read once at launch in one `display-message -p` round-trip — pane id,
  session name, directory, tag values — and carried, not re-read.
- Muting interacts with this correctly and for free: a pane attributed to a muted set finds
  its row hidden under the default preset, so decision 6 reports why (see
  [ADR-0200](0200-mute-is-a-timed-human-set-not-now-on-a-work-container.md)).
- Attribution is a general capability once it exists, and this ADR spends it on one cursor.
  Any later "what am I working on?" surface should use the same ladder rather than
  re-deriving one.
