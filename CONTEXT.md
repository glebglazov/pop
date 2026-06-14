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

**Topic**:
A short, agent-derived phrase describing the subject currently under discussion in an **Agentic pane** (e.g. "debugging auth middleware"). Distinct from a **Note**, which the user authors by hand: a Topic is machine-guessed and overwritten as the conversation moves. It is displayed, dimmed, in the parenthetical slot only when no Note is set, and lives for the pane's whole monitored lifetime — cleared only on retirement, never by `unfollow`.
_Avoid_: Note (user-authored), Label (process identity), summary, title

**Note**:
A short annotation the **user** types for a pane in the dashboard. Human intent; outranks a **Topic** in display and is cleared by `unfollow`.
_Avoid_: Topic (agent-derived), label

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
The fuzzy-search picker in `pop worktree` for choosing or deleting git worktrees in the current repository. Interactive picker creation remains out of scope for ordinary worktree navigation, but queue worktree parallelism is the explicit exception where pop owns `git worktree add` for a **Worktree-ready project**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone (killing it stays an explicit, separate action).
_Avoid_: Repo picker

**Dashboard picker**:
A selection-only Dashboard mode for choosing a tracked **Pane** and returning it to a caller without switching tmux focus or applying visit-like side effects. Its broad candidate set is the same tracked pane set shown by the **Dashboard**; message-sending callers narrow it to **Session-local panes** by default rather than inferring agentic panes. In picker mode, one candidate is selected without opening the TUI, while zero candidates exits unsuccessfully without output.
_Avoid_: Agent picker, monitor picker

**Session-local pane**:
A tracked **Pane** whose tmux session matches the session of the current tmux pane. Session-locality is a Dashboard filtering concern for targeted write actions; picker candidates exclude the current pane itself and do not imply the pane is agentic.
_Avoid_: Relevant pane, current pane's agent

**Pane ID target**:
A raw tmux pane identifier used as an explicit command target, such as `%63`. A Pane ID target is global within tmux and bypasses Pop's name-based agent-window lookup.
_Avoid_: Pane name, dashboard label

**Quick selection**:
A numeric shortcut for selecting a visible picker row relative to the cursor. Project and worktree pickers already expose quick selection; the **Dashboard picker** uses the same idea for fast target choice.
_Avoid_: Quick filter, fuzzy search

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

**Task set export**:
A portable `tar.gz` archive of one **Task set**'s on-disk directory — manifest, task markdown, **Progress record**, **Captured attempt stream**s, and any other sibling **Task artifact**s — produced for transfer to another machine or repository. The archive has a single top-level directory named for the set's **Task identifier**, mirroring its layout under **Task storage**. **`pop tasks export`** takes a bare **Task set identifier** only — not a **Task target reference** with a task file. Export writes `<task-set-id>.tar.gz` in the current working directory by default; the output path may be overridden. On success it prints the absolute path of the written archive. It resolves the source set from the current repository's **Task storage** via the same **Task project resolution** as other tasks commands. Any on-disk set may be exported regardless of derived **Task set status** — export is a filesystem snapshot, not a status gate. A **Missing** set fails naturally because its directory is absent. It is a faithful snapshot of the set's artifacts, not a curated planning-only subset and not the repository's **Task state** (registration order, priority).
_Avoid_: Backup, sync, bundle

**Task set import**:
Installing a **Task set export** into the current repository's **Task storage** `tasks/` directory, resolved via the same **Task project resolution** as other tasks commands. Import accepts a `tar.gz` path and requires strict archive shape: exactly one top-level directory, no path traversal, no absolute paths — hand-rolled or ambiguous archives are rejected before install. Import extracts to a temporary location, validates the set against the task contract, and only then installs it under the chosen **Task identifier** — a **Malformed** export is rejected with errors and nothing is written. By default the identifier is the archive's top-level directory name; when that identifier already exists, import is rejected and the existing set is left untouched — never merged or overwritten. **`--as <id>`** may supply a different identifier; when `<id>` has no chronological prefix (`YYYY-MM-DD` or `YYYY-MM-DD-HHMM`), pop prepends today's local date before installing, and if that dated identifier still collides it retries with the current local time as `YYYY-MM-DD-HHMM-<slug>` — the same disambiguation rule as task-set creation. On success it **registers** the set in **Task state** with priority `0`, appended after existing registrations — the same defaults as auto-discovery, and prints the absolute path to the installed set directory (the path **Show path** would report for that identifier).
_Avoid_: Legacy migration, Task set registration (when you mean the automatic discovery path only)

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
A headless agent the task executor recognizes — `claude`, `opencode`, `cursor`, `codex`, or `pi` — selected by name and optionally augmented with extra invocation arguments (e.g. `claude --model opus4.8`). Pop runs the supplied command as given, exactly as it runs a **Custom agent command**; the sole difference is recognition. Because the first token names a known agent, the **Agent adapter** appends the flags Pop owns — the output protocol governed by **Agent output mode** — after the user's arguments, then the generated prompt as the final positional argument with stdin disconnected. Appending last makes those flags authoritative: a user value for an owned flag is overridden, not rejected. Recognition is what lets Pop parse the structured stream and keep every adapter capability; augmenting a recognized preset this way is distinct from replacing the invocation with a Custom agent command.
_Avoid_: Integration

**Custom agent command**:
A trusted, opaque command supplied via `--agent-cmd` that Pop runs verbatim through a shell, with the generated prompt appended as the final positional argument. Pop neither recognizes nor inspects it, so it forgoes every adapter capability: plain output only, no **Agent quota detection**, no live rendering, and no entry in the **Captured attempt stream**. It governs only unattended task attempts, never attended HITL assistance. It replaces the invocation wholesale — the inverse of an augmented **Agent preset**.
_Avoid_: Override command, escape-hatch agent, agent passthrough

**Task agent**:
An optional per-task `agent` key in the **Manifest**, carrying an **Agent preset**-shaped value (e.g. `claude --model opus4.8`) so a planner can pick the agent and model for an individual task. It must name a recognized preset — an unknown first token is a contract fault that makes the Task set **Malformed**, and the opaque **Custom agent command** form is not allowed in a Manifest, since a durable definition must stay recognizable and replayable. The agent for a task attempt resolves by precedence: an explicit `--agent-cmd` wins, then an explicitly passed `--agent`, then the task's own `agent` key, then the default. A bare defaulted `--agent` never overrides a task key.
_Avoid_: Per-set agent, agent override

**Curated model aliases**:
A short, hand-maintained list of model aliases Pop ships for each recognized **Agent preset**, surfaced as a column in the recognized-agent catalog (`pop tasks agents`), recommended value first. It is a _suggestion_ surface to help a planner fill a **Task agent**'s `--model` — never exhaustive, and never a validation gate: a `--model` value absent from the list still runs. Only `claude`'s entries are stable auto-resolving aliases; other presets list pinned version IDs that need maintenance as models change. Pop ships a curated subset rather than a live listing because an exhaustive provider dump defeats picking.
_Avoid_: model source, live model listing, model provenance

**Interactive agent preset**:
A named attended-assistance command known to an Agent adapter. It is separate from an Agent preset because assisting a human at a HITL gate is an attended conversation, not a headless task attempt; custom headless agent commands do not imply an interactive preset. Every supported preset launches its own interactive binary, so when an **Agent preset** carries extra arguments, those arguments ride into that preset's own attended assistance.
_Avoid_: Agent preset, stripped headless command, agent-cmd

**Agent adapter**:
The preset-specific bridge between Pop and a supported agent. An adapter may provide headless invocation, headless output handling, agent-assistance invocation, and a **Model source**; attended assistance launches the preset's own interactive binary and is owned by the adapter rather than the HITL gate prompt. An adapter reports assistance Unavailable only when it has no usable interactive command at all (e.g. custom headless `--agent-cmd`).
_Avoid_: Universal JSON protocol, agent integration

**Agent catalog**:
The readout of `pop tasks agents`: every recognized **Agent preset** with its binary, whether that binary is on PATH, which preset is the default, and notes such as attended-assistance availability. It reports what Pop owns — recognition and availability — by PATH lookup only; it never execs agents by default, and authentication or deeper health stays with **Doctor**. Its audience is a planner choosing a **Task agent** as much as a human. Model details come from each preset's **Model source**, surfaced only on request.
_Avoid_: Supported agents matrix, doctor, model catalog

**Model source**:
An Agent adapter's answer to "which models can this preset's `--model` take", with three honesty levels and the provenance always shown: live enumeration by the agent's own listing command (e.g. `opencode models`), baked known-stable aliases when no listing exists (e.g. claude's `opus`, `sonnet`, `haiku`), or empty. Empty is honest — Pop never invents a model catalog for an agent, and a planner unsure of a model omits it, since a bare preset is always valid. Live listings run only when explicitly requested from the **Agent catalog**, never during its default render. A user-config layer of curated models is deferred until a real need appears.
_Avoid_: Model catalog, model registry, supported models

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

**Captured attempt stream**:
The timestamped raw agent stream recorded for one Task attempt and kept among **Task artifacts**, accumulating across attempts over a task's lifetime. It is captured only for structured adapter-mode attempts; plain-output and custom-command attempts are not recorded. It is the durable substrate from which an Attempt timing breakdown is derived, and is distinct from the ephemeral in-memory capture used for completion assessment and Agent quota detection.
_Avoid_: Agent output log, progress record, transcript

**Requested agent**:
The full resolved **Agent preset** string — preset name plus extra invocation arguments, e.g. `claude --model opus4.8` — that Pop invoked for a Task attempt. Pop always knows it at invocation time, so it is recorded verbatim in the Captured attempt stream's header and printed when the attempt starts. It states what was asked for, not what ran; the model an agent actually used is the separate **Actual model**.
_Avoid_: Agent name, preset name, model

**Actual model**:
The model identifier an agent itself reported inside its Captured attempt stream (e.g. Claude's `init` event). It is a derived, per-adapter, best-effort reading at display time — never recorded as a separate event — and is absent when the agent does not report one. It may differ from the model requested in the **Requested agent** arguments through aliases or provider fallbacks. Surfaced in the Attempt timing breakdown and shown once by the live renderer when the agent reports it mid-attempt; `pop tasks status` shows at most the manifest's **Task agent** and never reads streams.
_Avoid_: Model time, requested model, agent

**Attempt timing breakdown**:
The agent-specific accounting of where a Task attempt's wall-clock time went, derived from its Captured attempt stream: each attempt's outcome and total duration, and — for agents whose stream pairs a tool invocation with its result — a per-tool count and duration, followed by **Model time**. Tool figures are reported under the agent that ran the attempt because tool vocabularies differ by agent. Implement prints the breakdown for a task as the task finishes, showing the attempts made in that invocation; `pop tasks timings` reprints the full per-task history, ordered by attempt start time. There is no cross-Task-set rollup.
_Avoid_: Workload report, run summary, set rollup

**Model time**:
The portion of a Task attempt's total duration during which no tool was in flight — the agent itself producing output: reasoning, narration, composing edits. It is the residual after removing every interval covered by a tool invocation awaiting its result, so overlapping (parallel) tool calls are not double-counted, and a tool still running when the attempt ends counts as tool time, not Model time. It appears in the Attempt timing breakdown only when per-tool figures do, labeled `model`. It is a derived reading of the Captured attempt stream, not a recorded event.
_Avoid_: Thinking time, unattributed time, idle time, overhead

**Stream entry timing**:
The elapsed time since the previous live line, shown as a `+Xs` prefix on each rendered stream entry while implement runs. It is part of the cosmetic live side-channel and never feeds completion assessment.
_Avoid_: Tool duration, attempt timing breakdown

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
A machine-local planning document, task markdown file, task manifest, progress record, or captured attempt stream within **Task storage**. Task artifacts live outside the repository tree, so they can never enter implementation commits and require no ignore configuration.
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
Explicitly returning Failed or Skipped tasks to Open via `pop tasks open` so they may be attempted again — the command is named for the target status. It accepts either a Task-set-relative file reference `<task-set>/<file>.md`, which opens exactly that one task, or a whole-set form (`<task-set>` or `<task-set>/`), which opens a **Multi-task selection** of the set's Failed and Skipped tasks. It removes any recorded attempt count, appends a local progress entry, preserves runtime files, and does not commit. Open task batches need no ordering — each transition is independent. The status table prints copy-paste open hints in the `<task-set>/<file>.md` form.
_Avoid_: Issue reset, reset, automatic retry

**Complete task**:
Manually marking Open, Failed, or Skipped tasks Done via `pop tasks complete` without running an agent, regardless of task type. Used primarily to clear a human-in-the-loop task after the human performs the work, to conclude a Skipped task once its deferred verification is satisfied, and also valid for finishing AFK or Failed tasks by hand. The command accepts either a Task-set-relative file reference `<task-set>/<file>.md`, which completes exactly that one task, or a whole-set form (`<task-set>` or `<task-set>/`), which opens a **Multi-task selection** of the set's non-Done tasks. Every selected task's `blocked_by` dependencies must be satisfied — already Done/Skipped or also selected in the same batch — so a fully selected chain completes in dependency order; an unsatisfied, unselected blocker rejects the whole batch before any write. It bypasses the Completion sentinel — it does not verify acceptance criteria, does not prompt for a separate yes/no confirmation (the selection itself is the decision), and does not stage or commit implementation changes; the human owns and commits that work. It appends a local COMPLETE progress record per task noting the prior state.
_Avoid_: Complete issue, completion sentinel, no-op task completion, run

**HITL gate completion**:
Completing the blocking HITL task from a HITL gate prompt after explicit confirmation. It uses the same state transition as Complete task, then implement continues draining the Task set instead of stopping at the cleared gate.
_Avoid_: Completion sentinel, automatic HITL execution

**HITL gate deferral**:
Skipping the blocking HITL task from a HITL gate prompt after explicit confirmation. It uses the same state transition as Skipped task, then implement continues draining the Task set because a Skipped task satisfies dependent `blocked_by` prerequisites.
_Avoid_: Failed task, automatic HITL execution

**Skipped task**:
A task the human deliberately set aside via `pop tasks skip`, recorded with the `skipped` status. Skipping accepts only Open tasks of any type and is the deadlock breaker when a human-in-the-loop task cannot be verified until its own follow-up tasks complete. A Skipped task is never selected for execution, yet — unlike an Open dependency — it satisfies `blocked_by` for its dependents, so downstream tasks become eligible against a deliberately deferred, not completed, prerequisite. The command mirrors **Open task** targeting: a `<task-set>/<file>.md` reference skips one task, and a whole-set form opens a **Multi-task selection** of the set's Open tasks; batches need no ordering. It appends a local SKIP progress record per task. A Skipped task later resolves through Complete task (to Done) or Open task (to Open).
_Avoid_: Skipped issue, exhausted task, interrupted task, blocked task

**Multi-task selection**:
The interactive checkbox UI that Open task, Complete task, and Skip open when given a whole-set target (`<task-set>` or `<task-set>/`) instead of a file reference. It lists every task in the set in manifest order and splits rows three ways: rows the verb can move are **actionable** (toggleable checkboxes, cursor starts on the first one); rows already at the verb's target state show a locked status mark and cannot be toggled; rows the verb cannot touch are shown as inert locked context. The mark on a locked at-target row is a status indicator, not a removable selection. Confirming (Enter) applies the checked actionable rows as one atomic batch; cancelling (Esc) writes nothing. It is a terminal-only affordance — a whole-set target with no interactive TTY is rejected with a pointer to the `<task-set>/<file>.md` form rather than mutating many tasks silently. It shares the underlying state transitions and progress records of the single-task path; it only changes how many tasks are chosen at once.
_Avoid_: Project picker, checkbox (acceptance-criteria sense), HITL gate prompt, `--all`

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
The machine-local persisted record of a repository's registered Task sets, stored within its **Task storage**, in registration order with priority and an `archived` flag per set. Task state does not duplicate derived Task set completion; priority and the archived flag are deliberate registration metadata, not derived status.
_Avoid_: Workload state, task artifact, task manifest

**Runtime execution lock**:
A machine-local lock held while implement executes for a canonical runtime path. It prevents concurrent task execution in one checkout while allowing unrelated projects or isolated runtime worktrees to execute concurrently. Non-execution tasks commands remain available. Lock metadata records the executor PID; a dead PID is reported and replaced as a stale lock.
_Avoid_: Global task lock, project-name lock

**Status table**:
The non-interactive summary printed by `pop tasks status` after discovery refresh. **Archived Task set**s are excluded from the default table; when at least one exists, a quiet footer reports the archived count and the `pop tasks status --archived` command that lists them, so filed-away work stays discoverable. `--archived` instead renders only the Archived Task sets. In the default table, Missing Task sets appear first as stale registrations, followed by Done Task sets. Remaining discovered Task sets then appear in scheduler order: descending priority with stable registration order for ties, so the user can read the active schedule top-to-bottom to understand which Ready work will be selected first. The automatically selected Ready Task set is marked explicitly. Before execution, the actual implement target is also marked; when an explicit Task set override differs from the automatic selection, the table shows both markers on their respective rows. An interactive tasks dashboard is deferred until the table workflow is exercised.
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
An argument that identifies a Task set or task markdown file on Implement, Open task, Complete task, or Skip. Implement accepts an optional positional argument; Open task, Complete task, and Skip accept one optionally — when omitted on those override verbs the argument is required, but a bare Task set identifier is now a valid form for them too (it opens a multi-task selection rather than being rejected). Three input forms map to two semantics: a bare Task set identifier `<task-set>` and its trailing-slash equivalent `<task-set>/` both target the whole Task set, and a Task-set-relative file reference `<task-set>/<file>.md` targets one task. The trailing slash is tolerated so shell completion can drill from set to file without the operator deleting a separator; `<task-set>/` resolves identically to `<task-set>`. Resolution is scoped to the current repository's Task storage via **Repository identity** from the CWD. Relative paths, absolute paths, bare filenames, bare task identifiers, titles, prefixes, fuzzy matches, and unresolved references are rejected.
_Avoid_: Workload target reference, shell completion candidate, path

**Task shell completion**:
Read-only shell tab completion for tasks subcommands, project names, **Task target references**, agent presets, and path flags. Positional completion on Implement, Open task, Complete task, and Skip behaves uniformly, and never offers a done thing: at the set-identifier stage it offers each non-Done set as `<task-set>/` with a trailing slash and a no-space directive, so resolving one set leaves the cursor right after the slash to continue tabbing into Task-set-relative files `<task-set>/<file>.md`; the `<task-set>/` form is itself a valid whole-set target, so the operator may stop there. Done Task sets are omitted at the set stage and done tasks at the file stage, because neither is actionable by any of the four verbs; Deferred, Malformed, and every other set stays offered, and explicitly typing a done target still resolves — the filter narrows completion, not resolution. Timings completes the unfiltered target list, since Done sets are exactly what timings inspects. Set-priority, show-path, and **Task set export** complete bare Task set identifiers only (no file stage); export offers every on-disk set regardless of status. **Task set import** has no positional completion — the archive path is a filesystem argument outside this model. **Archived Task set**s are omitted from every completion surface except **Unarchive**, whose positional completion offers only Archived Task set identifiers; explicitly typing an archived identifier still resolves for the snapshot verbs that accept it, the same way the filter narrows completion rather than resolution for done targets. Completion never offers filesystem path segments. Completion may scan Task storage but must not auto-register Task sets, persist task state, or print warnings.
_Avoid_: Shell autosuggestion, discovery refresh

**Missing Task set**:
A locally registered Task set whose manifest is no longer present beneath its Task storage. Its registration, priority, and list order are preserved in case the Task set returns. It is skipped during execution and shown before all discovered Task sets in the status table so active work remains grouped toward the end for a future terminal UI.
_Avoid_: Malformed Task set

**Archived Task set**:
A registered Task set the human has filed away with **Archive**, recorded by an `archived` flag on its **Task state** registration entry. An Archived Task set is hidden from the **Status table**, from automatic selection and draining, and from every **Task shell completion** surface except **Unarchive**; its task markdown, **Task manifest**, **Progress record**, **Captured attempt stream**s, and task statuses are untouched, so archiving is non-destructive and fully reversible. Archiving is a registration-metadata decision like **Task set priority** — not a derived **Task set status** and not a task-status transition — so it appends no **Progress record**. An action verb (**Implement**, **Open task**, **Complete task**, **Skipped task** via `skip`, set-priority) refuses an Archived Task set target and points the human to **Unarchive** first; read-only snapshot verbs (**Task set export**, **Show path**, and `timings`) still resolve an explicitly typed archived **Task identifier** because they neither schedule nor mutate the set. `pop tasks status --archived` lists only the Archived Task sets so the human can see and reclaim what was filed away.
_Avoid_: Deleted Task set, Task set export (the tar.gz archive), Done Task set, Missing Task set

**Archive**:
The command `pop tasks archive` that files Task sets away as **Archived Task set**s. With no argument it opens a **Multi-set selection** of every non-archived registered set — Done, Deferred, Ready, Blocked, Failed, Missing, and Malformed alike — with only **Done** sets pre-checked, so the common "review the done ones and move on" pass is one confirmation. A bare **Task set identifier** archives exactly that set regardless of its **Task set status**, with no picker. `--yes` skips the picker and archives precisely the Done sets — the unattended form of the default. Like **Multi-task selection**, a no-argument run with no interactive TTY and no `--yes` is rejected rather than mass-mutating silently, pointing the human to `--yes` or a bare identifier. Archiving several sets is one atomic **Task state** write and appends no **Progress record**.
_Avoid_: Delete, Task set export, Remove registration, Skipped task

**Unarchive**:
The command `pop tasks unarchive` that restores **Archived Task set**s, clearing the `archived` flag so the set reappears in the **Status table**, automatic selection, and completion. With no argument it opens a **Multi-set selection** listing only Archived Task sets with nothing pre-checked; a bare **Task set identifier** restores exactly that set. Like **Archive** it touches only **Task state** and appends no **Progress record**.
_Avoid_: Restore from export, Task set import, Open task

**Multi-set selection**:
The interactive checkbox UI that **Archive** and **Unarchive** open across whole Task sets — the cross-set sibling of the within-set **Multi-task selection**. Each row is one registered Task set showing its **Task identifier** and derived **Task set status**; Archive pre-checks Done rows and lists every other status as unchecked-but-checkable, while Unarchive lists only Archived Task sets with none pre-checked. Confirming (Enter) applies the checked sets as one atomic **Task state** write; cancelling (Esc) writes nothing. Like Multi-task selection it is terminal-only — a no-argument invocation with no interactive TTY is rejected rather than mutating silently.
_Avoid_: Multi-task selection (within-set, task-level), Project picker, `--all`

**Queue**:
A daemon that supervises per-project Task-set draining, fanning `pop tasks implement <set>` runs out concurrently across registered projects into tmux. It targets one specific not-currently-running Ready set per idle project, never no-argument implement, which would re-pick a running set. Each project drains serially by local Task set priority, enforced by the **Runtime execution lock**; projects run in parallel. Global cross-project priority ordering is a non-goal.
_Avoid_: Machine-global scheduler, per-repository scheduler

**Picked-up Task set**:
A Task set currently being drained, identified by a live **Runtime execution lock** that records its **Task identifier**. Picked-up state is derived from lock liveness, never persisted as a task status; tmux panes are display only, not the source of truth.
_Avoid_: In-progress task, pane state

**Queue daemon**:
The supervisor process behind `pop queue run`. It is foreground and explicit, never auto-started from a picker, because it runs coding agents unattended across projects; the operator parks it in a pane and Ctrl-C (`SIGINT`) is graceful shutdown. It is single-instance via a PID/lock file. Unlike the **Monitor** daemon, it needs no control socket: it persists agent cooldowns, parked sets, backoff timers, and the **Queue journal** to disk, so `pop queue status` and `pop queue log` are pure file readers. On `run`, it reconciles in-flight drains from live **Runtime execution lock**s, so a restart never disturbs them. Its command surface is `run`, `status`, and `log`; Ctrl-C is stop.
_Avoid_: Monitor daemon, background service

**Queue scope**:
The set of work the **Queue daemon** supervises: all registered projects' Ready Task sets. Running the daemon with `pop queue run` is the standing unattended-AFK consent; there is no per-project opt-in flag. The blast radius is self-limiting because the daemon only acts on Ready sets, and a Task set is a deliberately authored artifact; a project with no sets is skipped. The per-set opt-out is **Archive**. When a project has no tmux session, the daemon creates one detached, ensures a `pop-queue` window, and spawns the drain pane there; finished panes are kept as a visible log.
_Avoid_: Per-project queue opt-in, global priority queue

**Queue journal**:
The durable append-only record in pop's data dir of every Queue drain event: started, done, failed, HITL-blocked, quota-paused-and-agent-switched, crashed, backing-off, or parked. It is emitted by **Implement** as a structured drain-outcome record carrying set id, outcome, and the exhausted preset when relevant; the **Queue daemon** consumes it to drive Queue agent fallback and backoff, and persists it for observability. `pop queue status` reads live state, such as picked-up sets, cooling agents, parked sets, and idle projects; `pop queue log` reads the journal history.
_Avoid_: Progress record, Captured attempt stream, Task state

**Queue backoff**:
The daemon's response to an abnormal drain exit, such as crash, kill, or interrupt. Unlike a clean failure or quota pause, an abnormal exit leaves the set Ready with nothing cooled and would otherwise re-spawn immediately. The daemon applies an escalating per-set delay and, after N consecutive abnormal exits, parks the set until a human clears it. A clean exit resets the counter. Distinguishing abnormal from clean exits requires the **Queue journal**'s outcome record; storage status alone cannot tell a crash from a quota pause.
_Avoid_: Failed task, Agent quota pause

**Queue agent fallback**:
The Queue's policy for choosing an **Agent preset** when draining, owned by the daemon rather than the executor. It rotates a configured ordered list of agents as a non-overriding default: **Task agent** pins always win, while unpinned tasks ride the rotating default. An agent whose binary is not on `PATH` is skipped with a **Queue journal** note, not a startup error. A global per-agent cooldown marks an agent exhausted-until after an **Agent quota pause**, because quota is per subscription, not per project. Recovery is probed by re-attempting after that fixed interval; there is no quota-remaining API to query. When a pinned task's agent is exhausted, the Queue backs that whole set off until that agent's cooldown expires rather than violating the pin.
_Avoid_: Executor agent policy, per-project quota, overriding task agent

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

**Manifest vs progress record vs captured stream (State vs Journal vs Telemetry)** — three stores answer three different questions and must not be conflated. The **Task manifest** (`index.json`) is *State*: the current, authoritative truth of each task's status, overwritten on refresh, holding no history. The **Progress record** (`progress.txt`) is the *Journal*: an append-only, terminal-grain history of distilled outcomes (Done/Failed/manual Complete/Open/Skip) plus the agent summary, written by agent completions and human overrides alike, read by humans and the HITL assistance prompt. The **Captured attempt stream** (`streams/…`) is *Telemetry*: the per-attempt raw transcript, recorded for structured attempts only, the substrate timings and any retry carry-forward derive from. Test — manifest: is-it-true-now (lookup); journal: what-happened-in-order (distilled, per terminal transition); stream: how-one-attempt-unfolded (raw, per attempt). The one deliberate overlap — "why did it fail" sits in both the journal's Failed line and the stream footer's reason — is owned by the stream footer as the durable signal (ADR 0020); the journal line is the human echo. Consequence: a failed approach is recoverable only from the stream — the journal records a Failed task as a single outcome line, never the approach that failed.

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
