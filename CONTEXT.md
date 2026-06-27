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
A normalized lowercase kebab slug (≤5 words, e.g. `debugging-auth-middleware`) naming the subject of an **Agentic pane**. It is single-sourced as a per-pane tmux property, so any tmux surface — not just pop's dashboard — can display it, and it is reusable in custom tmux labels. pop derives it once per pane via **Topic recipe**s and normalizes the result; a **Note** still outranks it in display.
_Avoid_: Note (user-authored), summarization, title, pane name, label, summary

**Topic recipe**:
A pop-curated invocation of an agent CLI (local or remote) that pop runs to derive a **Topic**. pop tries configured recipes in order and uses the first non-empty result, so a failed or rate-limited agent falls through to the next. pop owns the recipes, prompt, and output normalization but links no model SDK and holds no API keys — auth lives in the CLIs.
_Avoid_: topic command, topic model

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
An individually-installable unit `pop integrate` lands for one agent: the status wiring (core), the **Pane skill**, or the **Task planning skills**. `pop integrate <agent>` installs them all by default; each non-core component can be declined with a `--no-<component>` flag, and the decline is persisted (see **Component opt-out**).
_Avoid_: Bundle, default install

**Integration refresh**:
Reconciling installed **Integration components** to the state pop now expects: it re-renders by resolved name (not just content), so **Skills prefix** or base-name changes are applied and stale old-named entries pruned; it installs any baseline-listed component that is missing and not opted-out; and it leaves uninstalled agents alone. Runs on the binary-revision-gated picker-launch path and on `pop integrate --update-existing`. Never prompts; never re-adds or updates an opted-out component.
_Avoid_: Auto-install, update prompt

**Doctor**:
The readiness report opened by `pop doctor`: a top-level command-family view of whether Pop's user-facing workflows can run on this machine. Its canonical first-pass families are `pop project`, `pop worktree`, `pop monitor`, `pop pane`, `pop tasks`, and `pop integrate`; hidden or deprecated aliases are not top-level families. Doctor drills into subcommands or agent-specific integration state only where they explain a degraded or unavailable workflow. Read-only; it never installs or repairs.
_Avoid_: Integrate status, health subcommand

**Doctor status**:
The aggregate readiness state for one command family in **Doctor**. OK means the family's core workflow should run; Partial means the family is available through some configured variants but unavailable through others, most commonly agent-specific support or setup; Degraded means the core workflow can run but a relevant optional capability is missing, stale, conflicting, or otherwise limited; Blocked means the core workflow cannot run; N/A means the family intentionally does not apply in the current environment. Partial, Degraded, and Blocked statuses must name the concrete reason.
_Avoid_: Health score, severity level

**Doctor rendering**:
The terminal presentation of **Doctor**. Integrate-family sub-checks for file-based components name the resolved install name (same as **Integrate outcome line**), not the **Integration component id**; status wiring checks stay at component level (`<agent> status-wiring`). Otherwise unchanged: scannable ANSI, stable ASCII/Unicode-safe status labels rather than emoji, reliable alignment in plain terminals/logs/CI, one row per top-level command family with terse assessment checks printed directly beneath.
_Avoid_: Emoji health report, decorative symbols

**Doctor intent**:
The set of variants Doctor has reason to evaluate for a command family. Doctor reports missing or broken agent-specific setup only for agents the user appears to use through Pop configuration, installed Pop artifacts, or an explicit command context; unsupported but unused agents are suggestions, not reasons for Partial or Degraded status. Agent intent is inferred first from task execution configuration, then from Pop-owned integration artifacts or Pop hooks/extensions already present, and only then from explicit command context. Merely having an agent executable installed is a suggestion, not intent.
_Avoid_: Supported agents matrix, all possible integrations

**Integration conflict**:
A skill already present at an embedded skill's resolved install name (see **Skills prefix**) that pop does not recognise as its own (see **Pop-owned marker**). Pop never installs over, removes, or refreshes a conflicting skill; integrate and the health check report the conflict and leave resolution to the user.
_Avoid_: Stale integration, collision overwrite

**Pane skill**:
The embedded skill that teaches an agent to drive `pop pane`. Installed via the **Integration component id** `pane-skills` (one resolved skill, typically `tmux-pane` when **Skills prefix** is empty). Still selected in config via the **Integration skill alias** `pane`. An opt-in **Integration component**; pane monitoring works without it.
_Avoid_: Agent integration, hooks

**Task planning skills**:
The embedded, pop-independent skills (grill-with-docs, to-prd, to-tasks) whose output feeds Task sets. Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed. grill-consolidate also ships embedded, but is a glossary-maintenance pass that folds CONTEXT fragments into the base — not a Task-set producer.
_Avoid_: Workload framework, workload skills bundle, agent integration

**Skills prefix**:
The configurable string prepended to an embedded skill's base name to form its installed name (`<prefix><base>`). Set via `skills_prefix` in `[integrations]`, default `pop-`; an empty value installs skills under their bare base name.
_Avoid_: skill_prefix, pop- prefix, namespace

**Pop-owned marker**:
How pop recognises an installed artifact as its own, independent of the skill's name: a symlink resolving into pop's render tree, or — for copy-mode installs — a `pop-owned: true` frontmatter field written into every rendered skill. The legacy `pop-` name-prefix ownership check is retired; the **Skills prefix** can be empty without losing ownership detection for newly rendered skills.
_Avoid_: ownership convention, pop- name check

**Integration skill alias**:
The short name for an optional **Integration component** in the merged `skills` config array: `"pane"` → pane skill, `"tasks"` → task planning skills. Config and **Integration runtime config** use aliases; reasoned integrate output and `--no-*` flags use **Integration component id**s (`pane-skills`, `task-skills`). Unknown aliases are a config error.
_Avoid_: component shorthand, skill name

**Integration component id**:
The stable slug naming one **Integration component** in pop's machine-facing contract: `status-wiring`, `pane-skills`, `task-skills`. Used for CLI flags (`--no-pane-skills` only — the old `--no-pane-skill` flag is not accepted), render-tree directory names under `$XDG_DATA_HOME/pop/integrations/<agent>/`, **Doctor** evidence keys, and catalog lookup — not for individual installed skill names (`tmux-pane`, `grill-with-docs`, …). Skill-bundle components use plural ids; status wiring stays singular because it is hooks/plumbing, not a skill set.
_Avoid_: component slug-per-skill

**Integration baseline**:
The global `skills` array of **Integration skill alias** values declaring which optional **Integration components** pop may install (e.g. `["tasks", "pane"]`). Pop ships embedded defaults; user declares intent in `config.toml`; CLI mutations land in **Integration runtime config**. Resolved by **Config merge order**. Status wiring is never listed. The baseline is a contract: pop must install every listed component on every integrated agent once each **Agent install path** exists.
_Avoid_: default skills, integration policy

**Integration runtime config**:
The middle layer in pop's three-layer config merge: `$XDG_DATA_HOME/pop/config.runtime.toml`, written by integrate commands (`--no-*` shrinks `skills`; **Bare integrate** clears this file's overrides). Pop embedded defaults load first; user `~/.config/pop/config.toml` wins last. Integrate reads the merged result — no separate preference store.
_Avoid_: runtime settings, persisted opt-out json, integrations.toml

**Config merge order**:
How pop resolves effective configuration: embedded pop defaults, then **Integration runtime config** (`config.runtime.toml`), then user `config.toml` — each layer overrides the previous for the fields it sets. The merge mechanism is global (all config keys); v1 only integrate writes the runtime file, for `[integrations]` only. Integrate, refresh, and **Doctor** consume the merged config.
_Avoid_: three-layer integrate-only merge

**Component opt-out**:
Declining an optional **Integration component** by removing it from the global `skills` list in **Integration runtime config** (the middle config layer). Set by `--no-<component>` or `pop integrate remove`; cleared when bare `pop integrate <agent>` drops the runtime override and the merged config re-inherits pop defaults. **Integration baseline** in user config outranks runtime — editing `skills` there solidifies the set. Opt-out is global: declining pane applies to every agent, not one.
_Avoid_: negative consent, decline list

**Bare integrate**:
`pop integrate <agent>` with no component flags: installs status wiring for the named agent(s) plus every optional component in the merged **Integration baseline**, with no prompts. Clears **Integration runtime config** overrides (restores pop defaults unless user config constrains `skills`). Re-adds globally opted-out components unless solidified in user config.
_Avoid_: wizard path, default install flags

**Agent install path**:
Where pop lands a file-based **Integration component** for one agent (e.g. claude's skills directory, opencode's flat agent file). Each agent may need a different shape (directory symlink vs single file). A component is installable for an agent only once pop implements that agent's path; until then **Doctor** reports the gap and integrate records a reasoned skip — not a degraded partial install.
_Avoid_: agent support matrix, supported agents list

**Integration conflict overwrite**:
Destroying an unowned entry that blocks a pop **Integration component** requires an explicit `--overwrite-conflicts` on integrate; plain integrate and **Integration refresh** skip and name that command. The only integrate prompt is `Overwrite <path>? [y/N]` during that flow (or `--yes` to skip it). Pop-owned reinstalls and opt-out removals never prompt.
_Avoid_: conflict prompt, overwrite wizard

**Stale agent entry cleanup**:
After integrate links a component's freshly rendered skill names at an agent location, pop removes any remaining pop-owned entries there whose names are no longer in that render set — typically leftovers from a prior **Skills prefix** or base-name change. Scoped per component; never removes unowned or foreign skills.
_Avoid_: prune stale, stale-name prune

**Integrate outcome line**:
One stdout line per successful or skipped integrate action, naming what changed. File-based **Integration components** emit one line per resolved installed skill (not one line per component bundle); status wiring stays one line per agent with no skill name. Labels (`added`, `updated`, `skipped (conflict at …)`, `skipped (opted out)`, `removed (opted out)`, etc.) attach to that named unit — same per-skill granularity for skips and removals as for adds and updates. The named skill is the resolved install name — what appears at the agent's skill location after **Skills prefix** is applied — not the **Integration skill alias** or embed base alone.
_Avoid_: component outcome, integrate row

**Stale skill removal line**:
An **Integrate outcome line** emitted when **Stale agent entry cleanup** deletes a pop-owned skill whose resolved install name is no longer expected — e.g. after a **Skills prefix** change (`pop-tmux-pane` → `tmux-pane`) or **Integration component id** rename. Label: `removed (stale)`. Distinct from `removed (opted out)`.
_Avoid_: pruned line, stale prune report

**Integrate outcome ordering**:
**Integrate outcome line**s group by agent (existing configured agent order). Within an agent: status wiring first, then file-based skills in embed catalog source order (`tmux-pane`; then `grill-with-docs`, `grill-consolidate`, `to-prd`, `to-tasks`). For each embed base, emit any **Stale skill removal line** for superseded resolved names immediately before that base's current line — so `pop-grill-consolidate  removed (stale)` sits next to `grill-consolidate  updated`, not in a separate trailing block.
_Avoid_: alphabetical integrate output, sort by label

### Pickers

**Project picker**:
The fuzzy-search picker opened by the project command — for choosing a project, worktree, or standalone session.
_Avoid_: Session picker, select view, normal mode

**Worktree picker**:
The fuzzy-search picker in `pop worktree` for choosing or deleting git worktrees in the current repository. Interactive picker creation remains out of scope for ordinary worktree navigation, but queue worktree parallelism is the explicit exception where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone (killing it stays an explicit, separate action).
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
Executor drives the solid (───▶) edges; the human drives the dashed (- -▶) ones.

  open    ───────▶ done      Implement (agent success)
  open    ───────▶ failed    attempt exhaustion / timeout
  failed  - - - -▶ open      Open task
  failed  - - - -▶ done      Complete task
  open    - - - -▶ skipped   Skip task
  skipped - - - -▶ open      Open task
  skipped - - - -▶ done      Complete task
  done    - - - -▶ open      Open task   (a Done task is reopenable)
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
no open AFK work, open HITL remains ......... UNVERIFIED ← Human verification remains
otherwise (unfinished, none eligible) ....... BLOCKED    ← Human-blocked: HITL or undone dependency
```

(`MISSING` and `MALFORMED` sit outside this derivation — they are registration and contract faults.) Automatic selection runs READY sets in scheduler order and passes over DONE, DEFERRED, and UNVERIFIED sets; only when no READY set exists may a no-argument implement select a single unambiguous Human-blocked Task set for attended help, and only when the block is an open HITL task rather than an unresolved AFK dependency. Multiple Human-blocked Task sets are ambiguous and require an explicit target. Draining stops when its selected set reaches DONE, FAILED, BLOCKED, UNVERIFIED, or DEFERRED, or when an **Agent quota pause** interrupts draining without changing task status. At a BLOCKED HITL gate, interactive runs show a **HITL gate prompt** while non-interactive runs and `--yes` preserve stop-and-advice output and never auto-start attended assistance.

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
A portable `tar.gz` archive of one **Task set**'s on-disk directory — manifest, task markdown, **Progress record**, **Captured attempt stream**s, and any other sibling **Task artifact**s — produced for transfer to another machine or repository. The archive has a single top-level directory named for the set's **Task identifier**, mirroring its layout under **Task storage**. **`pop tasks transfer export`** takes a bare **Task set identifier** only — not a **Task target reference** with a task file. Export writes `<task-set-id>.tar.gz` in the current working directory by default; the output path may be overridden. On success it prints the absolute path of the written archive. It resolves the source set from the current repository's **Task storage** via the same **Task project resolution** as other tasks commands. Any on-disk set may be exported regardless of derived **Task set status** — export is a filesystem snapshot, not a status gate. A **Missing** set fails naturally because its directory is absent. It is a faithful snapshot of the set's artifacts, not a curated planning-only subset and not the repository's **Task state** (registration order, priority).
_Avoid_: Backup, sync, bundle

**Task set import**:
Installing a **Task set export** into the current repository's **Task storage** `tasks/` directory via **`pop tasks transfer import`**, resolved via the same **Task project resolution** as other tasks commands. Import accepts a `tar.gz` path and requires strict archive shape: exactly one top-level directory, no path traversal, no absolute paths — hand-rolled or ambiguous archives are rejected before install. Import extracts to a temporary location, validates the set against the task contract, and only then installs it under the chosen **Task identifier** — a **Malformed** export is rejected with errors and nothing is written. By default the identifier is the archive's top-level directory name; when that identifier already exists, import is rejected and the existing set is left untouched — never merged or overwritten. **`--as <id>`** may supply a different identifier; when `<id>` has no chronological prefix (`YYYY-MM-DD` or `YYYY-MM-DD-HHMM`), pop prepends today's local date before installing, and if that dated identifier still collides it retries with the current local time as `YYYY-MM-DD-HHMM-<slug>` — the same disambiguation rule as task-set creation. On success it **registers** the set in **Task state** with priority `0`, appended after existing registrations — the same defaults as auto-discovery, and prints the absolute path to the installed set directory (the path **Show path** would report for that identifier).
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
The git checkout from which task execution starts. It defaults to the selected project's path and may be overridden for a command. For a **Worktree set**, pop resolves it from the set's **Worktree binding** when one exists; otherwise the project's trunk checkout. Pop resolves it to the checkout root and uses that root for the agent working directory, dirty-tree preflight, staging, commits, and the Runtime execution lock. Task artifacts remain in the separate **Task storage**.
_Avoid_: Workload runtime path, task storage, shared git root

**Worktree set**:
A **Task set** drained in its own pop-provisioned git worktree under a **Worktree binding**. The checkout is an ephemeral execution context, not a navigable project peer: pop does not auto-create a session for it. It is still a registered git worktree, so it remains reachable on demand via the Worktree picker, which creates a session only when the human selects it.
_Avoid_: Worktree task, per-task worktree, queue shard

**Drain routing**:
Resolving which checkout a whole-set drain runs in. Precedence: an existing **Worktree binding** wins (resume in the bound checkout); otherwise an explicit Runtime-path override; otherwise the set's **Worktree directive** seeded at registration (provision a managed worktree or adopt a named one, recording a binding on the first unbound drain); otherwise a **Default binding** to the chosen checkout (the current checkout for **Implement**, the **Integration target** for the **Queue**), recorded so later drains resume there. Pop still never auto-provisions from a *repo* capability while routing — provisioning happens only from an explicit operator act or a per-set authored directive, never inferred. **Implement** and the **Queue** share this policy (one `RouteDrainCheckout`). An unsatisfiable directive (managed with no **Trunk worktree**, or a named worktree absent on this machine) is a config/registration-class error: the set is not drained, surfaced like a registration fault, with no backoff and no silent in-place fallback. Single-task file runs stay current-checkout.
_Avoid_: checkout picker, runtime resolver, workspace routing

**Integration target**:
The checkout a **Task set**'s work merges into, and the checkout the **Queue** drains an unbound set into. Derived per repository with no git: a **non-bare** repo's target is its main worktree (the parent of its git common directory); a **bare** repo's target is its **Trunk worktree** declared in config. Distinct from the execution checkout (where a drain runs): a set executes on its bound worktree but integrates into this target. A bare repo with no configured trunk has no integration target and its sets surface a config-class error.
_Avoid_: execution checkout, merge target

**Default binding**:
The **Worktree binding** a no-directive drain records to the checkout it resolved on its first run — the **current checkout** for a foreground **Implement**, the **Integration target** for a headless **Queue** drain. It makes an otherwise-unbound set sticky to where it first ran, so later drains resume the same checkout and branch. An operator **Bind worktree** or runtime override still takes precedence.
_Avoid_: implicit binding, auto-bind

**Worktree binding**:
A durable association between one **Task set** and one git checkout for that set's execution, recorded in shared per-repository drain state and owned by a provisioning module that both **`pop queue run`** and **`pop tasks implement`** call — not a **Queue**-private structure. It is the universal per-set drain router: **Drain routing** consults bindings first, then applies the remaining precedence rules. The bound checkout is either the project's trunk checkout or a worktree, and only worktree (non-trunk) bindings enter the **Integration backlog**. A binding carries a `Provisioned` bit recording who owns the checkout's teardown: **managed** (pop ran `git worktree add` under its data dir; pop tears the checkout and branch down on integration or **Unbind worktree**) versus **adopted** (a human pointed an existing, owned checkout at the set via **Bind worktree**, or a foreground **`pop tasks implement`** ran in and thereby adopted its current checkout; pop drains into it but never deletes it — unbind only forgets the association). Bindings default to adopted/never-delete, so a hand-written or unrecognized binding can never trigger a directory deletion; pop deletes only what it demonstrably created. A managed binding's checkout lives at a stable path derived from the set identifier and persists across drain exits, failures, and supervisor restarts so re-spawns resume the same branch rather than forking afresh. If a binding's checkout is missing or no longer registered with git, pop refuses to spawn and directs the human to repair git state or **Unbind worktree** — it never silently re-provisions. **Archive** does not release a binding.
_Avoid_: Runtime path override, per-spawn worktree, timestamped checkout

**Unbind worktree**:
The human act of releasing a **Worktree binding**, leaving **Task set** task statuses untouched. What it drops depends on the binding's `Provisioned` bit: a **managed** binding's checkout and branch are torn down (pop created them); an **adopted** binding is forget-only — the checkout and branch are retained and only the association is dropped. The symmetric inverse of **Bind worktree**, invoked via `pop tasks unbind-worktree` with a **Task set identifier**; interactive runs confirm for both **Failed** sets and **Done** sets awaiting integration (the prompt conveys the managed-teardown danger), and `--yes` skips confirmation. Refused while the set actively holds the **Runtime execution lock**; noop once integration already released the binding. After it runs the stable checkout slot is free; a later drain may provision a fresh binding from current trunk **HEAD**. Distinct from **Archive** (registration metadata only) and from integration (merging into trunk).
_Avoid_: abandon, abandon worktree, release worktree

**Bind worktree**:
The human act of pointing an existing, human-owned git checkout at a **Task set** so a later drain targets that checkout — `pop tasks bind-worktree <set>`, run from inside the target checkout. It creates an **adopted** **Worktree binding** (pop never deletes the checkout). Symmetric sibling of **Unbind worktree**; both mutate the shared binding store and run without the daemon. Refuses to re-point a set that is already bound elsewhere without `--force`, and never re-points a set holding a live **Runtime execution lock**.
_Avoid_: adopt worktree, claim worktree, queue bind

**Integration backlog**:
The derived set of **Task set**s whose drain landed in a worktree checkout (not trunk) and now await reconciliation into the working branch they forked from. Membership is determined by which checkout a set drained in, not by how it was triggered: a set that a bare **`pop tasks implement`** ran in a worktree is in the backlog exactly as one a **`pop queue run`** drained there is — and a set drained in trunk never enters it. It is a read-only view over non-trunk **Worktree binding**s plus their **Mergeability**, not a scheduler and not owned by the **Queue** command family. Membership is the binding's existence alone — never gated on a recorded **Mergeability**: a member whose mergeability has never been computed is still a member, shown as `unknown`, and still actionable (its integrate hint stands; **Integrate** computes mergeability when run). Listing the backlog is read-only and never computes mergeability. The dashboard and `pop queue status` both render this one source, so a Done worktree set can never be visible to one surface and invisible to the other. **Integrate** operates on the backlog regardless of trigger. Distinct from **Queue** (the per-project drain supervisor): the backlog routes integration, the Queue routes execution.
_Avoid_: merge queue, integration queue

**Mergeability**:
The clean-or-conflicts verdict from pop's no-side-effect `git merge-tree` dry run of a completed **Worktree set** branch against the working branch it forked from. Mergeability is algorithmic evidence for integration routing; it is not semantic validation and does not mean pop has integrated the branch.
_Avoid_: Merge result, integration status, semantic safety

**Dirty runtime strategy**:
Controls how task execution starts from a dirty runtime checkout. `continue` starts execution without modifying the existing dirty state; it is the default both when the option is absent and when it is present without a value, and after successful task completion the normal implementation commit intentionally includes both pre-existing and agent changes. `commit-and-continue` captures the existing dirty state in a separate implementation commit before invoking the agent. `stash-and-continue` stashes tracked and untracked changes but not ignored files, prints the stash reference when one is created, and leaves restoration to the user; an empty stash does not prevent execution. When the runtime is dirty the command always displays `git status` and the chosen strategy's effect, then requires interactive `y` confirmation; `--yes` auto-confirms, and a non-interactive run without `--yes` is rejected. Implement applies the chosen strategy once before draining its selected Task set.
_Avoid_: Clean runtime checkout requirement, automatic stash restoration

**Implementation commit**:
A commit created by the task executor from runtime-checkout changes. After successful task completion, the executor stages all runtime changes and commits them with a task-derived subject and the agent summary as body. The subject's scope names the Task set by its identifier without the timestamp prefix. Task artifacts remain local and unstaged.
_Avoid_: Task artifact update, progress record

**Task manifest**:
The `index.json` within a Task set. It remains the source of truth for task eligibility and completion. It may optionally carry set-level keys beside the `tasks` array — today `auto_drain` and the **Worktree directive** (`worktree`) — that express authoring intent consumed once at first registration into **Task state** as **Registration seed**s; those keys are not re-applied on refresh. Set-level keys must match their declared types; a non-boolean `auto_drain` or a malformed `worktree` value is a contract fault that makes the Task set **Malformed**. Planning skills such as **to-tasks** write these keys only when the human explicitly requests them in that session; otherwise they are omitted.
_Avoid_: Issue manifest, workload, dashboard

**Worktree directive**:
An optional `worktree` key in a **Task manifest**, beside `auto_drain`, declaring where the set should drain: `{ "managed": true }` provisions a **managed** worktree forked from the **Trunk worktree** (torn down on integration or **Unbind worktree**), or `{ "name": "<worktree>" }` adopts the existing worktree of that name on this machine (never deleted). Absent leaves the drain in the **current checkout** — the directive is a default-shut seam. It is a **Registration seed** (read once into **Task state** at first registration, never re-read) and **lazy**: registration records only the intent; the first *unbound* drain provisions or adopts and records a **Worktree binding**, after which the binding — and any operator **Bind worktree** — takes precedence. Honoured by every drain, foreground **Implement** and **Queue** alike. The portable identifier is the worktree *name* (the operator-facing label, as shown in the **Worktree picker**), never a path: a path would be machine-specific, and a name absent on a given machine is an explicit failure, not a fallback.
_Avoid_: worktree_ready, worktree mode, per-set worktree flag, isolation flag

**Registration seed**:
A set-level key in a **Task manifest** whose value is applied once into the persisted **Task state** at a Task set's first **Task set registration**, then never re-read from the manifest — authoring intent, not live config. The category covers `auto_drain` (the **Manifest auto-drain seed**) and the **Worktree directive** (`worktree`). Editing a seed key after a set is registered has no effect; the durable registry row — and, for the worktree directive, the resulting **Worktree binding** — is authoritative thereafter.
_Avoid_: manifest config, live setting, manifest sync key

**Manifest auto-drain seed**:
The one-time application of a **Task manifest**'s `"auto_drain"` key into **Task state** at first registration. When the key is the boolean `true`, pop sets the set's **Auto-drain** bit on; absent or `false` seeds off. Pop prints `(auto-drain)` on the registration line only when it seeded true. The key is never re-read after registration.
_Avoid_: auto-drain sync, manifest consent refresh

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
The status derived from a discovered Task set whenever a tasks command runs. A **Ready** Task set has at least one eligible task; a **Done** Task set has only done tasks; a **Failed** Task set has at least one failed task. When no AFK task is eligible and the set is neither Done, Failed, nor Deferred, the disposition splits by whether agent work remains: an **Unverified** Task set has no open AFK task left — only human verification (HITL) stands before Done; a **Blocked** Task set still has an open AFK task gated behind a human-in-the-loop task. Pop does not persist a separate completion flag, so artifact changes naturally affect the next derived status.
_Avoid_: Pane status, persisted Task set completion

**In Progress**:
A presentational refinement of the **Ready** display label, shown when a Ready **Task set** already has at least one `done` task — signalling that draining has begun on the set. It is NOT a derived **Task set status**: schedulability is still Ready (drainable), and all scheduling and summary logic keys on the underlying Ready status, never on this label. Rendered blue to distinguish it from a fresh Ready set (cyan); applied identically in both the `pop tasks status` table and the **Queue dashboard**.
_Avoid_: Started, Working, In-progress status, Active

**Run-next badge**:
The `NEXT` marker `pop tasks status` prints on the single highest-priority **Ready** **Task set** — the set a no-argument `pop tasks implement` would drain next in the local runner. Display-only (a derived row flag), unrelated to daemon consent; once the set is actually running the badge reads `RUN`, not `NEXT RUN`.
_Avoid_: AUTO badge, auto-pick, auto-picked, auto-pick badge

**Next task**:
Selecting and executing one task from the highest-priority Ready Task set. Non-runnable Task sets are reported and skipped; among Ready Task sets, equal priority retains registration order.
_Avoid_: First registered Task set, highest-priority Task set regardless of status

**Task executor**:
The mechanism that runs a selected task through an agent, verifies completion, updates the task manifest and progress record locally, and commits implementation changes.
_Avoid_: Workload executor, scheduler

**Implement**:
The single task-execution command, `pop tasks implement`, that runs tasks through the **Task executor** and dispatches by **Task target reference** shape. A `<task-set>/<file>.md` reference runs exactly that one task in the current checkout (**Execution confirmation** prompt once). A bare set identifier — or no argument, choosing the highest-priority Ready set — **drains** the set with no AFK start prompt until it reaches Done, Blocked, Deferred, Failed, or an **Agent quota pause**; mid-drain **HITL gate prompt** and **Failed gate prompt** stay interactive on a TTY. For a whole-set drain: a valid existing **Worktree binding** wins (resume in the bound checkout); otherwise the set's **Worktree directive** routes it (provision a managed worktree or adopt a named one); otherwise pop records a **Default binding** to the **current checkout** and drains there; `--in-worktree` instead provisions a **managed** worktree forked from the **Trunk worktree** and drains there. There is no automatic worktree default and no `--inline` flag — the current checkout is the baseline, and `--in-worktree` is the explicit opt-in to isolation. The interactive **Drain target picker** is a **Queue dashboard** affordance only; bare `pop tasks implement` never prompts for a target, so the Queue's spawned drains never block. When a drain lands Done in a non-trunk checkout, an interactive run's completion prompt offers integration; `--yes` integrates only if the repo opted in with `auto_merge_clean`.
_Avoid_: Run, Drain, separate one-vs-many verbs, --inline, auto-worktree default, run issue, run issues, run all, next Task set, Run PRD

**Agent preset**:
A headless agent the task executor recognizes — `claude`, `opencode`, `cursor`, `codex`, or `pi` — selected by name and optionally augmented with extra invocation arguments (e.g. `claude --model opus4.8`). Pop runs the supplied command as given, exactly as it runs a **Custom agent command**; the sole difference is recognition. Because the first token names a known agent, the **Agent adapter** appends the flags Pop owns — the output protocol governed by **Agent output mode** — after the user's arguments, then the generated prompt as the final positional argument with stdin disconnected. Appending last makes those flags authoritative: a user value for an owned flag is overridden, not rejected. Recognition is what lets Pop parse the structured stream and keep every adapter capability; augmenting a recognized preset this way is distinct from replacing the invocation with a Custom agent command.
_Avoid_: Integration

**Agent fallback**:
The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[workload] default_agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next only on an **Agent quota pause**; a machine-global cooldown store records quota pauses per preset, and the Queue spawns plain `pop tasks implement` so manual and queue-triggered implements share the same fallback memory. When attended **Integration conflict** assistance needs an agent, it uses only the first entry of that same list — no quota-fallback walk, no separate queue-scoped config, and no `--agent` flag on `pop tasks integrate` for now. Standalone integrate resolves the list from config only; the post-drain epilogue inherits the list already resolved for that implement invocation (including explicit `--agent` flags on implement).
_Avoid_: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation, [queue].agents

**Custom agent command**:
A trusted, opaque command supplied via `--agent-cmd` that Pop runs verbatim through a shell, with the generated prompt appended as the final positional argument. Pop neither recognizes nor inspects it, so it forgoes every adapter capability: plain output only, no **Agent quota detection**, no live rendering, and no entry in the **Captured attempt stream**. It governs only unattended task attempts, never attended HITL assistance. It replaces the invocation wholesale — the inverse of an augmented **Agent preset**.
_Avoid_: Override command, escape-hatch agent, agent passthrough

**Built-in model catalog**:
A short, hand-maintained list of model aliases Pop ships for each recognized **Agent preset**, surfaced as a column in `pop tasks agents`, recommended value first — a suggestion surface to help a planner fill an **Agent preset**'s `--model`, never exhaustive, never a validation gate. Distinct from the **Effort ladder** (the resolution surface): several presets' catalogs now feed built-in ladders — `claude`, `codex`, `cursor`, and `pi` — while the curated lists stay advisory. Only `claude`'s entries are stable, auto-resolving, account-independent aliases; the `codex`/`cursor`/`pi` ladders pin version- and account-specific ids that need maintenance and are overridable defaults, not commitments.
_Avoid_: model source, live model listing, model provenance, Curated model aliases

**Effort**:
An optional per-task `effort` key in the **Manifest** — `light`, `standard`, or `heavy` — naming how much capability the task wants from whichever agent runs it. It is the *single* user-facing strength knob: there is deliberately no separate reasoning axis. pop resolves it through the chosen agent's **Effort ladder** into a bundle of *both* a `--model` and a **Reasoning effort** (the model's thinking level), so one tier decides which model runs and how hard it thinks. Orthogonal to the agent axis owned by **Agent fallback** and explicit `--agent` augmentation; it never selects an agent. An absent key means `standard`; an unknown token is a contract fault that makes the Task set **Malformed**. Resolution applies only when no `--model` is hand-pinned in `--agent` args — pinning a model skips the whole bundle (model and reasoning both), since the tier's reasoning was curated for the tier's model; an absent `effort` injects nothing. A reasoning value hand-set in `--agent` args is kept while the ladder model still applies.
_Avoid_: Priority, weight, tier, task size, difficulty

**Effort ladder**:
A per-agent, per-tier ordered list of **(model, Reasoning effort)** bundles that resolves an **Effort** to a concrete `--model` plus a reasoning flag for whichever agent was chosen. Pop ships built-in ladders for `claude`, `codex`, `cursor`, and `pi`; every other agent (e.g. `opencode`) has none built-in and is configured in `config.toml` under `[effort.<agent>]`, which fully replaces the built-in for an agent it names. Each tier is a TOML array of `{ model, reasoning }` tables, reasoning optional. Resolution uses the head of the chosen tier; the ordered tail is reserved for a deferred runtime fallback, and each fallback element carries its own reasoning. Reasoning is rendered per-adapter — `claude --effort`, `codex -c model_reasoning_effort=`, `pi --thinking` — except for `cursor`, which selects a full concrete model name per tier and does not emit a separate reasoning parameter. Agents with no reasoning mechanism (`opencode`) or no ladder make that part a graceful no-op. Surfaced per agent in `pop tasks agents` with built-in-versus-configured provenance.
_Avoid_: Model catalog, effort table, model tier map, model priority list

**Reasoning effort**:
The model's thinking-intensity level (e.g. `low`/`medium`/`high`/`xhigh`/`max`), distinct from pop's **Effort** tier despite the shared word. Not a user-facing knob: it is bundled into each **Effort ladder** tier alongside the model and resolved together, so a tier sets both which model runs and how hard it thinks. Passed per-adapter (`claude --effort`, `codex -c model_reasoning_effort=`, `pi --thinking`); `cursor` has no separate reasoning parameter and instead selects a full model name that already encodes the desired capability. Agents with no mechanism (`opencode`) ignore it. A value hand-set in `--agent` args is respected over the ladder's.
_Avoid_: Effort (pop's task tier), thinking budget, depth

**Interactive agent preset**:
A named attended-assistance command known to an Agent adapter. It is separate from an Agent preset because assisting a human at a HITL gate is an attended conversation, not a headless task attempt; custom headless agent commands do not imply an interactive preset. Every supported preset launches its own interactive binary, so when an **Agent preset** carries extra arguments, those arguments ride into that preset's own attended assistance.
_Avoid_: Agent preset, stripped headless command, agent-cmd

**Agent adapter**:
The preset-specific bridge between Pop and a supported agent. An adapter may provide headless invocation, headless output handling, agent-assistance invocation, and a **Model source**; attended assistance launches the preset's own interactive binary and is owned by the adapter rather than the HITL gate prompt. An adapter reports assistance Unavailable only when it has no usable interactive command at all (e.g. custom headless `--agent-cmd`).
_Avoid_: Universal JSON protocol, agent integration

**Agent catalog**:
The readout of `pop tasks agents`: every recognized **Agent preset** with its binary, whether that binary is on PATH, which preset is the default, and notes such as attended-assistance availability. It reports what Pop owns — recognition and availability — by PATH lookup only; it never execs agents by default, and authentication or deeper health stays with **Doctor**. Its audience is a planner choosing an **Agent preset** as much as a human. Model details come from each preset's **Model source**, surfaced only on request.
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
The model identifier an agent itself reported inside its Captured attempt stream (e.g. Claude's `init` event). It is a derived, per-adapter, best-effort reading at display time — never recorded as a separate event — and is absent when the agent does not report one. It may differ from the model requested in the **Requested agent** arguments through aliases or provider fallbacks. Surfaced in the Attempt timing breakdown and shown once by the live renderer when the agent reports it mid-attempt; `pop tasks status` shows at most requested-agent metadata from the manifest and never reads streams.
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
A Task set with at least one still-open AFK task that cannot run because human-in-the-loop work must happen first — the pre-agent or mid-flow end of the HITL lifecycle. It derives status BLOCKED and a `blocked` **Drain outcome**: real agent work remains, gated on a human. Contrast an **Unverified Task set**, where no open AFK work remains and only human verification is left. Implement reports the condition and stops; the task executor never automatically runs HITL tasks.
_Avoid_: Failed Task set, Unverified Task set

**Unverified Task set**:
A Task set whose only remaining open work is human-in-the-loop verification: every AFK task is done or skipped, and one or more open HITL tasks stand between the set and Done. It is the post-agent end of the HITL lifecycle — the agents are finished and the only thing left is a human's eyes on the result. It derives status UNVERIFIED and a matching `unverified` **Drain outcome**.
_Avoid_: Blocked Task set, Human-blocked Task set, pending verification, review state

**HITL gate prompt**:
An interactive choice shown when implement reaches or selects a Human-blocked Task set. It defaults to getting agent assistance while still letting the human complete the task, defer it, open a **Runtime shell** in the checkout, or exit without changing task state; choosing complete or defer is the explicit manual decision and does not ask for a second yes/no confirmation. Exit is bound to the fixed key `0` (rendered last so its number never shifts as options are added); the other actions take ascending numbers. After complete or defer clears the blocking HITL task, implement refreshes the same Task set and continues from any newly eligible AFK task. When shown because a no-argument implement found no Ready Task set, it is framed as "No runnable AFK work" rather than as a dead end. It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs.
_Avoid_: Automatic HITL execution, yes/no launch prompt

**Failed gate prompt**:
An interactive choice shown when a drain reaches a Failed task. It defaults to re-running the task while still offering agent assistance, finishing by hand, opening a **Runtime shell** in the checkout, or exit without changing task state — the Failed-task counterpart of **HITL gate prompt**. Exit is bound to the fixed key `0` (rendered last so its number never shifts as options are added). It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs.
_Avoid_: Automatic retry, Open task

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
Explicitly returning Failed, Skipped, or Done tasks to Open via `pop tasks open`, regardless of task type — the command is named for the target status. It is the inverse of **Complete task**: undoing a premature completion (e.g. a human-in-the-loop task marked Done before its verification was actually finished) is as valid as retrying a Failed task or re-running a Done AFK task. Reopening a Done task flips the derived **Task set status** out of DONE; for a Done AFK task it becomes eligible again, so a later **Implement** — or the **Queue daemon** in an auto-drain set — re-fires an agent on it. It accepts either a Task-set-relative file reference `<task-set>/<file>.md`, which opens exactly that one task with no prompt, or a whole-set form (`<task-set>` or `<task-set>/`), which opens a **Multi-task selection** where Failed, Skipped, and Done tasks are all checkable (no row pre-checked) and an already-Open task is locked at-target. It removes any recorded attempt count, appends a local progress entry, preserves runtime files, and does not commit. Open task batches need no ordering. The status table prints copy-paste open hints only for Failed tasks; Done and Skipped tasks are reopenable but never advertised there.
_Avoid_: Issue reset, reset, automatic retry, uncomplete

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

**Runtime shell**:
An attended interactive subshell (`$SHELL`, fallback `/bin/sh`) rooted at the **Runtime path**, offered as a menu option at the **HITL gate prompt** and **Failed gate prompt** and as the `O` action in the **Queue dashboard**. It is a pure side-trip for running commands by hand in the checkout — typically an install or build (e.g. `make install-dev`) before sign-off. It never changes task state: on exit, control returns to the gate menu (or the dashboard) unchanged, with no Task-set refresh. In the dashboard it suspends the TUI for the subshell and resumes on exit; a row with no resolved checkout (empty Runtime path) makes the action a no-op with a status-line hint rather than opening a shell.
_Avoid_: assistance session, subshell escape, terminal

**Runtime execution lock**:
A machine-local lock held only while implement is actively executing in a checkout — it is the running **Drain** row, not a claim spanning the whole invocation. It is acquired around each contiguous run of AFK attempts and released at every wait for human input: the pre-run confirmation, the **HITL gate prompt**, and the **Failed gate prompt**. A drain that reaches a gate finishes (recording its park outcome) and the menu, plus any **HITL assistance session** or **Runtime shell** launched from it, runs lock-free; resuming after the human clears the gate re-acquires, refusing cleanly if another drain grabbed the checkout meanwhile. It prevents concurrent task execution in one checkout while allowing unrelated projects or isolated runtime worktrees to execute concurrently; a parked-at-gate pane is no longer treated as busy, so the **Queue daemon**'s anti-double-spawn relies on worktree isolation, not the lock. Lock metadata records the executor PID and running set identifier; a dead PID is reported and replaced as a stale lock.
_Avoid_: Global task lock, project-name lock

**Status table**:
The non-interactive summary printed by `pop tasks status` after discovery refresh. **Archived Task set**s are excluded from the default table; when at least one exists, a quiet footer reports the archived count and the `pop tasks status --archived` command that lists them, so filed-away work stays discoverable. `--archived` instead renders only the Archived Task sets. In the default table, Missing Task sets appear first as stale registrations, followed by Done Task sets. Remaining discovered Task sets then appear in scheduler order: descending priority with stable registration order for ties, so the user can read the active schedule top-to-bottom to understand which Ready work will be selected first. The automatically selected Ready Task set is marked explicitly. Before execution, the actual implement target is also marked; when an explicit Task set override differs from the automatic selection, the table shows both markers on their respective rows. The checkout note describes where a whole-set **Implement** would run by default: the bound checkout when the set has a **Worktree binding**, the target of its **Worktree directive** when one is seeded, otherwise the **current checkout** (a **Default binding** is recorded there on first drain). Single task-file runs are still current-checkout operations. An interactive tasks dashboard is deferred until the table workflow is exercised.
_Avoid_: Workload status table, dashboard

**Execution confirmation**:
The human gate before implement spawns an agent for exactly one targeted task — `Run task? [y/N]` on a `<task-set>/<file>.md` reference. Set drains do not ask "Run AFK tasks in this Task set?"; **Queue scope** standing consent and manual drains alike start AFK work after printing the status table. An explicit `--yes` (`-y`) bypasses the single-task prompt and all interactive mid-drain menus for fully unattended use. Non-interactive single-task runs without `--yes` fail rather than waiting for input.
_Avoid_: HITL task, open task, drain start prompt

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
The scheduling concern over Task-set draining across repositories, surfaced by two drivers: the **Queue daemon** (`pop queue run`, automatic, polls and fans out unattended over **Auto-drain**-marked Ready sets) and the **Queue dashboard** (`pop queue dashboard`, manual, the primary way a human starts drains). Both schedule onto the same substrate — **Repository identity** as the scheduling unit (a repo's worktrees collapse to one unit sharing one **Task storage**, not the picker **Project**), **Worktree binding** as the per-set drain router, the **Integration target** as the fallback checkout, and the **Integration backlog** for reconciliation. The daemon dispatches at most one drain per idle repository per Ready set — never once per worktree — targeting one specific not-currently-running Ready set rather than no-argument implement; each repository drains serially by local **Task set priority** under the **Runtime execution lock** while repositories run in parallel. Absent a per-set binding it records a **Default binding** to the repository's **Integration target**: a non-bare repo's git main worktree, or a **bare** repo's configured **Trunk worktree**; a bare repo with no configured trunk has no integration target, so the Queue refuses to schedule it (it never guesses a checkout). Global cross-project priority ordering is a non-goal.
_Avoid_: Machine-global scheduler, per-worktree scheduler

**Picked-up Task set**:
A Task set currently being drained, identified by a live **Runtime execution lock** that records its **Task identifier**. Picked-up state is derived from lock liveness, never persisted as a task status; tmux panes are display only, not the source of truth.
_Avoid_: In-progress task, pane state

**Queue daemon**:
The supervisor process behind `pop queue run`. It is foreground and explicit, never auto-started from a picker, because it runs coding agents unattended across projects; the operator parks it in a pane and Ctrl-C (`SIGINT`) is graceful shutdown. It is single-instance via a PID/lock file. Unlike the **Monitor** daemon, it needs no control socket: it persists agent cooldowns, parked sets, backoff timers, and the **Queue journal** to disk, so `pop queue status` and `pop queue log` are pure file readers. On `run`, it reconciles in-flight drains from live **Runtime execution lock**s, so a restart never disturbs them. Its command surface is `run`, `status`, and `log`; Ctrl-C is stop.
_Avoid_: Monitor daemon, background service

**Queue scope**:
The set of work the **Queue daemon** supervises: **Auto-drain**-marked Ready Task sets in git-backed registered projects. Running `pop queue run` is standing consent to act, but the daemon drains only sets a human has marked **Auto-drain** (default off); there is no per-project opt-in flag and no per-drain AFK start prompt. The per-set opt-in is **Auto-drain**, toggled from the **Queue dashboard**; the per-set opt-out remains **Archive**; manual `i` from the **Queue dashboard** drains a set regardless of its **Auto-drain** bit. Queue spawns plain `pop tasks implement <set>` — no `--yes` — so **HITL gate prompt** and **Failed gate prompt** stay interactive when the drain pane has a TTY. The blast radius is self-limiting because the daemon only acts on Auto-drain Ready sets, and a Task set is a deliberately authored artifact; a project with no sets is skipped. A configured **Project** with no git checkout is also outside **Queue scope** — it has no **Repository identity** and therefore no **Task storage**; the supervisor silently skips it like a project with no sets, never a scan error. When a project has no tmux session, the daemon creates one detached and splits a drain pane into that session's main window (index 0); subsequent drains split additional panes there.
_Avoid_: Per-project queue opt-in, global priority queue, per-drain --yes

**Queue journal**:
The durable append-only record in pop's data dir of every Queue drain event: started, done, failed, HITL-blocked, quota-paused-and-agent-switched, crashed, backing-off, or parked. It is emitted by **Implement** as a structured drain-outcome record carrying set id, outcome, and the exhausted preset when relevant; the **Queue daemon** consumes it to drive **Agent fallback** and backoff, and persists it for observability. `pop queue status` reads live state, such as picked-up sets, cooling agents, parked sets, and idle projects; `pop queue log` reads the journal history.
_Avoid_: Progress record, Captured attempt stream, Task state

**Drain**:
One supervised execution of draining a **Task set**, tracked through an explicit lifecycle from start to a terminal disposition (its **Drain outcome**). A Task set may be drained many times — after a reset, a crash, or a quota pause — and each is a distinct Drain; a set's Drain history is the ordered record of them. The Drain, not the Task set, carries execution lifecycle state; the set's manifest-derived **Task set status** (what work remains) is a separate, derived concern.
_Avoid_: Run, attempt, drain record

**Drain outcome**:
How a **Drain**'s process ended — its exit reason, not the set's work disposition: finished (the drain ran to its own stopping point), quota-paused (an agent preset hit quota), interrupted (deliberate SIGINT teardown), or crashed (the process died unexpectedly, recorded by reconciliation rather than by the drain itself). The set's resulting work disposition — done, failed, blocked, unverified, deferred — is read from the manifest-derived **Task set status**, never restated on the Drain. finished and quota-paused are clean exits; interrupted and crashed are abnormal and drive crash backoff.
_Avoid_: Task set status, drain disposition, drain result

**Queue run output**:
The live stdout of `pop queue run` — an operator-facing event stream, not a repeating inventory. It prints one **Queue run baseline** on startup (the full scheduling-relevant picture of what the supervisor is watching), then only **Queue run deltas** when something changes: spawns, terminal drain outcomes, agent cooldowns, parks, integration events, and errors. A quiet tick with no change prints nothing. Drain panes keep their own implement output; `pop queue status` remains the on-demand full snapshot.
_Avoid_: Per-tick status dump, queue log replay

**Queue run baseline**:
The one-time inventory printed when `pop queue run` starts. It opens with a **Queue status summary** — aggregate queue work (running, queued, blocked) and integration readiness (awaiting count, ready-to-merge, conflicts) — then lists every scheduling-relevant bucket the supervisor is watching: running drains, queued ready sets, blocked state (parked sets, crash backoffs, agent cooldowns), sets awaiting integration, and scan errors for in-scope projects that failed to scan or have a broken repo-root `.pop.toml` — in the same human-readable shape as `pop queue status` (without the raw daemon-state JSON dump). Projects outside **Queue scope** and in-scope projects with no ready work and no active drain are not listed individually; they collapse into a single count line (e.g. "12 other projects: no ready work").
_Avoid_: Per-project idle listing, repeating status table

**Queue status summary**:
The headline block at the top of `pop queue status` and the **Queue run baseline**. It aggregates current queue work — how many Task sets are running, queued for drain, or blocked — and integration posture — how many completed sets await integration and whether they merge clean, conflict, or are unknown. Detail lines below expand each bucket; the summary is the at-a-glance answer to "what is in the queue and what can I integrate now?"
_Avoid_: Daemon state JSON, per-project idle dump

**Queue run delta**:
A single stdout line emitted by `pop queue run` when supervisor-relevant state changes after the baseline. Deltas cover spawns, terminal drain outcomes (done, failed, HITL-blocked, quota-paused, crashed), set parked, agent cooldown started, cooldown or backoff cleared (work may resume), integration landed, and per-project scan errors. Unchanged state — still running, still cooling, still waiting — prints nothing.
_Avoid_: Heartbeat line, per-tick inventory repeat

**Queue backoff**:
The daemon's response to an abnormal drain exit, such as crash, kill, or interrupt. Unlike a clean failure or quota pause, an abnormal exit leaves the set Ready with nothing cooled and would otherwise re-spawn immediately. The daemon applies an escalating per-set delay and, after N consecutive abnormal exits, parks the set until a human clears it. A clean exit resets the counter. Distinguishing abnormal from clean exits requires the **Queue journal**'s outcome record; storage status alone cannot tell a crash from a quota pause.
_Avoid_: Failed task, Agent quota pause

**Queue window**:
The single tmux window, named `pop-queue`, that the Queue daemon spawns its drains into within a Project's session. All queue-spawned drains for that project — both in-place and **Worktree set** — land here as panes under a balanced (`tiled`) layout, instead of in the user's working windows or in per-worktree sessions. One Queue window per project session; created on first spawn, reused thereafter.
_Avoid_: Drain session, worktree session, queue tab

**Auto-drain**:
A per-set persisted consent bit in **Task state**, alongside priority and the archived flag, marking that the **Queue daemon** may automatically drain this **Task set**. It defaults off for a freshly-discovered set, inverting the old standing-consent model: `pop queue run` drains nothing until a set is marked auto-drainable from the **Queue dashboard**, or a human launches it by hand. A **Task manifest** may declare `"auto_drain": true` at the set level; pop reads that key once at first registration — whether via lazy discovery, import, or any other path that creates the registration entry — and seeds Task state accordingly; it does not re-sync on later refresh, so the **Queue dashboard** toggle remains authoritative after registration. It is orthogonal to **Archive** (which hides a set entirely), distinct from a **Picked-up Task set** (a runtime live-lock fact, not consent), and distinct from the **Run-next badge** (`NEXT`, a local-runner display marker that shares the word "auto" only in the retired `AUTO` badge — they are unrelated).
_Avoid_: Pickable, pick-up status, auto-pickup, queue-enrolled

**Queue dashboard**:
The interactive `pop queue dashboard` TUI — the primary hands-on surface for starting and managing **Queue** work, sibling to the **Project picker** and **Worktree picker**. Machine-global like `pop queue status`, it scans every registered repository's **Task storage** and renders one row per non-archived **Task set** with outstanding queue-actionable state, excluding only a concluded **Done Task set**. Each row shows the derived **Task set status**, the set's worktree/destination column (the bound checkout if bound, else the **Trunk worktree** — an honest "where auto-drain lands"), a live **Picked-up** drain indicator, and an **Auto-drain** badge. Keys: `i` opens the **Drain target picker** for an unbound set then drains, or resumes silently in the bound checkout for a bound one; `I` integrate; `b` bind or create a worktree in advance (without draining); `U` unbind; `a` toggle **Auto-drain**; `p` preview the working pane; `gg`/`G` move to top/bottom; `h`/left/`esc` back or exit; `l`/Enter open the **Task set detail view**. `q` and the former `s` shortcut are intentionally unbound.
_Avoid_: Queue picker, queue status table, drain dashboard

**Drain target picker**:
The interactive chooser the **Queue dashboard** opens on `i` for an **unbound** **Task set**, fusing target selection with the drain into one bind-and-start action. It lists the repo's existing **non-managed** worktrees (pick → adopt as an adopted **Worktree binding**), a "new managed worktree" option (provision a managed binding forked from the **Trunk worktree**; the default cursor), and the **Trunk worktree** itself (drain inline, no binding). The chosen target is bound and then drained immediately. A set already holding a binding skips the picker and resumes in its bound checkout — retargeting requires **Unbind worktree** first. Options requiring a trunk (new managed worktree, trunk) are absent when no trunk is resolvable (an unconfigured bare repo). Managed and already-adopted worktrees are excluded from the existing-worktree list, since each checkout belongs 1:1 to one set.
_Avoid_: checkout picker, drain wizard, runtime picker

**Task set detail view**:
The full-screen interactive drill-down entered with `l` or Enter from the **Queue dashboard**, replacing the table until dismissed with `h`/left/`esc`. It lists the focused **Task set**'s tasks, supports Vim-style list movement including top and bottom (`gg`/`G`), opens a read-only **Task text peek** for the cursored task with `l` or Enter, and applies **Complete task** (`C`), **Open task** (`O`), or **Skip** (`K`) to the single cursored task without a separate confirmation.
_Avoid_: status view, status modal, inspect modal, task editor

**Task text peek**:
A read-only nested view inside the **Task set detail view** that shows the full markdown text of the cursored task file from **Task storage**. It is opened with `l` or Enter from the task list, supports Vim-style scrolling (`j`/`k`, `ctrl-d`/`ctrl-u`, `gg`/`G`), and is dismissed with `h`/left/`esc`, returning to the same **Task set detail view** without changing task status.
_Avoid_: task editor, task modal, preview pane

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

### Configuration

**Repo override**:
A section in the global `config.toml` — `[repo."<path>"]`, keyed by any path that canonicalizes to a **Repository identity** — carrying the per-repo behaviour subset (the `.pop.toml` **RepoConfig** schema: `auto_merge_clean`, plus the **Trunk worktree** marker `trunk`) at higher priority than the repo's `.pop.toml`. Resolution is global override → `.pop.toml` → built-in default. The asymmetry is deliberate: global config may express everything `.pop.toml` can (so a user can flip behaviour without committing to the repo), but `.pop.toml` never gains global-only settings (the project registry, queue agent rotation, machine-global daemon knobs).
_Avoid_: project entry override, glob-scoped behaviour

**Trunk worktree**:
A repository's single canonical integration anchor and the fork base for managed **Worktree set**s. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must declare one explicitly via a `trunk = true` per-checkout **Repo override**. Managed worktrees fork from the trunk's HEAD and every non-trunk binding integrates back into it. An unconfigured bare repo has no trunk, so pop can neither provision a managed worktree nor integrate there — it can only drain in place in whatever checkout the operator is currently sitting in.
_Avoid_: Execution base, execution_base, queue base, queue_base, default worktree

**Config finding**:
A single config-validation problem discovered during load, keyed to its config path (e.g. `effort.foo`, `projects[2].display_depth`) and carried on the loaded config instead of thrown. Surfaced two ways: as the `error` from the getter for that key, and as a non-blocking entry in the picker's warning banner.
_Avoid_: config error (when you mean a non-fatal finding, not unparseable TOML)

**Core capability**:
The one thing a command must produce to be worth running — e.g. the project list for `pop project dashboard`. A command aborts on a config problem only when a value it consumes is invalid *and* essential to this capability; every other config problem degrades to a default plus a warning, and the command still runs.
_Avoid_: command feature, required config

**Include**:
A sidecar TOML file the global `config.toml` pulls in via `includes`, carrying only a whitelisted subset of config — registered **Project**s and **Repo override** blocks — so a user can keep which directories they work on out of the main file. Precedence is parent first, then includes in listed order; the first definition of a repo key sticks, and any other config section in an include is ignored. Distinct from `.pop.toml`, which rides in a repo and describes one already-registered project.
_Avoid_: Import, partial, sidecar config, overlay

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
- `workload artifact ignore coverage` → removed; Task storage lives outside the repository tree (ADR 0039)
- `Queue base`, `queue_base`, `Execution base`, `execution_base` → **Trunk worktree**, `trunk`
- `Worktree-ready project`, `worktree_ready` → removed; there is no repo-capability auto-managed-worktree default — worktree execution is explicit via a **Worktree directive** or `pop tasks implement --in-worktree`
- `Curated model aliases` → **Built-in model catalog**

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
