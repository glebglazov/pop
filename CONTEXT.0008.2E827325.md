---
fragment: 2E827325
generation: 0008
branch: now-pop-tasks
---

+ Run-next badge
  The `NEXT` marker `pop tasks status` prints on the single highest-priority **Ready** Task set — the set a no-argument `pop tasks implement` would drain next in the local runner. Display-only (a derived row flag), unrelated to daemon consent; once the set is actually running the badge reads `RUN`, not `NEXT RUN`.
  avoid: AUTO badge, auto-pick, auto-picked, auto-pick badge
  under: Language

~ Auto-drain
  A per-set persisted consent bit in **Task state**, alongside priority and the archived flag, marking that the **Queue daemon** may automatically drain this **Task set**. It defaults off for a freshly-discovered set, inverting the old standing-consent model: `pop queue run` drains nothing until a set is marked auto-drainable from the **Queue dashboard**, or a human launches it by hand. A **Task manifest** may declare `"auto_drain": true` at the set level; pop reads that key once at first registration — whether via lazy discovery, import, or any other path that creates the registration entry — and seeds Task state accordingly; it does not re-sync on later refresh, so the **Queue dashboard** toggle remains authoritative after registration. It is orthogonal to **Archive** (which hides a set entirely), distinct from a **Picked-up Task set** (a runtime live-lock fact, not consent), and distinct from the **Run-next badge** (`NEXT`, a local-runner display marker that shares the word "auto" only in the retired `AUTO` badge — they are unrelated).
  was: A per-set persisted consent bit in **Task state**, alongside priority and the archived flag, marking that the **Queue daemon** may automatically drain this **Task set**. It defaults off for a freshly-discovered set, inverting the old standing-consent model: `pop queue run` drains nothing until a set is marked auto-drainable from the **Queue dashboard**, or a human launches it by hand. A **Task manifest** may declare `"auto_drain": true` at the set level; pop reads that key once at first registration — whether via lazy discovery, import, or any other path that creates the registration entry — and seeds Task state accordingly; it does not re-sync on later refresh, so the **Queue dashboard** toggle remains authoritative after registration. It is orthogonal to **Archive** (which hides a set entirely) and distinct from a **Picked-up Task set**, which is a runtime fact (a live lock), not a consent fact.
