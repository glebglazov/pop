# Pop

A CLI for navigating between development directories and their tmux sessions. Pop tracks which panes need attention and provides fuzzy-search pickers for switching context quickly.

## Language

**Project**:
A directory on disk that pop knows about — either listed explicitly in config or matched by a glob pattern. Choosing a project in the project picker is the primary workflow; attaching to or creating a tmux session follows from that choice.
_Avoid_: Folder, workspace, session (when you mean the directory itself)

**Project command**:
The `pop project` entry point — opens the project picker. Project-specific config lives in `[project]`. `pop select` and `[select]` are deprecated aliases; removal is gated on beta-tester sign-off (tracked in CLEANUP.md). The CLI alias is hidden (not shown in help) and emits no runtime warning; the config alias emits a load-time warning.
_Avoid_: Select command, normal mode

**Project readiness**:
The **Doctor status** of the `pop project` command family. It depends on tmux availability, loadable project configuration, and at least one selectable project, worktree, or standalone session. A missing config file is not Blocked by itself because `pop project` can enter the first-run configure flow; an existing but invalid config is Blocked.
_Avoid_: Config existence check

**Session**:
The tmux session pop creates or attaches to when you select a project or worktree. One project maps to one session; selecting it puts you in that session (creating it first if needed).
_Avoid_: Project (when you mean the tmux session, not the directory)

**Session name**:
The sanitized tmux identifier pop uses to refer to a **Session**. Each checkout path has exactly one session name, built the same way everywhere from git repo context — not from config or picker display labels. For a worktree in a bare repo, `repoName/worktreeFolderName`. For a worktree in a non-bare repo, the worktree folder name. When the path is not a git checkout, the directory base name. Dots and colons are replaced for tmux compatibility. Works for any checkout path pop can resolve, including paths outside configured projects. **Standalone sessions** use tmux's existing name as-is.
_Avoid_: Config display name, display_depth, raw absolute path

**Standalone session**:
A tmux session that appears in the picker but has no corresponding project in config. Pop discovers these from tmux directly; its **Session name** is whatever tmux already uses.
_Avoid_: Orphan session, external session

**Worktree**:
A linked checkout of a git repository at a separate path. Each worktree is also a project — it appears in the picker and gets its own session. Bare repos expand into their worktrees rather than appearing as a single entry.
_Avoid_: Checkout, clone (when you mean a worktree specifically)

**Pane**:
A tmux pane that pop tracks for attention status, visit time, and optional notes. Untracked tmux panes are outside pop's domain.
_Avoid_: Terminal, window (tmux window ≠ pane)

### Pane status

**Working**:
The pane's agent or process is actively running.
_Avoid_: Busy, active

**Unread**:
The pane has output or a state change that needs your attention.
_Avoid_: Needs attention

**Clear**:
No attention is required — either you've acknowledged the pane or nothing new is pending.
_Avoid_: Idle, read

**Active pane**:
A pane currently visible to the user in tmux. A pane may be **Active** regardless of whether its status is **Working**, **Unread**, or **Clear**.
_Avoid_: Working pane, focused pane

**Dashboard**:
The presentation of the monitored set of panes — a browsable view of registered panes, their status, and visit times. `pop monitor dashboard` opens this view; `pop dashboard` is only a hidden compatibility alias.
_Avoid_: Monitor (when you mean the tracking mechanism, not the view)

**Monitor**:
The subsystem that maintains the monitored set of registered panes — tracking status, visit times, and notes via daemon, state, and tmux hooks. Agent integrations report into the monitor; the dashboard reads from it. Exposed via `pop pane monitor-start`, `monitor-stop`, and `monitor-status`.
_Avoid_: Dashboard (when you mean the view, not the mechanism)

**Monitor readiness**:
The **Doctor status** of the `pop monitor` command family. It depends on tmux availability, a running or startable monitor daemon, readable monitor state, tmux focus-event/hook support for visit tracking, and status wiring for agents in **Doctor intent**. Missing or broken setup for only some intended agents is Partial; monitor operation with limited automatic visit or status quality is Degraded; inability to run the daemon or read monitor state is Blocked.
_Avoid_: Agent integration table

**Agentic pane**:
A pane running an AI coding agent or its runtime (e.g. Claude, OpenCode, Pi). Integrations cause these panes to register with the **Monitor**; other panes may also be tracked explicitly.
_Avoid_: Agent pane, bot pane

**Registration**:
A pane entering the **Monitor**'s tracked set. A pane is **tracked** once registered; untracked panes are outside pop's domain.
_Avoid_: Tracking (when you mean the act of entering the set, not the ongoing state)

**Auto-registration**:
**Registration** that happens as a side effect of an untracked pane's first report, rather than an explicit add — the common path for **agentic panes** via **integrations**. The trigger differs by report: reporting a status auto-registers the pane unless registration is suppressed; setting **Following** auto-registers only when following (never when unfollowing); a **Visit** never auto-registers.
_Avoid_: Self-registration (same event seen from the agent's side; prefer auto-registration)

### Agent integrations

**Agent integration**:
The per-agent wiring that makes a coding agent report pane status into the **Monitor** — hooks or an agent extension installed by `pop integrate`. Plumbing only: an agent integration never adds skills or other behavior-changing files to the agent.
_Avoid_: Skill install, setup, framework

**Integration component**:
An individually consented unit `pop integrate` can install for one agent: the status wiring (core), the **Pane skill**, or the **Task planning skills**. Running integrate implies consent to the status wiring only; every other component is an explicit per-component opt-in.
_Avoid_: Bundle, default install

**Integration wizard**:
The re-entrant interactive flow of `pop integrate <agent>`. It shows each **Integration component**'s current state, explains what the component brings before asking, and may be re-run at any time to add or remove components. Non-interactive runs require explicit component flags; without them they fail rather than installing a default.
_Avoid_: One-shot installer, setup command

**Integration refresh**:
The automatic re-render of already-installed **Integration components** when the pop binary changes. Refresh never adds components the user did not opt into, never prompts, and leaves uninstalled agents alone.
_Avoid_: Auto-install, update prompt

**Doctor**:
The readiness report opened by `pop doctor`: a top-level command-family view of whether Pop's user-facing workflows can run on this machine. Its canonical first-pass families are `pop project`, `pop worktree`, `pop monitor`, `pop pane`, `pop tasks`, and `pop integrate`; hidden or deprecated aliases are not top-level families. Doctor drills into subcommands or agent-specific integration state only where they explain a degraded or unavailable workflow. Read-only; it never installs or repairs.
_Avoid_: Integrate status, health subcommand

**Doctor status**:
The aggregate readiness state for one command family in **Doctor**. OK means the family's core workflow should run; Partial means the family is available through some configured variants but unavailable through others, most commonly agent-specific support or setup; Degraded means the core workflow can run but a relevant optional capability is missing, stale, conflicting, or otherwise limited; Blocked means the core workflow cannot run; N/A means the family intentionally does not apply in the current environment. Partial, Degraded, and Blocked statuses must name the concrete reason.
_Avoid_: Health score, severity level

**Doctor rendering**:
The terminal presentation of **Doctor**. It should be visually scannable with ANSI color and stable ASCII/Unicode-safe status labels rather than emoji or custom pictograms; alignment must remain reliable in plain terminals, logs, and CI output. The primary structure is one row per top-level command family, with terse assessment checks printed directly beneath that family when they explain how the status was reached.
_Avoid_: Emoji health report, decorative symbols

**Doctor intent**:
The set of variants Doctor has reason to evaluate for a command family. Doctor reports missing or broken agent-specific setup only for agents the user appears to use through Pop configuration, installed Pop artifacts, or an explicit command context; unsupported but unused agents are suggestions, not reasons for Partial or Degraded status. Agent intent is inferred first from task execution configuration, then from Pop-owned integration artifacts or Pop hooks/extensions already present, and only then from explicit command context. Merely having an agent executable installed is a suggestion, not intent.
_Avoid_: Supported agents matrix, all possible integrations

**Integration conflict**:
A skill already present at an agent's skill location under an embedded skill's name (with or without the `pop-` prefix) that pop does not own. Pop never installs over, removes, or refreshes a conflicting skill; the wizard and health check report the conflict and leave resolution to the user.
_Avoid_: Stale integration, collision overwrite

**Pane skill**:
The embedded skill that teaches an agent to drive `pop pane`. An opt-in **Integration component**; pane monitoring works without it.
_Avoid_: Agent integration, hooks

**Task planning skills**:
The embedded, pop-independent skills (grill-with-docs, to-prd, to-tasks) whose output feeds Task sets. Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.
_Avoid_: Workload framework, workload skills bundle, agent integration

### Pickers

**Project picker**:
The fuzzy-search picker opened by the project command — for choosing a project, worktree, or standalone session.
_Avoid_: Session picker, select view, normal mode

**Worktree picker**:
The fuzzy-search picker in `pop worktree` for choosing or deleting git worktrees in the current repository. Worktree creation is not built in; it belongs to user-defined commands, which hand the new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone (killing it stays an explicit, separate action).
_Avoid_: Repo picker

**Worktree readiness**:
The **Doctor status** of the `pop worktree` command family. It depends on being able to identify the current Git repository and list its worktrees. A repository with no linked worktrees is still OK; the absence of worktrees is content, not a readiness failure.
_Avoid_: Worktree count health

**History**:
The persisted record of projects you've selected or switched to, with timestamps.
_Avoid_: Recents, access log

**Switch**:
Attaching to — or creating, then attaching to — the session for a path, recording it in **History**. The non-picker entry point (`pop project switch <dir>`), used by external tooling (e.g. worktree-creation scripts) so out-of-band paths still land in **History**.
_Avoid_: Open, jump

**Unread view** (removed):
Previously a sub-view in the project picker (entered via `→`) for quickly jumping to unread panes. Removed — unread panes are now only accessible via the **Dashboard**.
_Avoid_: Attention view, triage view

**Visit**:
Recording interaction with a pane — focus, switch, or an explicit `pop pane visit`. Updates the pane's last-active time and drives recency ordering on the dashboard. Not the same as clearing unread; a Clear pane still accumulates visits.
_Avoid_: Acknowledgment, last seen

**Following**:
A dashboard-scoped mark for ongoing interest in a tracked pane. The mark persists across dashboard openings while the pane exists; following mode filters the dashboard to show only followed panes.
_Avoid_: Pin, watch

**Integration**:
An agent setup that connects a coding tool (Claude, Pi, OpenCode) to the monitor, so its pane self-reports status. Installed via `pop integrate <agent>`.
_Avoid_: Hook, plugin (when you mean the whole setup, not a single file)

### Tasks

#### Lifecycle

This overview relates the terms defined below; read it before changing task behaviour. It is a domain model, not an implementation guide.

A **task** moves between four statuses. The executor drives the solid transitions; the human drives the dashed ones through manual override commands.

```
                       Implement (agent success)
        open ──────────────────────────────────────────▶ done
         │ ▲                                              ▲ ▲
         │ │ Open task                         Complete   │ │ Complete
         │ └──────────── failed ◀── attempt ───┐ task ────┘ │  task
         │                  │   exhaustion/timeout           │
         │ Skip task        │ Open task                      │
         ▼                  ▼                                │
      skipped ─────────────┴────────────────────────────────┘
              Open task (skipped → open) / Complete task (skipped → done)
```

- A task is **eligible** when it is `open`, type AFK, and every `blocked_by` prerequisite is satisfied. A prerequisite counts as satisfied when it is `done` **or** `skipped` — a Skipped task unblocks its dependents even though it was deferred, not completed.
- HITL tasks are never eligible; the executor never runs them.
- A HITL task contains only human work — verification, decisions, manual checks. Agent-doable prep (building the artifact to verify) belongs in a separate AFK task that the HITL task is blocked by; a HITL task describing software to build is mis-typed.
- `complete`, `skip`, and `open` are the only manual overrides; each moves exactly one task and bypasses the agent.

A **Task set**'s status is derived from its tasks, in this precedence:

```
all tasks done .............................. DONE
any task failed ............................. FAILED
has an eligible AFK task .................... READY      ← Implement drains these
every task done or skipped, ≥1 skipped ...... DEFERRED   ← conclude or reopen later
otherwise (unfinished, none eligible) ....... BLOCKED    ← Human-blocked: HITL or undone dependency
```

(`MISSING` and `MALFORMED` sit outside this derivation — they are registration and contract faults.) Automatic selection runs READY sets in scheduler order and passes over DONE and DEFERRED sets; only when no READY set exists may a no-argument implement select a single unambiguous Human-blocked Task set for attended help, and only when the block is an open HITL task rather than an unresolved AFK dependency. Multiple Human-blocked Task sets are ambiguous and require an explicit target. Draining stops when its selected set reaches DONE, FAILED, BLOCKED, or DEFERRED, or when an **Agent quota pause** interrupts draining without changing task status. At a BLOCKED HITL gate, interactive runs show a **HITL gate prompt** while non-interactive runs and `--yes` preserve stop-and-advice output and never auto-start attended assistance.

**Tasks readiness**:
The **Doctor status** of the `pop tasks` command family. Because the tasks feature is aimed at Git projects and its central workflow is agent execution from a **Runtime path**, Doctor reports `pop tasks` as Blocked when no Git runtime checkout can be resolved, even if read-only status rendering could still inspect local artifacts.
_Avoid_: Workload readiness, task status availability

**Task set**:
The local `<id>/index.json` manifest and its sibling task markdown files beneath the **Task storage** `tasks/` directory. A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. It may be created from a PRD, a grilling session, or another planning workflow; PRD existence is irrelevant to task scheduling and execution.
_Avoid_: Issue set, PRD, workload

**Task set registration**:
A Task set entering the repository's **Task state** so pop may select tasks from it. Pop automatically registers discovered Task sets and reports newly registered Task sets to the user. Registration metadata and Task set artifacts remain machine-local.
_Avoid_: Import, tracking

**Repository identity**:
The key mapping a repository to its **Task storage**: the hash of the canonical git common directory path. All worktrees of one repository share one identity and therefore one Task storage. A fresh clone or a moved repository is a new identity.
_Avoid_: Remote URL, project name, worktree path

**Task storage**:
The per-repository directory in pop's data dir where a repository's Task sets live — `repos/<repo-basename>-<short-hash>` from **Repository identity**. It contains a `repo.json` reverse-lookup marker, a `tasks/` directory, and the repository's **Task state**; discovery scans `tasks/*/index.json` beneath it. It is derived, never configured, and created on demand by **Show path**. Nothing task-related lives inside the repository tree.
_Avoid_: Workload storage, workload definition path, thoughts directory, project root, runtime path

**Show path**:
Printing the absolute path to the current repository's Task storage `tasks/` directory, or to one Task set's directory when given a target. It creates the Task storage on demand, making it the single entry point for humans (`cd`, `$EDITOR`) and for planning skills that write Task sets.
_Avoid_: Path command, show command, status table

**Legacy migration**:
The one-shot move of legacy `thoughts/issues/` Task sets from the current worktree into Task storage via `pop tasks migrate`, rekeying **Task state** entries while preserving registration metadata and priority. A Task set whose identifier already exists in storage is reported and skipped, never merged. Legacy global-ignore entries are left untouched.
_Avoid_: Import, worktree sweep

**Storage layout migration**:
The automatic, idempotent move of a pre-rename storage layout (`workloads/<repo>/issues/`, issue-keyed manifests) to the current layout on first touch. It never merges colliding identifiers and reports what moved.
_Avoid_: Legacy migration, manual rekeying

**Orphaned task storage**:
A Task storage directory whose recorded repository path no longer exists. Doctor reports it; pop never deletes it automatically.
_Avoid_: Missing Task set, stale registration

**Runtime path**:
The git checkout from which task execution starts. It defaults to the selected project's path and may be overridden for a command. Pop resolves it to the checkout root and uses that root for the agent working directory, dirty-tree preflight, staging, commits, and the Runtime execution lock. Task artifacts remain in the separate **Task storage**. Durable runtime path configuration is deferred until worktree-oriented execution needs it.
_Avoid_: Workload runtime path, task storage, shared git root

**Dirty runtime strategy**:
Controls how task execution starts from a dirty runtime checkout. `continue` starts execution without modifying the existing dirty state; it is the default both when the option is absent and when it is present without a value, and after successful task completion the normal implementation commit intentionally includes both pre-existing and agent changes. `commit-and-continue` captures the existing dirty state in a separate implementation commit before invoking the agent. `stash-and-continue` stashes tracked and untracked changes but not ignored files, prints the stash reference when one is created, and leaves restoration to the user; an empty stash does not prevent execution. When the runtime is dirty the command always displays `git status` and the chosen strategy's effect, then requires interactive `y` confirmation; `--yes` auto-confirms, and a non-interactive run without `--yes` is rejected. Implement applies the chosen strategy once before draining its selected Task set.
_Avoid_: Clean runtime checkout requirement, automatic stash restoration

**Implementation commit**:
A commit created by the task executor from runtime-checkout changes. After successful task completion, the executor stages all runtime changes and commits them with a task-derived subject and the agent summary as body. The subject's scope names the Task set by its identifier without the timestamp prefix. Task artifacts remain local and unstaged.
_Avoid_: Task artifact update, progress record

**Task manifest**:
The `index.json` within a Task set. It remains the source of truth for task eligibility and completion.
_Avoid_: Issue manifest, workload, dashboard

**Task parent reference**:
Optional planning context written inside a task markdown file, such as a `## Parent` section pointing to a PRD or another artifact. A task may be self-contained. Pop does not require, synthesize, validate, or interpret parent references.
_Avoid_: Required PRD pairing, Task set identity

**Task project resolution**:
Choosing the project path for a tasks command. A unique project display-name match may be selected explicitly; ambiguous names must be rejected with candidate paths. A direct path may be supplied as an escape hatch. When neither is supplied, the current directory is used.
_Avoid_: Worktree discovery, task storage

**Task set priority**:
A numeric value used to choose between ready Task sets. Newly registered Task sets start at priority `0`. Higher priority wins; equal-priority Task sets retain registration order.
_Avoid_: Task dependency, task-manifest order

**Task set status**:
The status derived from a discovered Task set whenever a tasks command runs. A **Ready** Task set has at least one eligible task; a **Done** Task set has only done tasks; a **Failed** Task set has at least one failed task; a **Blocked** Task set is unfinished but has no eligible task. Pop does not persist a separate completion flag, so artifact changes naturally affect the next derived status.
_Avoid_: Pane status, persisted Task set completion

**Next task**:
Selecting and executing one task from the highest-priority Ready Task set. Non-runnable Task sets are reported and skipped; among Ready Task sets, equal priority retains registration order.
_Avoid_: First registered Task set, highest-priority Task set regardless of status

**Task executor**:
The mechanism that runs a selected task through an agent, verifies completion, updates the task manifest and progress record locally, and commits implementation changes.
_Avoid_: Workload executor, scheduler

**Implement**:
The single task-execution command, `pop tasks implement`, that runs tasks through the **Task executor** and dispatches by **Task target reference** shape — there is no separate one-vs-many verb. Given a Task-set-relative file reference `<task-set>/<file>.md`, it executes exactly that one task, which must be Open, AFK, and have satisfied dependencies. Given a bare Task set identifier — or no argument, in which case pop chooses the highest-priority Ready Task set — it **drains** that set, executing eligible tasks sequentially until the set becomes Done, Blocked, Deferred, or Failed, or until an **Agent quota pause** stops cleanly; it does not continue into another Task set. There is no way to run a single auto-picked task: a no-argument implement always drains, and exactly one task runs only when a file reference names it. Only when no Ready Task set exists may a no-argument implement instead attend one unambiguous Human-blocked Task set through a **HITL gate prompt**; multiple Human-blocked sets are ambiguous and require an explicit target, and an explicitly targeted Human-blocked set may be attended even when Ready sets exist elsewhere. Draining requires explicit consent once per session before executing AFK tasks in the selected Task set, phrased "Run AFK tasks in this Task set?"; a single targeted task asks "Run task?". When the selected Task set is already Human-blocked at a HITL gate, implement may go directly to the HITL gate prompt only if it will still ask for AFK-execution consent before running any AFK task the gate unblocks. Paths, bare filenames, and bare task identifiers are rejected.
_Avoid_: Run, Drain, separate one-vs-many verbs, run issue, run issues, run all, next Task set, Run PRD

**Agent preset**:
A named headless agent command known to the task executor. An explicit agent command may override a preset. The executor appends its generated prompt as the final positional argument and disconnects stdin.
_Avoid_: Integration

**Interactive agent preset**:
A named attended-assistance command known to an Agent adapter. It is separate from an Agent preset because assisting a human at a HITL gate is an attended conversation, not a headless task attempt; custom headless agent commands do not imply an interactive preset.
_Avoid_: Agent preset, stripped headless command, agent-cmd

**Agent adapter**:
The preset-specific bridge between Pop and a supported agent. An adapter may provide headless invocation, headless output handling, and agent-assistance invocation; fallback for attended assistance belongs inside the selected adapter rather than in the HITL gate prompt.
_Avoid_: Universal JSON protocol, agent integration

**Agent output handling**:
The Agent adapter capability that interprets an agent's headless output. It may recover completion text or detect an **Agent quota pause** from a structured protocol; when it cannot interpret the output, the original text remains subject to the normal **Completion sentinel** contract. It may also render the agent's activity live as it streams — assistant prose plus a compact tick per tool use — so a structured run shows progress instead of going silent until it ends. Live rendering is cosmetic: the captured raw output, not the rendered view, remains the source of truth for completion assessment and quota detection.
_Avoid_: Interactive invocation, universal JSON protocol

**Agent output mode**:
Controls whether one Agent preset uses its Agent output handling or a plain-text compatibility fallback. In adapter mode the agent's activity is rendered live as it streams; plain-text mode passes the agent's raw output through untouched and disables adapter capabilities such as Agent quota detection.
_Avoid_: Agent quota reporting, universal JSON protocol

**Agent quota reporting**:
Proactively displaying subscription quota remaining in a provider-specific rolling window, such as a five-hour limit. This is separate from **Agent quota detection** and remains deferred until each agent CLI exposes a supported headless status interface. Token totals, private authentication-file access, undocumented endpoints, and interactive-terminal scraping are not substitutes for quota reporting.
_Avoid_: Token usage, API cost

**Agent quota detection**:
Identifying from Agent output handling that a task attempt stopped because the agent allowance is exhausted. Detection is preset-specific and relies on a stable headless signal. A detected quota pause stops implement cleanly without retrying, leaves the task Open, preserves partial runtime changes, and does not append a progress record. It is not a Failed, Skipped, or Interrupted task. Proactively reporting remaining allowance is the separate **Agent quota reporting** concern.
_Avoid_: Agent quota reporting, failed task, skipped task

**Agent quota pause**:
The clean stop produced by Agent quota detection. It leaves the current task Open and preserves its partial runtime changes, so a later implement invocation may resume work after allowance returns.
_Avoid_: Exhausted task, Interrupted task, Failed task

**Task attempt**:
One agent invocation for a task. The task executor retries an unsuccessful task up to the configured maximum, defaulting to three attempts. Exhaustion marks the task Failed, records the attempt count and reason locally, and stops draining.
_Avoid_: Task set retry, task dependency

**Task attempt timeout**:
The maximum duration for one task attempt, defaulting to one hour and configurable per command. When exceeded, the task executor terminates the agent process group, preserves partial changes, marks the task Failed locally, appends a Failed progress record, and stops immediately without further retries. A deliberate retry requires an **Open task** override.
_Avoid_: Task set timeout, interruption

**Human-blocked Task set**:
A Task set with unfinished tasks but no eligible AFK task because human-in-the-loop work must happen first. Implement reports the condition and stops; the task executor never automatically runs HITL tasks. On stopping, pop prints the blocking task body verbatim — the human sees what to do without opening the file — and advises the recovery paths for the blocking HITL task: Complete task once the human work is done, edit the task file and re-run, or skip the task to defer it and unblock its dependents (Skipped task). The blocked row also shows a copy-paste complete hint, symmetric with the open hint on Failed rows.
_Avoid_: Failed Task set

**HITL gate prompt**:
An interactive choice shown when implement reaches or selects a Human-blocked Task set. It defaults to getting agent assistance while still letting the human complete the task, defer it, or exit without changing task state; choosing complete or defer is the explicit manual decision and does not ask for a second yes/no confirmation. After complete or defer clears the blocking HITL task, implement refreshes the same Task set and continues from any newly eligible AFK task. When shown because a no-argument implement found no Ready Task set, it is framed as "No runnable AFK work" rather than as a dead end.
_Avoid_: Automatic HITL execution, yes/no launch prompt

**HITL assistance session**:
An attended agent session started from a HITL gate prompt with the blocking HITL task and surrounding Task set context loaded. It helps the human inspect, verify, and decide; it does not make HITL tasks eligible for unattended execution.
_Avoid_: Agent attempt, automatic HITL fallback

**HITL assistance prompt**:
The context loaded into a HITL assistance session. It identifies the Task set and blocking HITL task, includes the HITL task body, summarizes completed AFK work when available, and names the allowed manual outcomes without changing task state by itself.
_Avoid_: Agent transcript, completion sentinel

**Task artifact**:
A machine-local planning document, task markdown file, task manifest, or progress record within **Task storage**. Task artifacts live outside the repository tree, so they can never enter implementation commits and require no ignore configuration.
_Avoid_: Workload artifact, implementation change, task state

**No-op task completion**:
A successful task execution that produces no staged implementation change. The task executor marks the task Done locally, appends progress, reports that no implementation commit was created, and allows draining to continue.
_Avoid_: Failed task, empty commit

**Exhausted task**:
A task that remains unsuccessful after its maximum attempts. The task executor marks it Failed locally, preserves any partial implementation changes for inspection, does not commit them, and stops draining.
_Avoid_: No-op task completion, reverted task

**Interrupted task**:
A task whose active agent process was terminated by user interruption or process termination. The task executor forwards termination to the agent process group, preserves partial implementation changes, leaves task artifacts unchanged, and exits without committing. An interrupted task is not Failed.
_Avoid_: Exhausted task, failed task

**Open task**:
Explicitly returning one Failed or Skipped task to Open via `pop tasks open` so it may be attempted again — the command is named for the target status. It requires a positional Task-set-relative file reference, `<task-set>/<file>.md`; paths, bare filenames, and bare task identifiers are rejected. It removes any recorded attempt count, appends a local progress entry, preserves runtime files, and does not commit. The status table prints copy-paste open hints in the same `<task-set>/<file>.md` form.
_Avoid_: Issue reset, reset, automatic retry

**Complete task**:
Manually marking one Open, Failed, or Skipped task Done via `pop tasks complete` without running an agent, regardless of task type. Used primarily to clear a human-in-the-loop task after the human performs the work, to conclude a Skipped task once its deferred verification is satisfied, and also valid for finishing an AFK or Failed task by hand. The command requires a positional Task-set-relative file reference, `<task-set>/<file>.md`; paths, bare filenames, and bare task identifiers are rejected. All `blocked_by` dependencies must be Done. It bypasses the Completion sentinel — it does not verify acceptance criteria, does not prompt for confirmation, and does not stage or commit implementation changes; the human owns and commits that work. It appends a local COMPLETE progress record noting the prior state.
_Avoid_: Complete issue, completion sentinel, no-op task completion, run

**HITL gate completion**:
Completing the blocking HITL task from a HITL gate prompt after explicit confirmation. It uses the same state transition as Complete task, then implement continues draining the Task set instead of stopping at the cleared gate.
_Avoid_: Completion sentinel, automatic HITL execution

**HITL gate deferral**:
Skipping the blocking HITL task from a HITL gate prompt after explicit confirmation. It uses the same state transition as Skipped task, then implement continues draining the Task set because a Skipped task satisfies dependent `blocked_by` prerequisites.
_Avoid_: Failed task, automatic HITL execution

**Skipped task**:
A task the human deliberately set aside via `pop tasks skip`, recorded with the `skipped` status. Skipping accepts only an Open task of any type and is the deadlock breaker when a human-in-the-loop task cannot be verified until its own follow-up tasks complete. A Skipped task is never selected for execution, yet — unlike an Open dependency — it satisfies `blocked_by` for its dependents, so downstream tasks become eligible against a deliberately deferred, not completed, prerequisite. The command mirrors **Open task** targeting and appends a local SKIP progress record. A Skipped task later resolves through Complete task (to Done) or Open task (to Open).
_Avoid_: Skipped issue, exhausted task, interrupted task, blocked task

**Deferred Task set**:
A Task set in which every task is Done or Skipped and at least one is Skipped, so no runnable, failed, or open work remains but the set is not Done. Implement stops cleanly reporting the deferral rather than an error, and automatic selection passes over it like a Done set so it never blocks selection. The status table keeps it visible with its skipped count so the human remembers to conclude or reopen the Skipped tasks. A set with any still-Open task, including an Open HITL task, is Ready or Human-blocked rather than Deferred.
_Avoid_: Done Task set, Human-blocked Task set

**Progress record**:
The append-only local `progress.txt` history beside a task manifest. It records terminal Done and Failed outcomes, explicit task reopenings, and manual completions. Intermediate attempts are streamed during execution but are not appended.
_Avoid_: Task state, agent output log

**Completion sentinel**:
The machine-readable ending emitted by an agent after a task attempt. Success requires a zero agent exit status, a summary block followed by `TASK_COMPLETE`, and every acceptance-criteria checkbox in the task markdown checked. Failure may end with `TASK_FAILED: <reason>`.
_Avoid_: Agent exit code, progress record

**Malformed Task set**:
A discovered Task set whose task manifest or task markdown files violate the contract. This includes a task with persisted `in_progress` status: the synchronous task executor does not use that status because it could become stale after a crash. Malformed Task sets are reported in the status table and skipped during automatic selection; the task executor never spawns an agent for them.
_Avoid_: Blocked Task set

**Task state**:
The machine-local persisted record of a repository's registered Task sets, stored within its **Task storage**, in registration order with priority. Task state does not duplicate derived Task set completion.
_Avoid_: Workload state, task artifact, task manifest

**Runtime execution lock**:
A machine-local lock held while implement executes for a canonical runtime path. It prevents concurrent task execution in one checkout while allowing unrelated projects or isolated runtime worktrees to execute concurrently. Non-execution tasks commands remain available. Lock metadata records the executor PID; a dead PID is reported and replaced as a stale lock.
_Avoid_: Global task lock, project-name lock

**Status table**:
The non-interactive summary printed by `pop tasks status` after discovery refresh. Missing Task sets appear first as stale registrations, followed by Done Task sets. Remaining discovered Task sets then appear in scheduler order: descending priority with stable registration order for ties, so the user can read the active schedule top-to-bottom to understand which Ready work will be selected first. The automatically selected Ready Task set is marked explicitly. Before execution, the actual implement target is also marked; when an explicit Task set override differs from the automatic selection, the table shows both markers on their respective rows. An interactive tasks dashboard is deferred until the table workflow is exercised.
_Avoid_: Workload status table, dashboard

**Execution confirmation**:
The human gate before implement spawns an agent. Pop prints the refreshed status table with the selected Task set marked and asks for `y/n` confirmation. A drain asks once before draining its selected Task set, not before each task. An explicit `--yes` (`-y`) option bypasses the prompt for unattended use. Non-interactive execution without that option fails rather than waiting for input.
_Avoid_: HITL task, open task

**Execution exit status**:
The process result exposed by implement: `0` for completed work or a declined confirmation, `1` for execution failure, timeout, malformed target, commit failure, or a live Runtime execution lock, `2` when no runnable task exists or when a HITL gate exits without changing task state, `3` for usage, configuration, or project-resolution errors, and `130` for interruption.
_Avoid_: Task set status, agent exit code

**Status exit status**:
The process result exposed by `pop tasks status`. Rendering succeeds even when rows are Malformed, Failed, or Blocked; non-zero is reserved for failures that prevent resolution or rendering.
_Avoid_: Execution exit status

**Task identifier**:
The canonical name of a Task set — its directory name under the **Task storage** `tasks/` directory — or a task-manifest task ID. These identifiers drive scheduling, state, and display.
_Avoid_: Display title, filename, path

**Task target reference**:
An argument that identifies a Task set or task markdown file on Implement, Open task, Complete task, or Skip. Implement accepts an optional positional argument; Open task, Complete task, and Skip require one. Exactly two forms exist: a bare Task set identifier targets a Task set, and a Task-set-relative file reference `<task-set>/<file>.md` targets one task. Resolution is scoped to the current repository's Task storage via **Repository identity** from the CWD. Relative paths, absolute paths, bare filenames, bare task identifiers, titles, prefixes, fuzzy matches, and unresolved references are rejected.
_Avoid_: Workload target reference, shell completion candidate, path

**Task shell completion**:
Read-only shell tab completion for tasks subcommands, project names, **Task target references**, agent presets, and path flags. Positional completion on Implement offers bare Task set identifiers and also completes Task-set-relative task files after a Task set identifier and slash, such as `<task-set>/<file>.md`. Open task, Complete task, and Skip complete Task-set-relative file references only. Set-priority completes bare Task set identifiers for its TASK_SET positional. Completion never offers filesystem path segments. Completion may scan Task storage but must not auto-register Task sets, persist task state, or print warnings.
_Avoid_: Shell autosuggestion, discovery refresh

**Missing Task set**:
A locally registered Task set whose manifest is no longer present beneath its Task storage. Its registration, priority, and list order are preserved in case the Task set returns. It is skipped during execution and shown before all discovered Task sets in the status table so active work remains grouped toward the end for a future terminal UI.
_Avoid_: Malformed Task set

### Releases

**Release**:
A published build of pop identified by a CalVer tag — `vYYYY.M.N`, where N is a release counter that resets each month; the version displays without the `v` prefix. The version records when the release happened, never compatibility: breaking changes are communicated through deprecation warnings and beta-tester sign-off, not version bumps. A Release ships prebuilt binaries; the homebrew tap points at the latest one.
_Avoid_: Major/minor version, semver, compatibility promise

**Dev build**:
A pop binary whose version is not exactly a release tag. It identifies itself tag-relative — latest Release tag plus commits-since and short SHA, with a dirty marker (`v2026.6.0-5-gabc123-dirty`); before any tag exists, the bare SHA. A Dev build never shows the **Update notice**.
_Avoid_: Snapshot, pre-release, bare commit SHA

**Update check**:
Determining whether a newer **Release** exists than the running binary. Pickers refresh this in the background at most once a day and render only from the cached result — never a network wait in the picker path. **Doctor** performs the check live on every run. Disabling the Update notice also disables the automatic background check, so an explicit Doctor run becomes the only check.
_Avoid_: Phone home, telemetry, blocking version lookup

**Update notice**:
The dimmed top-right indication in a picker that a newer **Release** exists — surfaced at most once per calendar day across all pickers, suppressed for **Dev builds**, and disabled via config. In **Doctor**, version freshness is a header line only; an outdated binary never affects any family's **Doctor status**, and a failed check is a dim note, not a failure.
_Avoid_: Upgrade nag, degraded status, update row

#### Reserved terms

**Queue** (reserved, not implemented):
Reserved for a future machine-global scheduler that picks the next Task set across all projects by priority and runs it. Do not use "queue" for today's per-repository scheduling; it has no current definition.

## Deprecated aliases

Removal of all deprecated aliases is gated on beta-tester sign-off, not a version number (inventory and checklist in CLEANUP.md).

- `idle`, `read` → **Clear**
- `needs_attention` → **Unread**
- `issue` → **Task**; `Issue set` → **Task set**
- `pop workload` (command family) → **`pop tasks`**; the umbrella term "workload" is retired — say "the repository's Task sets" or name the specific concept
- `run-issue`, `run-issues` → **Implement** (`pop tasks implement`); the one-task and whole-set verbs merged into one command that dispatches by target shape
- `reset-issue` → **Open task** (`pop tasks open`); `complete-issue` → **Complete task**; `skip-issue` → **Skip**
- `to-issues` (skill) → **to-tasks**; `run-one` (skill) → **run-task**
- `workload definition path`, `thoughts/issues` → **Task storage**
- `workload artifact ignore coverage` → removed; Task storage lives outside the repository tree (ADR 0012)

## Flagged ambiguities

**Dashboard vs monitor** — **Monitor** maintains the monitored set; **Dashboard** presents it. Code uses both names loosely (`monitor` package, `dashboard` command); use domain terms when writing docs or discussing behavior.

**Visit vs status change** — A **Visit** records interaction with a pane without changing its status. Changing a pane to **Clear** records that no attention is required. Some navigation actions intentionally do both.

**Active vs working** — An **Active pane** is currently visible to the user. A **Working** pane has an agent or process actively running. A pane may be either, both, or neither.

**Open as status vs override** — `open` is both a task status and the override command that returns a task to that status. The command is deliberately named for its target status; context (noun vs verb) disambiguates.

**Session name derivation trade-off** — `project.SessionName` is the single source of truth for exact session names (bare-repo worktrees, regular repos, non-git paths). It calls git commands and is correct for all entry points that create, attach to, or kill sessions. The **dashboard** deliberately uses `project.FastSessionName` for history matching because exact derivation is too slow for a frequently-opened popup. `FastSessionName` is a pure string approximation (directory base + tmux-safe sanitization); it is identical for regular repos and non-git paths, and only differs for bare-repo worktrees where the exact name is `repo/worktree`. See ADR 0005.

## Example dialogue

> **Dev:** I picked a worktree in the project picker — did I select a project or a worktree?
>
> **Expert:** Both, in a sense. A **worktree** is also a **project** — it's a directory pop knows about. The **worktree picker** is different: that's for git operations inside the repo you're already in.
>
> **Dev:** What happens after I select it?
>
> **Expert:** Pop attaches to or creates a **session** for that project. The path goes into **history** for recency sorting next time.
>
> **Dev:** My Claude pane finished — the integration marked it Unread. Do I visit it or clear it?
>
> **Expert:** Different things. **Unread** is the status — something needs your attention. A **visit** records that you interacted with the pane without changing its status. When you switch to it on the **dashboard**, that typically also clears it to **Clear**.
>
> **Dev:** Is the dashboard the same as the monitor?
>
> **Expert:** No. The **monitor** tracks pane status in the background — that's what the **integration** talks to. The **dashboard** is just the view over that monitored set. An **agentic pane** self-registers with the monitor; you browse it on the dashboard.
>
> **Dev:** I saw a `!` in the project picker and pressed `→`. Is that the dashboard?
>
> **Expert:** No. The old **unread view** was removed. Open the **dashboard** to browse registered panes and their attention state.
>
> **Dev:** I want to keep an eye on one agent even when it's Clear.
>
> **Expert:** **Following** on the dashboard. Toggle follow on the pane, then use following mode to filter to just followed panes.
>
> **Dev:** What if a task agent changes its structured output and pop cannot interpret it?
>
> **Expert:** Its **Agent output handling** falls back to the original text, which still has to satisfy the normal **Completion sentinel** contract. An **Agent quota pause** is different: when the adapter recognizes one, the task stays Open and **implement** stops cleanly.
