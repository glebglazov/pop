---
fragment: 182b48ca
generation: 0022
branch: master
---

+ Repository display label
  The label shown for a repository on machine-global **Queue** surfaces (the **Queue dashboard** PROJECT column, `pop queue status`, daemon output) — the depth-aware picker display name with the trailing worktree segment removed, so a bare repo reads `game server` (not `game server/main`) while a `display_depth = 2` repo still reads `work/game server`. Derived by `repoName()` from `ProjectLabel`, the pre-suffix `displayName` captured at project expansion. It denotes the repository (worktrees collapse to one **Repository identity**), distinct from the trunk **Worktree** shown in the WORKTREE column and from the git-identity basename (`RepoLabel` / `repoLabelFromScan`) used for keying and binding paths. The **Project picker** deliberately keeps the full `game server/main` — there each worktree is its own row (ADR-0117).
  avoid: repo label, RepoLabel (that is the identity basename, not this display value)
  under: Language
