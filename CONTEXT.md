# Pop

A CLI for navigating between development directories and their tmux sessions. Pop tracks which panes need attention and provides fuzzy-search pickers for switching context quickly.

## Language

**Project**:
A directory on disk that pop knows about — either listed explicitly in config or matched by a glob pattern. Choosing a project in the project picker is the primary workflow; attaching to or creating a tmux session follows from that choice.
_Avoid_: Folder, workspace, session (when you mean the directory itself)

**Project command**:
The `pop project` entry point — opens the project picker. Project-specific config lives in `[project]`. `pop select` and `[select]` are deprecated aliases; remove at the next major release. The CLI alias is hidden (not shown in help) and emits no runtime warning; the config alias emits a load-time warning.
_Avoid_: Select command, normal mode

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
The presentation of the monitored set of panes — a browsable view of registered panes, their status, and visit times. `pop dashboard` opens this view.
_Avoid_: Monitor (when you mean the tracking mechanism, not the view)

**Monitor**:
The subsystem that maintains the monitored set of registered panes — tracking status, visit times, and notes via daemon, state, and tmux hooks. Agent integrations report into the monitor; the dashboard reads from it. Exposed via `pop pane monitor-start`, `monitor-stop`, and `monitor-status`.
_Avoid_: Dashboard (when you mean the view, not the mechanism)

**Agentic pane**:
A pane running an AI coding agent or its runtime (e.g. Claude, OpenCode, Pi). Integrations cause these panes to register with the **Monitor**; other panes may also be tracked explicitly.
_Avoid_: Agent pane, bot pane

**Registration**:
A pane entering the **Monitor**'s tracked set. A pane is **tracked** once registered; untracked panes are outside pop's domain.
_Avoid_: Tracking (when you mean the act of entering the set, not the ongoing state)

**Auto-registration**:
**Registration** that happens as a side effect of an untracked pane's first report, rather than an explicit add — the common path for **agentic panes** via **integrations**. The trigger differs by report: reporting a status auto-registers the pane unless registration is suppressed; setting **Following** auto-registers only when following (never when unfollowing); a **Visit** never auto-registers.
_Avoid_: Self-registration (same event seen from the agent's side; prefer auto-registration)

### Pickers

**Project picker**:
The fuzzy-search picker opened by the project command — for choosing a project, worktree, or standalone session.
_Avoid_: Session picker, select view, normal mode

**Worktree picker**:
The fuzzy-search picker in `pop worktree` for choosing, creating, or deleting git worktrees in the current repository.
_Avoid_: Repo picker

**History**:
The persisted record of projects you've selected, with timestamps.
_Avoid_: Recents, access log

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

### Workloads

#### Lifecycle

This overview relates the terms defined below; read it before changing workload behaviour. It is a domain model, not an implementation guide.

An **issue** moves between four statuses. The executor drives the solid transitions; the human drives the dashed ones through manual override commands.

```
                  Run issue / Run issues (agent success)
        open ──────────────────────────────────────────▶ done
         │ ▲                                              ▲ ▲
         │ │ Issue reset                       Complete   │ │ Complete
         │ └──────────── failed ◀── attempt ───┐ issue ───┘ │  issue
         │                  │   exhaustion/timeout           │
         │ Skip issue       │ Issue reset                    │
         ▼                  ▼                                │
      skipped ─────────────┴────────────────────────────────┘
              Issue reset (skipped → open) / Complete issue (skipped → done)
```

- An issue is **eligible** when it is `open`, type AFK, and every `blocked_by` prerequisite is satisfied. A prerequisite counts as satisfied when it is `done` **or** `skipped` — a Skipped issue unblocks its dependents even though it was deferred, not completed.
- HITL issues are never eligible; the executor never runs them.
- `complete-issue`, `skip-issue`, and `reset-issue` are the only manual overrides; each moves exactly one issue and bypasses the agent.

An **Issue set**'s status is derived from its issues, in this precedence:

```
all issues done ............................. DONE
any issue failed ............................ FAILED
has an eligible AFK issue ................... READY      ← Run issues drains these
every issue done or skipped, ≥1 skipped ..... DEFERRED   ← conclude or reopen later
otherwise (unfinished, none eligible) ....... BLOCKED    ← Human-blocked: HITL or undone dependency
```

(`MISSING` and `MALFORMED` sit outside this derivation — they are registration and contract faults.) Automatic selection runs READY sets in scheduler order and passes over DONE and DEFERRED sets. Run issues stops when its set reaches DONE, FAILED, BLOCKED, or DEFERRED, or when an **Agent quota pause** interrupts draining without changing issue status. At a BLOCKED HITL gate it advises the recovery paths (complete, edit-and-rerun, or skip).

**Workload**:
A machine-local schedule of Issue sets whose issues can be executed by an agent. A workload decides which Issue set to draw work from next; it does not replace the local Issue sets or their execution rules.
_Avoid_: Issue set, project dashboard

**Issue set**:
The local `thoughts/issues/<id>/index.json` manifest and its sibling issue markdown files. An Issue set is the schedulable unit of a workload. Its directory name is its canonical identifier and display label; there is no separate Issue-set title. It may be created from a PRD, a grilling session, or another planning workflow; PRD existence is irrelevant to workload scheduling and execution.
_Avoid_: PRD, workload

**Issue set registration**:
An Issue set entering a workload so pop may select issues from it. Pop automatically registers discovered Issue sets and reports newly registered Issue sets to the user. Registration metadata and Issue set artifacts remain machine-local.
_Avoid_: Import, tracking

**Workload definition path**:
The directory where pop discovers a workload's Issue sets. Discovery scans only `thoughts/issues/*/index.json` beneath this directory. It defaults to the selected project's path and may be overridden for a command. The definition path may be a designated worktree and must not be inferred from a repository's shared git directory.
_Avoid_: Shared git root, runtime path, project root

**Workload runtime path**:
The git checkout from which issue execution starts. It defaults to the selected project's path and may be overridden for a command. Pop resolves it to the checkout root and uses that root for the agent working directory, dirty-tree preflight, staging, commits, and the Runtime execution lock. Workload artifacts remain under the separate Workload definition path. Durable workload path configuration is deferred until worktree-oriented execution needs it.
_Avoid_: Definition path, shared git root

**Dirty runtime strategy**:
Controls how workload execution starts from a dirty runtime checkout. `continue` starts execution without modifying the existing dirty state; it is the default both when the option is absent and when it is present without a value, and after successful issue completion the normal implementation commit intentionally includes both pre-existing and agent changes. `commit-and-continue` captures the existing dirty state in a separate implementation commit before invoking the agent. `stash-and-continue` stashes tracked and untracked changes but not ignored files, prints the stash reference when one is created, and leaves restoration to the user; an empty stash does not prevent execution. When the runtime is dirty the command always displays `git status` and the chosen strategy's effect, then requires interactive `y` confirmation; `--yes` auto-confirms, and a non-interactive run without `--yes` is rejected. Run issues applies the chosen strategy once before draining its selected Issue set.
_Avoid_: Clean runtime checkout requirement, automatic stash restoration

**Implementation commit**:
A commit created by the workload executor from runtime-checkout changes. After successful issue completion, the executor stages all runtime changes and commits them with an issue-derived subject and the agent summary as body. Workload artifacts remain local and unstaged.
_Avoid_: Workload artifact update, progress record

**Issue manifest**:
The `index.json` within an Issue set. It remains the source of truth for issue eligibility and completion.
_Avoid_: Workload, dashboard

**Issue parent reference**:
Optional planning context written inside an issue markdown file, such as a `## Parent` section pointing to a PRD or another artifact. An issue may be self-contained. Pop does not require, synthesize, validate, or interpret parent references.
_Avoid_: Required PRD pairing, Issue set identity

**Workload project resolution**:
Choosing the project path for a workload command. A unique project display-name match may be selected explicitly; ambiguous names must be rejected with candidate paths. A direct path may be supplied as an escape hatch. When neither is supplied, the current directory is used.
_Avoid_: Worktree discovery, workload definition path

**Issue set priority**:
A numeric workload value used to choose between ready Issue sets. Newly registered Issue sets start at priority `0`. Higher priority wins; equal-priority Issue sets retain workload list order.
_Avoid_: Issue dependency, issue-manifest order

**Issue set workload status**:
The status derived from a discovered Issue set whenever a workload command runs. A **Ready** Issue set has at least one eligible issue; a **Done** Issue set has only done issues; a **Failed** Issue set has at least one failed issue; a **Blocked** Issue set is unfinished but has no eligible issue. The workload does not persist a separate completion flag, so artifact changes naturally affect the next derived status.
_Avoid_: Pane status, persisted Issue set completion

**Next issue**:
Selecting and executing one issue from the highest-priority Ready Issue set. Non-runnable Issue sets are reported and skipped; among Ready Issue sets, equal priority retains workload list order.
_Avoid_: First registered Issue set, highest-priority Issue set regardless of status

**Workload executor**:
The mechanism that runs a selected issue through an agent, verifies completion, updates the issue manifest and progress record locally, and commits implementation changes.
_Avoid_: Workload scheduler

**Run issue**:
Executing exactly one eligible issue from a Ready Issue set. By default pop chooses the Issue set using workload priority. When a positional argument is supplied, it must be a CWD-relative path to the issue markdown file; bare issue identifiers and absolute paths are rejected. A bare filename is accepted when it resolves from the current directory to an issue markdown file under a discovered Issue set. Targeting still requires Open status, AFK type, and satisfied dependencies.
_Avoid_: Next issue

**Run issues**:
Sequentially executing eligible issues from one Ready Issue set until it becomes Done, Blocked, Deferred, or Failed, or until an **Agent quota pause** stops draining cleanly. By default pop chooses the Issue set using workload priority. When a positional argument is supplied, it must be a CWD-relative path to the Issue set directory; bare Issue set identifiers, absolute paths, and non-relative reference forms are rejected. It does not continue into another Issue set.
_Avoid_: Run all, next Issue set, Run PRD

**Agent preset**:
A named headless agent command known to the workload executor. An explicit agent command may override a preset. The executor appends its generated prompt as the final positional argument and disconnects stdin.
_Avoid_: Integration

**Agent output adapter**:
The preset-specific interpretation of an agent's headless output. An adapter may recover completion text or detect an **Agent quota pause** from a structured protocol; when it cannot interpret the output, the original text remains subject to the normal **Completion sentinel** contract.
_Avoid_: Universal JSON protocol, agent integration

**Agent output mode**:
Controls whether one Agent preset uses its Agent output adapter or a plain-text compatibility fallback. Plain-text mode disables adapter capabilities such as Agent quota detection.
_Avoid_: Agent quota reporting, universal JSON protocol

**Agent quota reporting**:
Proactively displaying subscription quota remaining in a provider-specific rolling window, such as a five-hour limit. This is separate from **Agent quota detection** and remains deferred until each agent CLI exposes a supported headless status interface. Token totals, private authentication-file access, undocumented endpoints, and interactive-terminal scraping are not substitutes for quota reporting.
_Avoid_: Token usage, API cost

**Agent quota detection**:
Identifying from an Agent output adapter that an issue attempt stopped because the agent allowance is exhausted. Detection is preset-specific and relies on a stable headless signal. A detected quota pause stops Run issue or Run issues cleanly without retrying, leaves the issue Open, preserves partial runtime changes, and does not append a progress record. It is not a Failed, Skipped, or Interrupted issue. Proactively reporting remaining allowance is the separate **Agent quota reporting** concern.
_Avoid_: Agent quota reporting, failed issue, skipped issue

**Agent quota pause**:
The clean stop produced by Agent quota detection. It leaves the current issue Open and preserves its partial runtime changes, so a later Run issue or Run issues invocation may resume work after allowance returns.
_Avoid_: Exhausted issue, Interrupted issue, Failed issue

**Issue attempt**:
One agent invocation for an issue. The workload executor retries an unsuccessful issue up to the configured maximum, defaulting to three attempts. Exhaustion marks the issue Failed, records the attempt count and reason locally, and stops Run issues.
_Avoid_: Issue set retry, issue dependency

**Issue attempt timeout**:
The maximum duration for one issue attempt, defaulting to 30 minutes and configurable per command. When exceeded, the workload executor terminates the agent process group, preserves partial changes, marks the issue Failed locally, appends a Failed progress record, and stops immediately without further retries. A deliberate retry requires an Issue reset.
_Avoid_: Issue set timeout, interruption

**Human-blocked Issue set**:
An Issue set with unfinished issues but no eligible AFK issue because human-in-the-loop work must happen first. Run issue and Run issues report the condition and stop; the workload executor never automatically runs HITL issues. On stopping, pop advises the recovery paths for the blocking HITL issue: Complete issue once the human work is done, edit the issue file and re-run, or skip the issue to defer it and unblock its dependents (Skipped issue). The blocked row also shows a copy-paste complete hint, symmetric with the reset hint on Failed rows.
_Avoid_: Failed Issue set

**Workload artifact**:
A machine-local planning document, issue markdown file, issue manifest, or progress record beneath `thoughts/`. The workload executor updates workload artifacts locally but does not stage or commit them with implementation changes.
_Avoid_: Implementation change, workload state

**No-op issue completion**:
A successful issue execution that produces no staged implementation change. The workload executor marks the issue Done locally, appends progress, reports that no implementation commit was created, and allows Run issues to continue.
_Avoid_: Failed issue, empty commit

**Exhausted issue**:
An issue that remains unsuccessful after its maximum attempts. The workload executor marks it Failed locally, preserves any partial implementation changes for inspection, does not commit them, and stops Run issues.
_Avoid_: No-op issue completion, reverted issue

**Interrupted issue**:
An issue whose active agent process was terminated by user interruption or process termination. The workload executor forwards termination to the agent process group, preserves partial implementation changes, leaves workload artifacts unchanged, and exits without committing. An interrupted issue is not Failed.
_Avoid_: Exhausted issue, failed issue

**Issue reset**:
Explicitly returning one Failed or Skipped issue to Open so it may be attempted again. The reset command requires a CWD-relative path positional argument to the issue markdown file; bare issue identifiers and absolute paths are rejected. A bare filename is accepted when it resolves from the current directory to an issue markdown file under a discovered Issue set. Reset removes any recorded attempt count, appends a local progress entry, preserves runtime files, and does not commit. The workload status table prints copy-paste reset hints using the canonical path `thoughts/issues/<id>/<file>.md` from the workload definition root.
_Avoid_: Issue set reset, automatic retry

**Complete issue**:
Manually marking one Open, Failed, or Skipped issue Done without running an agent, regardless of issue type. Used primarily to clear a human-in-the-loop issue after the human performs the work, to conclude a Skipped issue once its deferred verification is satisfied, and also valid for finishing an AFK or Failed issue by hand. The command requires a CWD-relative path positional argument to the issue markdown file; bare issue identifiers and absolute paths are rejected, and a bare filename is accepted when it resolves from the current directory to an issue markdown file under a discovered Issue set. All `blocked_by` dependencies must be Done. It bypasses the Completion sentinel — it does not verify acceptance criteria, does not prompt for confirmation, and does not stage or commit implementation changes; the human owns and commits that work. It appends a local COMPLETE progress record noting the prior state.
_Avoid_: Completion sentinel, no-op issue completion, run issue

**Skipped issue**:
An issue the human deliberately set aside via skip-issue, recorded with the `skipped` status. Skipping accepts only an Open issue of any type and is the deadlock breaker when a human-in-the-loop issue cannot be verified until its own follow-up issues complete. A Skipped issue is never selected for execution, yet — unlike an Open dependency — it satisfies `blocked_by` for its dependents, so downstream issues become eligible against a deliberately deferred, not completed, prerequisite. The command mirrors Issue reset targeting and appends a local SKIP progress record. A Skipped issue later resolves through Complete issue (to Done) or Issue reset (to Open).
_Avoid_: Exhausted issue, interrupted issue, blocked issue

**Deferred Issue set**:
An Issue set in which every issue is Done or Skipped and at least one is Skipped, so no runnable, failed, or open work remains but the set is not Done. Run issue and Run issues stop cleanly reporting the deferral rather than an error, and automatic selection passes over it like a Done set so it never blocks the queue. The workload status table keeps it visible with its skipped count so the human remembers to conclude or reopen the Skipped issues. A set with any still-Open issue, including an Open HITL issue, is Ready or Human-blocked rather than Deferred.
_Avoid_: Done Issue set, Human-blocked Issue set

**Progress record**:
The append-only local `progress.txt` history beside an issue manifest. It records terminal Done and Failed outcomes, explicit issue resets, and manual completions. Intermediate attempts are streamed during execution but are not appended.
_Avoid_: Workload state, agent output log

**Completion sentinel**:
The machine-readable ending emitted by an agent after an issue attempt. Success requires a zero agent exit status, a summary block followed by `TASK_COMPLETE`, and every acceptance-criteria checkbox in the issue markdown checked. Failure may end with `TASK_FAILED: <reason>`.
_Avoid_: Agent exit code, progress record

**Malformed Issue set**:
A discovered Issue set whose issue manifest or issue markdown files violate the workload contract. This includes an issue with persisted `in_progress` status: the synchronous workload executor does not use that status because it could become stale after a crash. Malformed Issue sets are reported in the workload status table and skipped during automatic selection; the workload executor never spawns an agent for them.
_Avoid_: Blocked Issue set

**Workload state**:
The machine-local persisted record of workloads and their registered Issue sets. A workload is keyed by its canonical definition path; its Issue sets are stored in registration order with priority. Workload state does not duplicate derived Issue set completion.
_Avoid_: Workload artifact, issue manifest

**Runtime execution lock**:
A machine-local lock held while Run issue or Run issues executes for a canonical workload runtime path. It prevents concurrent workload execution in one checkout while allowing unrelated projects or isolated runtime worktrees to execute concurrently. Non-execution workload commands remain available. Lock metadata records the executor PID; a dead PID is reported and replaced as a stale lock.
_Avoid_: Global workload lock, project-name lock

**Workload status table**:
The non-interactive summary printed by the workload status command after discovery refresh. Missing Issue sets appear first as stale registrations, followed by Done Issue sets. Remaining discovered Issue sets then appear in scheduler order: descending priority with stable registration order for ties, so the user can read the active queue top-to-bottom to understand which Ready work will be selected first. The automatically selected Ready Issue set is marked explicitly. Before execution, the actual Run target is also marked; when an explicit Issue set override differs from the automatic selection, the table shows both markers on their respective rows. An interactive workload dashboard is deferred until the table workflow is exercised.
_Avoid_: Dashboard

**Execution confirmation**:
The human gate before Run issue or Run issues spawns an agent. Pop prints the refreshed workload status table with the selected Issue set marked and asks for `y/n` confirmation. Run issues asks once before draining its selected Issue set, not before each issue. An explicit `--yes` (`-y`) option bypasses the prompt for unattended use. Non-interactive execution without that option fails rather than waiting for input.
_Avoid_: HITL issue, issue reset

**Workload execution exit status**:
The process result exposed by Run issue and Run issues: `0` for completed work or a declined confirmation, `1` for execution failure, timeout, malformed target, commit failure, or a live Runtime execution lock, `2` when no runnable issue exists, `3` for usage, configuration, or project-resolution errors, and `130` for interruption.
_Avoid_: Issue set workload status, agent exit code

**Workload status exit status**:
The process result exposed by the workload status command. Rendering a resolved workload succeeds even when rows are Malformed, Failed, or Blocked; non-zero is reserved for failures that prevent workload resolution or rendering.
_Avoid_: Workload execution exit status

**Workload identifier**:
The canonical name of an Issue set — its directory name under `thoughts/issues/` — or an issue-manifest issue ID. These identifiers drive scheduling, state, and display.
_Avoid_: Display title, filename, path

**Workload target reference**:
A CWD-relative path that identifies an Issue set directory or issue markdown file on Run issue, Run issues, and Issue reset. Run issue and Run issues accept an optional positional argument; Issue reset requires one. Bare **Workload identifiers**, absolute paths, and other non-relative forms are rejected. A bare filename is accepted when it resolves from the current directory to an issue markdown file under a discovered Issue set. A reference may point at an Issue set directory, at `thoughts/issues/<id>`, or at an issue markdown file beneath a discovered Issue set, including `.` when the shell is already inside that Issue set directory. Pop normalizes every accepted reference to the canonical Issue set and issue identifiers before selection. Resolved paths must match an Issue set discovered under the command's workload definition path; paths outside that discovery are rejected. When the argument is not a relative path form — including bare **Workload identifiers** and absolute paths — rejection explains that a relative path is required. When a relative path fails to resolve, rejection lists valid **Workload identifiers** only, not example paths. Titles, prefixes, fuzzy matches, and unresolved paths are rejected.
_Avoid_: Workload identifier, shell completion candidate

**Workload shell completion**:
Read-only shell tab completion for workload subcommands, project names, **Workload target reference** paths, agent presets, and path flags. Positional completion on Run issue, Run issues, and Issue reset offers CWD-relative path segments only — such as `thoughts/issues/<id>/` prefixes and `./` or `../` — not bare **Workload identifiers**. Set-priority still completes bare Issue set identifiers for its ISSUE_SET positional. Completion may scan local workload artifacts but must not auto-register Issue sets, persist workload state, or print warnings.
_Avoid_: Shell autosuggestion, discovery refresh

**Missing Issue set**:
A locally registered Issue set whose manifest is no longer present beneath the workload definition path. Its registration, priority, and list order are preserved in case the Issue set returns. It is skipped during execution and shown before all discovered Issue sets in the workload status table so active work remains grouped toward the end for a future terminal UI.
_Avoid_: Malformed Issue set

## Deprecated aliases

- `idle`, `read` → **Clear**
- `needs_attention` → **Unread**

## Flagged ambiguities

**Dashboard vs monitor** — **Monitor** maintains the monitored set; **Dashboard** presents it. Code uses both names loosely (`monitor` package, `dashboard` command); use domain terms when writing docs or discussing behavior.

**Visit vs status change** — A **Visit** records interaction with a pane without changing its status. Changing a pane to **Clear** records that no attention is required. Some navigation actions intentionally do both.

**Active vs working** — An **Active pane** is currently visible to the user. A **Working** pane has an agent or process actively running. A pane may be either, both, or neither.

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
> **Dev:** What if a workload agent changes its structured output and pop cannot interpret it?
>
> **Expert:** Its **Agent output adapter** falls back to the original text, which still has to satisfy the normal **Completion sentinel** contract. An **Agent quota pause** is different: when the adapter recognizes one, the issue stays Open and Run issues stops cleanly.
