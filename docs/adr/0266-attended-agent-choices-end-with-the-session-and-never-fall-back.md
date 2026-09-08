---
status: accepted
---

# Attended agent choices end with the session and never fall back

An Assist session opened by the dashboard carries an agent flag. A Tab choice
was saved globally but could not replace that flag. The human wants the choice
to control this conversation without changing the next run or another pane.

This amends [the attended chooser decision](0264-an-attended-launch-surface-writes-the-agent-override-it-renders.md),
[the config override decision](0202-config-overrides-are-a-top-ranked-layer-edited-by-one-component.md),
and the attended launch-time skip rule in [the attended invocation decision](0195-an-attended-entry-owns-its-whole-invocation.md).

## Decision

1. A successful Tab choice is an **Attended session choice**. It lasts for one
   interactive command invocation: an Assist session, a drain across its gates,
   or a Routine refinement or fold-conflict session. Returning from an agent to
   the menu retains it. A separate invocation, even in the same pane, starts
   without it. It writes no config or store state and changes no other session.
2. Attended precedence is: session choice, explicit attended agent flag, saved
   Agent override, source config, built-in default. A drain's implementation
   agent flag still selects its unattended group, not its attended assistance.
   Cancelled or failed choices preserve the current selection.
3. A choice selects the whole Agent entry, including its model and arguments.
   No model or argument is inherited from the entry it replaces. Existing
   preset argument defaults still apply where the entry states no value.
4. An attended launch selects exactly one entry. Without a session choice or
   flag, it selects the first usable configured attended entry, else the existing
   built-in default. A valid entry with a missing executable or quota cooldown
   remains the selected entry; availability must not promote another entry.
   If it cannot launch, report why and return to the menu where one exists.
   Direct commands report the error. Never try another attended entry, either
   before launch or after the selected agent exits. The attended list is a list
   of choices, not a fallback list. Unattended fallback is unchanged.
5. Remove dashboard alt+a and its chooser, hint, persistence behaviour and
   dedicated key reservation. Do not add a replacement dashboard chooser.
   Keep the Config dashboard as the surface for saved Agent overrides.
6. The attended row renders the effective entry, including an explicit flag or
   session choice, so the displayed agent and model match the launch. Gate Tab
   remains available when at least two valid configured choices exist. Missing
   executables and cooldowns are launch errors, not reasons to silently pick
   another entry. Malformed entries remain excluded from the chooser.

The persistence and fallback rules above replace the corresponding rules in
the older decisions. Saved overrides already on disk remain user config; do
not delete them because the writer has changed.

## Alternatives rejected

Saving a Tab choice affects future runs and other panes. It is the wrong
lifetime for a choice made inside an attended session. Letting a launch flag
win after Tab makes the newer choice ineffective. Automatic attended fallback
can start a different agent or model from the one the human selected; an
attended failure should give the choice back to the human.
