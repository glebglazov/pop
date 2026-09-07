---
fragment: 38744532
generation: 0050
branch: master
---

~ Read-only agent posture
  The **Adapter capability** by which an **Agent preset** is told, in argv, to run without the ability to change the checkout it was pointed at. claude contributes `--disallowedTools=Edit,Write,NotebookEdit`, codex `--sandbox read-only`, cursor `--mode ask`, pi `--exclude-tools edit,write`; opencode and kimi declare it blind, and pop states the posture it actually obtained rather than the one it wanted. The **Explore phase** is the role spawned under it: its whole output is prose pop files, so withholding the editing tools is a stronger guarantee than a sentence in a prompt asking for the same restraint. The **Refiner** — formerly its one consumer, as the Reviewer — is not, since it fixes in place. The capability stays declared per preset because it is a fact about what each CLI can do, not a preference.
  avoid: sandbox mode, review worktree, permission mode
  was: The **Adapter capability** by which an **Agent preset** is told, in argv, to run without the ability to change the checkout it was pointed at. claude contributes `--disallowedTools=Edit,Write,NotebookEdit`, codex `--sandbox read-only`, cursor `--mode ask`, pi `--exclude-tools edit,write`; opencode and kimi declare it blind, and pop states the posture it actually obtained rather than the one it wanted. No role is currently spawned under it: the **Refiner** — formerly its one consumer, as the Reviewer — now fixes in place. The capability stays declared per preset because it is a fact about what each CLI can do, not a preference, and the per-preset flag research should not be re-derived when the next read-only role appears.
