---
status: accepted
relates: "amends decision 5 of [ADR-0266](0266-attended-agent-choices-end-with-the-session-and-never-fall-back.md) and builds its **Attended session choice** first; retires the `alt+a` trigger and the narrow override writer of [ADR-0264](0264-an-attended-launch-surface-writes-the-agent-override-it-renders.md) decisions 3 and 5, keeping that ADR's affordance-only-where-a-choice-exists rule; restores the one-editor rule of [ADR-0202](0202-config-overrides-are-a-top-ranked-layer-edited-by-one-component.md) whole; leaves the row-list marking rule of [ADR-0254](0254-a-selection-is-a-human-mark-that-outranks-a-preset-and-turns-verbs-plural.md) decision 4 untouched"
---

# The Work dashboard chooses its attended agent for the run

## Context

ADR-0264 closed a real gap: a human sitting on a row that names the **Agent
entry** about to run could see what would launch but not change it. It closed
that gap on two surfaces — a gate menu's assist row on `tab`, and a dashboard's
attended action row on `alt+a` — and both wrote the persisted **Agent override**.

ADR-0266 then found the dashboard half broken for the reason the write was
persisted. A dashboard-launched Assist session carries an `--agent` flag; a
saved override could not replace that flag, so the pick did not control the
conversation it was made for, and it *did* change the next run and other panes.
Its decision 5 therefore removed `alt+a` and added a flat prohibition: do not
add a replacement dashboard chooser.

That prohibition is wider than its own reason. What failed was the *lifetime*,
not the surface. ADR-0266 decision 1 supplies the lifetime that works — an
**Attended session choice**, lasting one interactive command invocation — and a
Work dashboard is one interactive command invocation. The dashboard is also
already plumbed for it: ADR-0264 decision 8 threads an attended spec from every
launch site to the resolved invocation, so the dashboard's own launches take a
spec rather than re-resolving config for themselves.

Nothing implements ADR-0266 yet. `alt+a` is still live, and the concept it needs
does not exist in code.

## Decision

1. **A dashboard chooser returns, session-lived only.** This amends ADR-0266
   decision 5: its removal of `alt+a` and of the persistence behaviour stands,
   and only its prohibition on any replacement is lifted. A chooser whose pick
   cannot outlive the dashboard does not have the failure decision 5 was written
   against.

2. **`tab` in the Run menu is the trigger.** The chooser opens from the Run
   menu (`r`) — the menu whose attended action rows already render the entry
   that will run — and its hint on those rows reads `· tab to change`, the same
   words a gate's assist row uses. ADR-0264 decision 3 chose a chord because
   "`tab` on a dashboard marks the cursored row and no mode may gate it"; that
   rule is the row list's, and `tab` inside an open Run menu is unclaimed key
   space today. Marking and clearing on the row list are untouched, so ADR-0254
   decision 4 is unaffected.

3. **`alt+a` retires whole** — the chord, the row hint that named it, and its
   dedicated entry in the dashboard's key-space reservation. One act gets one
   trigger; the reservation existed only to stop a Work kind shadowing the
   chord.

4. **The choice is the dashboard's, and dies with it.** A pick is an **Attended
   session choice** held by the dashboard: no config write, no store row, no
   second lifetime. It rides out as the attended spec on every attended launch
   the dashboard makes — assist, work, fan-out, map assist — so the row's render
   stays binding and the launched session runs the picked entry. Quitting the
   dashboard ends it; the next dashboard starts from configured resolution.

5. **It is offered wherever the Run menu names an entry, singular or plural.**
   A **Selection**'s plural Run menu renders attended rows too, and the choice
   belongs to the dashboard rather than to a row, so `tab` opens the chooser
   there on the same terms. ADR-0264 decision 4 is kept unchanged: the hint and
   the key are offered only while two or more usable entries are configured.

6. **The Config dashboard is the only writer of the override layer again.**
   ADR-0264 decision 5 admitted a narrow interactive writer to it; with the pick
   no longer persisted there is nothing to admit, so ADR-0202's one-editor rule
   stands whole rather than bent, and every saved **Agent override** is still
   seen, previewed and removed in one place.

7. **The dashboard is where the Attended session choice is built first.** It is
   the cheapest host of the concept — a field on the page plus the spec it
   already passes — so the concept lands where it is smallest to get right, and
   ADR-0266's gate, Assist and phase-picker surfaces inherit it rather than
   define it.

## Alternatives rejected

**Hold decision 5 and change the agent from the gate inside the session.** The
dashboard would launch on the wrong entry and the human would change it there,
paying an agent start per pick. The choice is live on the dashboard row; making
it reachable only after a launch is the gap ADR-0264 opened, reintroduced.

**Persist the dashboard pick, as `alt+a` did.** This is exactly what ADR-0266
diagnosed: the wrong lifetime for a choice made inside an interactive run, a
write the launch flag then beats, and a change to panes the human was not
thinking about.

**A new global chord instead of `tab` in the menu.** A bare modifier chord
teaches nobody it exists — ADR-0212 decision 6 retired `ctrl+w` on that ground
and ADR-0264's own `alt+a` proved it twice. The Run menu is where the row that
names the entry lives, so the key belongs next to the render that advertises it.
