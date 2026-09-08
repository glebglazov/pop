---
status: accepted
relates: "completes decision 5 of [ADR-0202](0202-config-overrides-are-a-top-ranked-layer-edited-by-one-component.md) without superseding it, and consumes the `alt+a` its decision 10 returned to the pool; extends the **Attended entry render** of [ADR-0196](0196-one-agent-override-picker-and-attended-gates-become-inline-tui.md) decision 9; leaves the precedence ladder of ADR-0202 decision 9 unchanged; shares a dashboard with [ADR-0263](0263-assist-panes-are-addressed-by-slot-and-a-container-may-hold-nine.md)"
---

# An attended launch surface writes the agent override it renders

> **Amended by [attended session choices](0266-attended-agent-choices-end-with-the-session-and-never-fall-back.md):**
> Tab choices now last only for the current interactive run and take precedence
> over its attended agent flag. Dashboard alt+a is removed. Attended launches
> select one entry and never fall back. The earlier persistence rules below
> record the previous behaviour.


## Context

ADR-0202 deleted a session-lived attended-agent picker and diagnosed the failure
precisely: *"the observed failure is not 'the override went stale' — it is that
the override is never reached at all."* It then built the reachable thing — a
persisted **Agent override** in `config.override.toml`, edited in the **Config
dashboard** — and kept ADR-0196's four renders so a human can always see which
**Agent entry** is about to run.

Reached from where the choice matters, that is still one surface short. A human
sitting on a gate's `1. Get agent assistance (default) · Claude Opus · opus` row
is at the exact moment the choice is live, and the row tells them what will run
but not that anything can change it. Changing it means leaving the gate for a
different program, editing a TOML list in `$EDITOR`, and coming back. In
practice the attended list's tail is never used: every attended surface takes
the head entry (`tasks.EffectiveAttendedEntry`), and the tail exists only as the
**Attended launch-time skip** walk for a cooling or missing binary.

The per-command escape hatch already exists and is already plumbed end to end —
`ResolveAgentAssistanceInvocation` takes an override spec that jumps to the head
of the candidate walk, fed by `--agent` on `pop tasks assist`, `pop tasks fold`
and `pop routine refine`. What is missing is only a way to fill it from a
keystroke, on a surface a human is already looking at.

## Decision

1. **The pick writes the persisted Agent override, and nothing else persists.**
   The chooser moves the selected entry to the head of `work.attended.agents`
   and stores the reordered list through `config.SetOverrideValue`. There is no
   new store row, no session memory, and no second lifetime. A dedicated
   "last attended pick" memory was rejected on ADR-0202 decision 5's own
   grounds — two overrides at two lifetimes on the same list is the
   invisible-state problem twice over — and it would have needed its own
   precedence rule against the layer and its own way of being seen and cleared.

2. **Reordering, not pinning.** The tail keeps its relative order behind the new
   head, so the value written is exactly the list a human would have typed in
   the editor, and the **Agent fallback** walk behind it is unchanged. ADR-0202
   decision 2 already dissolved promote-versus-pin by making the list itself the
   unit; this decision inherits that and adds nothing.

3. **Explicit trigger only — a launch is never interposed.** `tab` on a gate
   menu's assist row, `alt+a` on a dashboard's attended action row. `1` at a
   gate and `A` on a dashboard keep meaning exactly what they mean today, which
   matters most at a gate hit in the middle of a drain. Auto-opening the chooser
   whenever the list held two entries was rejected: it charges every launch a
   keystroke for a choice that is usually already right.

4. **The affordance renders only where a choice exists** — while the attended
   list holds two or more usable entries. With one entry there is nothing to
   choose and the hint would be noise; the moment a second is configured the key
   appears on the row, which is how the feature teaches itself. This makes the
   **Attended entry render** the discipline that carries it: a surface that says
   what will run also says how to change it.

5. **A narrow interactive writer is admitted to the override layer.** ADR-0202's
   "edited by one component" governs the *general-purpose key editor* — one
   surface for arbitrary exposed keys, with `$EDITOR` and free TOML text. This
   chooser writes one key, with a value it generated itself, through the same
   schema gate `--trunk`, `pop config repo set` and `pop workbench` already pass.
   Admitting it therefore leaves the rule standing rather than bending it: the
   Config dashboard remains the only place the layer is *edited*, and remains
   where every override is seen, previewed and removed with `ctrl+x`.

6. **The `ui/` component returns a choice; the host writes.** The chooser has no
   config dependency and reaches no writer. Its host — `tasks`' gate prompt or
   the dashboard page — performs the write, re-resolves the merged config, and
   re-renders the row from the result, mirroring the dashboard's existing
   `AfterConfigReload`. Rendering the pick optimistically was rejected: a value
   the schema gate refused must not leave the row lying about what will launch.
   ADR-0202 decision 11's host contracts survive intact, the stdout prohibition
   included.

7. **The non-TTY line path is unchanged.** `tab` means nothing to a line reader,
   so a headless gate sees no chooser and no hint. `--agent` stays the
   per-command answer there, and stays on top of the ladder (ADR-0202
   decision 9) everywhere.

8. **Scope at the cut**: a gate menu's assist row, task-set **Assist**, and
   **Map assist** — the launches a human reaches by hand. A **Map** fan-out
   takes one pick for the whole spawn, never one per pane, since a wall of
   prompts is not a choice. Routine refinement and fold-conflict assistance keep
   `--agent` only; they are reached by command far more often than by keystroke.
   `pop map assist` needs the override threaded up through `AssistInvocation`
   and `SpawnAssist`, which hardcode the empty spec today.

9. **The chord is deliberate, given ADR-0263.** On a dashboard the assist verb
   opens the **Assist pane menu**, whose grammar is digits and `n`. A chord
   cannot contend with that grammar, and `tab` on a dashboard is row marking
   that no mode may gate (ADR-0254 decision 4), so `alt+a` is the only key that
   works on both surfaces. It is the key ADR-0202 decision 10 returned to the
   pool "because muscle memory for it points at a picker that no longer exists";
   the muscle memory now points at something again, and at very nearly the same
   thing.

## Consequences

A keystroke at a gate mutates a file on disk, which no gate keystroke did
before. That is the point rather than a side effect — the whole complaint
ADR-0202 recorded is that the persisted choice was unreachable from where it is
made — and the mutation is one the Config dashboard shows with its override
marker and clears with `ctrl+x`, so it cannot become the invisible state the
deleted picker was.

The attended list's ordering becomes something a human reorders often and almost
never reads. That is safe only because the tail is pure fallback: if a later
decision gives tail position a second meaning, this chooser is where that
meaning would be silently rewritten.
