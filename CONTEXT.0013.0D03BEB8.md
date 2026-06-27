---
fragment: 0D03BEB8
generation: 0013
branch: master
---

~ Integration conflict
  An entry already present at an agent's skill location under an embedded skill's resolved install name or its bare (prefix-stripped) form that pop does not own. By default pop never installs over, removes, or refreshes a conflicting entry — it is skipped and reported, with the report naming the explicit command that would resolve it. The only way pop deletes a conflicting entry is a deliberate, confirmed conflict overwrite on an explicitly named agent: pop hard-deletes the unowned entry and links its own, asking before each overwrite (default no) unless an assume-yes is given, and never overwriting unattended without that assent. **Integration refresh** never overwrites a conflict.
  was: A skill already present at an agent's skill location under an embedded skill's name (with or without the `pop-` prefix) that pop does not own. Pop never installs over, removes, or refreshes a conflicting skill; the wizard and health check report the conflict and leave resolution to the user.

+ Integration opt-out
  The persisted per-agent negative consent recorded against a default-on **Integration component** — the agent's declared "this component is not wanted." Set by declining a component on an explicit `pop integrate <agent>` run or by `pop integrate remove`; cleared by a bare `pop integrate <agent>`, which re-asserts the full default set. An explicit decline is **declarative**: it both records the opt-out and removes the component if currently installed (pop-owned only, no prompt — pop freely manages what it owns). **Integration refresh** honours the opt-out by never re-adding it, but never itself removes an installed-but-opted-out component; removal is reserved to an explicit run.
  avoid: skip flag, decline, negative flag, no-install

~ Integration refresh
  The reconcile pass that brings already-installed **Integration component**s up to the current binary — run automatically when the pop binary changes (picker launch) and on demand without an agent argument. For each integrated agent it updates stale pop-owned components and adds any default component that is missing and not opted-out (see **Integration opt-out**), reporting each outcome with its reason. It never removes a component and never overwrites an **Integration conflict** — both deletions are reserved to an explicit `pop integrate <agent>` run; refresh only ever adds, updates, or skips-with-reason. It never prompts and leaves agents with no pop integration alone.
  was: The automatic re-render of already-installed Integration components when the pop binary changes. Refresh never adds components the user did not opt into, never prompts, and leaves uninstalled agents alone.
