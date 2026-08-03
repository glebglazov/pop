~ Work window
  The single tmux window, named `pop-work`, that pop spawns its Task-set panes into
  within the session of the checkout the set is **bound** to. Every pane for that set
  — drain, verify, assist, fold, runtime shell, and the **Work daemon**'s unattended
  auto-drain — lands here under a balanced (`tiled`) layout, instead of in the user's
  working windows. One Work window per session, created on first spawn and reused
  thereafter; a set bound to the **Trunk worktree** therefore shares the trunk
  session's Work window, and a set bound to a **Managed worktree** gets that
  checkout's own. It was named `pop-queue` before `pop queue` became `pop work`; a
  live `pop-queue` window at upgrade time is left orphaned rather than migrated, since
  drains are ephemeral.
  avoid: Drain session, queue tab, `pop-queue` (the pre-cut window name)
  was: The single tmux window, named `pop-work`, that the **Work daemon** spawns its
    drains into within a Project's session. All daemon-spawned drains for that project
    — both in-place and **Worktree set** — land here as panes under a balanced
    (`tiled`) layout, instead of in the user's working windows or in per-worktree
    sessions. One Work window per project session; created on first spawn, reused
    thereafter. It was named `pop-queue` before `pop queue` became `pop work`; a live
    `pop-queue` window at upgrade time is left orphaned rather than migrated, since
    drains are ephemeral.
    (avoid: Drain session, worktree session, queue tab, `pop-queue`)

~ Supervision scope
  The set of work the **Work daemon** supervises: **Auto-drain**-marked Ready Task
  sets in git-backed registered projects, plus every non-paused **Routine** regardless
  of whether its bound directory is git-backed (Routines are discovered from
  `routines/` in pop's data dir, not from project scanning). Running `pop work daemon`
  is standing consent to act, but the daemon drains only sets a human has marked
  **Auto-drain** (default off) and fires only Routines a human authored and has not
  paused; there is no per-project opt-in flag and no per-drain AFK start prompt. The
  per-set opt-in is **Auto-drain**, toggled from the **Work dashboard**; the per-set
  opt-out remains **Archive**; manual `i` from the **Work dashboard** drains a set
  regardless of its **Auto-drain** bit. **Work supervision** spawns plain `pop tasks
  implement <set>` — no `--yes` — so **HITL gate prompt** and **Failed gate prompt**
  stay interactive when the drain pane has a TTY. The blast radius is self-limiting
  because the daemon only acts on Auto-drain Ready sets and deliberately authored
  Routines; a project with no sets is skipped. A configured **Project** with no git
  checkout is also outside set-draining scope — it has no **Repository identity** and
  therefore no **Task storage**; the supervisor silently skips it like a project with
  no sets, never a scan error. A drain targets the session of the checkout its set is
  bound to; when that session does not exist the daemon creates it detached and
  spawns into its **Work window**.
  avoid: Per-project queue opt-in, global priority queue, per-drain --yes
  was: (as above, with the closing clause reading) When a project has no tmux session,
    the daemon creates one detached and splits a drain pane into that session's main
    window (index 0); subsequent drains split additional panes there.
