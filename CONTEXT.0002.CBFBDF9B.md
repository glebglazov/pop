---
fragment: CBFBDF9B
generation: 0002
branch: master
---

~ Auto-drain
  A per-set persisted consent bit in **Task state**, alongside priority and the archived flag, marking that the **Queue daemon** may automatically drain this **Task set**. It defaults off for a freshly-discovered set, inverting the old standing-consent model: `pop queue run` drains nothing until a set is marked auto-drainable from the **Queue dashboard**, or a human launches it by hand. A **Task manifest** may declare `"auto_drain": true` at the set level; pop reads that key once at first registration — whether via lazy discovery, import, or any other path that creates the registration entry — and seeds Task state accordingly; it does not re-sync on later refresh, so the **Queue dashboard** toggle remains authoritative after registration. It is orthogonal to **Archive** (which hides a set entirely) and distinct from a **Picked-up Task set**, which is a runtime fact (a live lock), not a consent fact.
  was: A per-set persisted consent bit in **Task state**, alongside priority and the archived flag, marking that the **Queue daemon** may automatically drain this **Task set**. It defaults off for a freshly-discovered set, inverting the old standing-consent model: `pop queue run` drains nothing until a set is marked auto-drainable from the **Queue dashboard**, or a human launches it by hand. A **Task manifest** may declare `"auto_drain": true` at the set level; pop reads that key once at first registration and seeds Task state accordingly — it does not re-sync on later refresh, so the **Queue dashboard** toggle remains authoritative after registration. It is orthogonal to **Archive** (which hides a set entirely) and distinct from a **Picked-up Task set**, which is a runtime fact (a live lock), not a consent fact.

+ Manifest auto-drain seed
  The one-time application of a **Task manifest**'s `"auto_drain"` key into **Task state** at first registration. When the key is the boolean `true`, pop sets the set's **Auto-drain** bit on; absent or `false` seeds off. Pop prints `(auto-drain)` on the registration line only when it seeded true. The key is never re-read after registration.
  avoid: auto-drain sync, manifest consent refresh
  under: Task execution

~ Task manifest
  The `index.json` within a Task set. It remains the source of truth for task eligibility and completion. It may optionally carry set-level keys beside the `tasks` array — today `auto_drain` — that express authoring intent consumed at first registration into **Task state**; those keys are not re-applied on refresh. Set-level keys must match their declared types; a non-boolean `auto_drain` is a contract fault that makes the Task set **Malformed**. Planning skills such as **to-tasks** write `"auto_drain": true` only when the human explicitly requests it in that session; otherwise the key is omitted.
  was: The `index.json` within a Task set. It remains the source of truth for task eligibility and completion.
