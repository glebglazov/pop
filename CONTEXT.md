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
A normalized lowercase kebab slug (≤5 words) naming the subject of an **Agentic pane**, single-sourced as a per-pane tmux property any tmux surface can display. pop fills it in stages — an instant **Topic seed** from truncating the prompt, then optionally a higher-quality agent-derived final value, and optionally re-derived as the conversation drifts. It is now the sole display subject of a pane: the user-authored note that used to outrank it has been removed.
_Avoid_: summarization, title, pane name, label, summary

**Topic recipe**:
One step in pop's ordered Topic-derivation list. A step is either a **truncate step** (cheap, local, no model — produces a seed) or an **agent step** (a curated agent-CLI invocation — produces a final Topic). Each step declares a `set_if` guard for when it may run against the current **Topic provenance**, and may carry its own appended arguments and timeout. pop owns the prompt and output normalization but links no model SDK and holds no API keys — auth lives in the CLIs.
_Avoid_: topic command, topic model

**Topic seed**:
A provisional Topic written instantly by the truncate step, before any model runs, so a pane has an immediate subject. An agent step may overwrite a seed; a final Topic may not be overwritten by it.
_Avoid_: provisional topic, draft topic

**Topic provenance**:
Whether a pane's current Topic is a provisional seed or a final value (`@pop_topic_kind`). It is the gate every derivation step is checked against — the basis for seed-then-refine and for opt-in regeneration via `set_if = "always"`.
_Avoid_: topic kind, topic state

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
How pop resolves effective configuration, by an ownership/modality-first law: (1) hand-authored (user-written) config always beats runtime-generated config at any scope; (2) the user's central `config.toml` beats a repo's in-tree `.pop.toml`. Ladder, highest→lowest: `config.toml` `[repo."<path>"]` → `config.toml` global → this worktree's `.pop.toml` → the **Trunk worktree**'s `.pop.toml` (→ **Repository identity** root fallback) → runtime (`config.runtime.toml`: worktree, then trunk, then global integrations) → embedded default. Runtime is a gap-filler: to override, remove or edit the hand-authored value. Integrations (`config.toml` beats runtime skills) is preserved as the tier-1-over-tier-3 case.
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
The fuzzy-search picker in `pop worktree` for choosing, creating, or deleting git worktrees in the current repository. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base branch**, name the new branch/worktree, then `git worktree add`. Queue worktree parallelism remains the separate path where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone.
_Avoid_: Repo picker

**Base branch**:
The ref picked in the interactive worktree-create flow that the new worktree is forked from — the `git worktree add -b <name> <path> <base>` start-point. Distinct from the typed worktree name, which becomes the new branch. Shown in the name prompt as `(base: <ref>)`. A remote base (`origin/x`) yields a local tracking branch.
_Avoid_: source branch, target branch, selected branch

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

**List**:
The shared, generic scrolling-list viewport the pickers and dashboards stand on: it owns cursor, scroll, height, navigation, identity-preserving reload, and per-row drawing (the █ cursor block, quick-access prefix, padding), exposing the visible rows as strings for the caller to compose. A passive state+render module driven by the model (no key handling of its own); rows are generic with Key/Cell closures.
_Avoid_: list widget, viewport, scroller, picker (the picker is a List adapter)

**Frame**:
The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it. The hints region advertises the **Help binding** (`C-h help`) on surfaces that support a **Help overlay**.
_Avoid_: chrome, header/footer helper, Layout (that is the per-window tmux tier, a different sense)

**Help overlay**:
A modal layer listing every binding active in the current TUI surface; `C-h` toggles it (a second press closes as well as opens) and Esc also dismisses. Other keys are swallowed while it is open. Bindings shown are **contextual** — only what applies in the surface's present mode (main list, action menu, filter, modal, configure phase, etc.), with a header naming the mode. Layout and the **Help binding** live in shared `ui` infrastructure (`ui.HelpKeys`, `ui.RenderHelpOverlay`); each surface supplies only its contextual entry table so binding and render cannot drift apart.
_Avoid_: help screen, help mode, F1 screen

**Help binding**:
The house chord that opens the **Help overlay** on any list-based TUI: `ctrl+h` (displayed `C-h`). Replaces F1. Non-US keyboard layouts are out of scope for now. The **Error screen** skips the overlay — its footer hint already lists every binding.
_Avoid_: F1, C-?, help key

**Text field**:
The house single-line editable input: a Model-shaped embeddable component (rune buffer, block cursor, house prompt glyph `❯ `) hand-rolled on raw bubbletea, `ui.TextField`. It owns an emacs-style editing keymap (arrows, home/end, backspace, clear) as a default callers may preempt by intercepting their own reserved keys first. It is the single house config point for text entry, replacing the retired `newTextInput()` bubbles wrapper. Distinct from the bordered input box (`WriteInputBox`), which is chrome that wraps a Text field.
_Avoid_: text input, input field, line editor

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
An agent setup that connects a coding tool (Claude, Pi, OpenCode) to the monitor, so its pane self-reports status. Installed via `pop integrate <agent>`. This is the ONLY surviving sense of the word: pop no longer has any worktree-merge "integration" — reconciling a drained worktree branch into trunk is the human's own concern (a PR or a manual merge), and pop neither computes mergeability nor offers a merge action.
_Avoid_: Hook, plugin (when you mean the whole setup), worktree-set integration, merge reconciliation

### Workbench

**Workbench**:
A named blueprint for the shape of a whole tmux **Session** — its named windows and, within each window, a **Layout** (an explicit weighted split tree of **Pane spec**s). Defined in global config, a repo's `.pop.toml`, or a global `[repo."<path>"]` block; resolved per checkout as a most-specific-wins union by name. Instantiated into a live **Session** by `pop workbench apply` (alias `wb`). The whole-session thing, one tier above a **Layout**. Formerly called "Session template".
_Avoid_: Session template, layout (that is the per-window tier), workspace, desk, session preset

**Layout**:
The arrangement and sizing of panes within a single tmux window — pop's own weighted split tree (a window's `layout` field), the per-window tier. Keeps tmux's own word for the same scope; strictly per-window, never the whole session (that is a **Workbench**).
_Avoid_: Workbench (the whole-session tier), session template, window arrangement

**Pane spec**:
A leaf node in a **Workbench** window's **Layout**: a declaration of a pane to create — its optional name (→ pane title), command, cwd, and weight. Distinct from a **Pane**: a spec has no pane ID or attention status and carries a birth command/weight; a Pane is the live tracked result it produces when a Workbench is applied. Internal (non-leaf) tree nodes are unnamed splits (children = "rows"/"columns" over weighted children), not Pane specs.
_Avoid_: Pane (the live tracked pane), pane template, pane definition

**Preferred workbench**:
A personal, per-checkout choice of which **Workbench** auto-applies when a session is born for that checkout, skipping the create-time prompt. Stored per-worktree in **Integration runtime config** (`[workbench.preferred]`, path-keyed; set via the picker's `ctrl+w` or `pop workbench prefer`), with a coarser per-repo `preferred_workbench` default on a global `[repo."<path>"]` block. Never in `.pop.toml` — it is personal taste, not committed team config. Resolves finest-first: this worktree's entry → the **Trunk worktree**'s entry (inheritance, dynamic at open) → the repo default → none (then `pick_on_create` decides prompt vs flat). Three-valued per worktree: unset (inherit), a name, or explicit none (flat/prompt here, overriding any inherited default). A resolved value that auto-applies suppresses the create-time prompt regardless of `pick_on_create`; a stored name that no longer resolves is skipped with a warning and resolution continues.
_Avoid_: preferred layout, default workbench, preferred worktree, default worktree

**Workbench order**:
A global `[workbench] order` list that fixes the display sequence of the interactive **Workbench** lists (the `pick_on_create` create prompt and the **Preferred workbench** picker). Tokens are the literal on-screen labels: **Workbench** names plus the special options `<empty>` and `<reset>`. One flat rule: tokens named in `order` front-load in that sequence; everything unnamed follows in default order — `<empty>` leads the tail, Workbenches in resolution order, `<reset>` trails. An unresolvable name is ignored (same tolerance as a stale **Preferred workbench**). Global-only for now; per-repo ordering is deferred.
_Avoid_: workbench sort, pick order, default workbench (that is Preferred workbench)

**Empty (Workbench option)**:
The `<empty>` entry in the interactive **Workbench** lists: in the create prompt it starts a plain workbench-less **Session**; in the **Preferred workbench** picker it writes an explicit-none preference (opt this checkout out of any inherited or repo default). Angle brackets mark it as a special, non-Workbench option. The Preferred picker also offers `<reset>` — delete this checkout's entry and fall back to inheriting down the chain (distinct from `<empty>`, which is an active "none", not a "forget my choice").
_Avoid_: no workbench, no workbench (here), reset to default, none

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
no open AFK work, open HITL remains ......... AWAITING-APPROVAL ← human sign-off remains
otherwise (unfinished, none eligible) ....... BLOCKED    ← Human-blocked: HITL or undone dependency
```

(`MISSING` and `MALFORMED` sit outside this derivation — they are registration and contract faults.) Automatic selection runs READY sets in scheduler order and passes over DONE, DEFERRED, and AWAITING-APPROVAL sets; only when no READY set exists may a no-argument implement select a single unambiguous Human-blocked Task set for attended help, and only when the block is an open HITL task rather than an unresolved AFK dependency. Multiple Human-blocked Task sets are ambiguous and require an explicit target. Draining stops when its selected set reaches DONE, FAILED, BLOCKED, AWAITING-APPROVAL, VERIFY-FAILED, or DEFERRED, or when an **Agent quota pause** interrupts draining without changing task status. At a BLOCKED HITL gate, interactive runs show a **HITL gate prompt** while non-interactive runs and `--yes` preserve stop-and-advice output and never auto-start attended assistance.

**Tasks readiness**:
The **Doctor status** of the `pop tasks` command family. Because the tasks feature is aimed at Git projects and its central workflow is agent execution from a **Runtime path**, Doctor reports `pop tasks` as Blocked when no Git runtime checkout can be resolved, even if read-only status rendering could still inspect local artifacts.
_Avoid_: Workload readiness, task status availability

**Task set**:
The local `<id>/index.json` manifest and its sibling task markdown files beneath the **Task storage** `tasks/` directory, optionally alongside a co-located `prd.md` (the set's whole context in one folder; PRD-less sets are normal). A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. PRD existence remains irrelevant to task scheduling and execution — `prd.md` is optional enrichment the **Verifier** may read, never a required input.
_Avoid_: Issue set, PRD, workload

**Task set registration**:
A Task set entering the repository's **Task state** so pop may select tasks from it. Pop automatically registers discovered Task sets and reports newly registered Task sets to the user. Registration metadata and Task set artifacts remain machine-local.
_Avoid_: Import, tracking

**Task set export**:
A portable `tar.gz` archive of one or more **Task set**s' on-disk directories — each set's manifest, task markdown, **Progress record**, **Captured attempt stream**s, and any other sibling **Task artifact**s — produced for transfer to another machine or repository. The archive has one top-level directory per exported set, each named for that set's **Task identifier** and mirroring its layout under **Task storage**; a single-set export is just the one-directory case of this shape. **`pop tasks transfer export`** takes one or more bare **Task set identifier**s (repeated ids are deduped) — not a **Task target reference** with a task file. Default output: a single-set export writes `<task-set-id>.tar.gz`, a multi-set export writes `pop-tasks-<YYYY-MM-DD-HHMM>.tar.gz`, both in the current working directory; the output path may be overridden with `-o`. On success it prints the absolute path of the written archive. It resolves each source set from the current repository's **Task storage** via the same **Task project resolution** as other tasks commands. Any on-disk set may be exported regardless of derived **Task set status** — export is a filesystem snapshot, not a status gate. Export is atomic: if any requested identifier is **Missing** (its directory is absent) the whole export fails and nothing is written. It is a faithful snapshot of the sets' artifacts, not a curated planning-only subset and not the repository's **Task state** (registration order, priority).
_Avoid_: Backup, sync, bundle

**Task set import**:
Installing a **Task set export** into the current repository's **Task storage** `tasks/` directory via **`pop tasks transfer import`**, resolved via the same **Task project resolution** as other tasks commands. Import accepts a `tar.gz` path holding one or more top-level directories and requires strict archive shape: each top-level entry must be a valid **Task set** directory, with no path traversal and no absolute paths — hand-rolled or ambiguous archives are rejected before install. Import is all-or-nothing across the whole archive: it extracts every set to a temporary location and validates each against the task contract, and only if every set is well-formed and every target identifier is free does it install them — any **Malformed** set, or any identifier that still collides after disambiguation, rejects the entire archive with errors and nothing is written. By default each set installs under its archive top-level directory name; when that identifier already exists, pop applies the same chronological-prefix disambiguation as task-set creation (prepend today's `YYYY-MM-DD`, then retry with `YYYY-MM-DD-HHMM-<slug>`), and only a still-unresolved collision fails the import — the existing set is never merged or overwritten. **`--as <id>`** may supply a different identifier but is accepted only for a single-set archive; it is rejected for a multi-set archive. On success it **registers** each installed set in **Task state** with priority `0`, appended in **Task identifier** order after existing registrations — the same defaults as auto-discovery — and prints the absolute path to each installed set directory (the path **Show path** would report).
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
Resolving which checkout a whole-set drain runs in, split by trigger. A foreground **`pop tasks implement`** always targets the **current checkout**: a live **Runtime execution lock** elsewhere refuses; otherwise it rebinds to the current checkout (dropping any prior binding — an idle **managed** binding prompts to delete its worktree, an adopted/trunk one is silently re-pointed), recording a **Default binding** to current. The **Queue** instead honours bindings and directives only: an existing binding wins; else the set's **Worktree directive** provisions a managed worktree (from the **Trunk worktree**) or adopts a named one; else the set is not drainable — it surfaces as a needs-bind fault, never an invented checkout. There is no integration-target fallback. `--in-worktree` on a foreground implement provisions a managed worktree forked from the current checkout's HEAD. Single-task file runs stay current-checkout.
_Avoid_: checkout picker, runtime resolver, workspace routing

**Default binding**:
The **Worktree binding** a foreground **`pop tasks implement`** records to the current checkout it ran in, making an otherwise-unbound (or rebound) set sticky to where it last ran in the foreground. The **Queue** records no default binding: with no binding and no directive it does not drain. An operator **Bind worktree** or a Queue-honoured directive still takes precedence for Queue routing.
_Avoid_: implicit binding, auto-bind

**Worktree binding**:
A durable association between one **Task set** and one git checkout for that set's execution, recorded in shared per-repository drain state and owned by a provisioning module both **`pop queue run`** and **`pop tasks implement`** call. It is the universal per-set drain router: **Drain routing** consults bindings first, then applies the remaining precedence rules. Bindings are per-set, but a checkout is no longer owned exclusively: several sets may bind the **same** checkout (an N-sets-to-one-checkout sharing that arises when **to-tasks** writes `{ "name": ... }` for a checkout another set already uses — ADR-0115/ADR-0116); the former one-checkout-to-one-set exclusivity is retired. A binding carries a `Provisioned` bit: **managed** (pop ran `git worktree add` under its data dir) versus **adopted** (a human pointed an existing checkout at the set via **Bind worktree**, or a foreground **`pop tasks implement`** adopted its current checkout). The destructive teardown trigger for a managed checkout is **Archive** (confirm-gated), now **reference-counted** (see **Managed-worktree teardown reference count**): archiving deletes the worktree and branch only when the checkout lives under the managed-worktree root and no other non-archived set still binds that path — keyed on the checkout, not on the archived binding's `Provisioned` bit — and **Unbind worktree** never deletes. Bindings default to adopted/never-delete, so a hand-written or unrecognized binding can never trigger a directory deletion; pop deletes only what it demonstrably created. A foreground implement that rebinds a set away from an idle managed binding first prompts to delete that managed worktree. A managed binding's checkout lives at a stable path derived from the set identifier and persists across drain exits, failures, and supervisor restarts so re-spawns resume the same branch rather than forking afresh. If a binding's checkout is missing or no longer registered with git, pop refuses to spawn and directs the human to repair git state or **Unbind worktree** — it never silently re-provisions.
_Avoid_: Runtime path override, per-spawn worktree, timestamped checkout

**Managed-worktree teardown reference count**:
The rule gating destructive deletion of a pop-**managed** worktree at **Archive** (ADR-0116): pop removes the checkout and branch only when it lives under the managed-worktree root **and zero** non-archived **Task set**s still hold a **Worktree binding** to that path. Keyed on the checkout path and the live referent count — not on the archived binding's `Provisioned` bit — so among several sets sharing one managed checkout, the *last* one archived fires the confirm-gated delete, and an adopting set (whose own binding is `adopted`, never self-torn-down) can still be that trigger. Closes both failure modes of N-sets-to-one-checkout sharing: deleting a worktree out from under a still-active set, and leaking a pop-created worktree whose original managed set was archived first.
_Avoid_: 1:1 checkout ownership, provisioned-bit teardown

**Unbind worktree**:
The human act of releasing a **Worktree binding**, leaving **Task set** task statuses untouched. It is ALWAYS forget-only and never destructive: even a **managed** binding's checkout and branch are retained — only the association is dropped. The symmetric inverse of **Bind worktree**, invoked via `pop tasks unbind-worktree` with a **Task set identifier** (or `U` in the **Queue dashboard**). Refused while the set actively holds the **Runtime execution lock**. To delete a managed worktree, use **Archive** with its delete-worktree confirmation; Unbind followed by Archive is the explicit "keep the worktree, file the set" path.
_Avoid_: abandon, abandon worktree, release worktree, teardown

**Bind worktree**:
The human act of pointing an existing, human-owned git checkout at a **Task set** so a later drain targets that checkout — `pop tasks bind-worktree <set>`, run from inside the target checkout. It creates an **adopted** **Worktree binding** (pop never deletes the checkout). Symmetric sibling of **Unbind worktree**; both mutate the shared binding store and run without the daemon. Refuses to re-point a set that is already bound elsewhere without `--force`, and never re-points a set holding a live **Runtime execution lock**.
_Avoid_: adopt worktree, claim worktree, queue bind

**Dirty runtime strategy**:
Controls how task execution starts from a dirty runtime checkout. `continue` starts execution without modifying the existing dirty state; it is the default both when the option is absent and when it is present without a value, and after successful task completion the normal implementation commit intentionally includes both pre-existing and agent changes. `commit-and-continue` captures the existing dirty state in a separate implementation commit before invoking the agent. `stash-and-continue` stashes tracked and untracked changes but not ignored files, prints the stash reference when one is created, and leaves restoration to the user; an empty stash does not prevent execution. When the runtime is dirty the command always displays `git status` and the chosen strategy's effect, then requires interactive `y` confirmation; `--yes` auto-confirms, and a non-interactive run without `--yes` is rejected. Implement applies the chosen strategy once before draining its selected Task set.
_Avoid_: Clean runtime checkout requirement, automatic stash restoration

**Implementation commit**:
A commit created by the task executor from runtime-checkout changes. After successful task completion, the executor stages all runtime changes and commits them with a task-derived subject and the agent summary as body. The subject's scope names the Task set by its identifier without the timestamp prefix. Task artifacts remain local and unstaged.
_Avoid_: Task artifact update, progress record

**Task manifest**:
The `index.json` within a Task set. It remains the source of truth for task eligibility and completion. It may optionally carry set-level keys beside the `tasks` array — today `auto_drain` and the **Worktree directive** (`worktree`) — that express authoring intent consumed once at first registration into **Task state** as **Registration seed**s; those keys are not re-applied on refresh. Set-level keys must match their declared types; a non-boolean `auto_drain` or a malformed `worktree` value is a contract fault that makes the Task set **Malformed**. Planning skill **to-tasks** now *always* writes the **Worktree directive** (defaulting to the current checkout's name — ADR-0115); `auto_drain` stays opt-in, written only when the human explicitly asks for it (the `auto-drain` argument).
_Avoid_: Issue manifest, workload, dashboard

**Worktree directive**:
An optional `worktree` key in a **Task manifest**, beside `auto_drain`, declaring where the set should drain when the **Queue** runs it: `{ "managed": true }` provisions a **managed** worktree forked from the **Trunk worktree**, or `{ "name": "<worktree>" }` adopts the existing worktree of that name on this machine. It is a Queue-only seed: a foreground **`pop tasks implement`** ignores the directive entirely and binds the current checkout (see **Implement**). It is a **Registration seed** (read once into **Task state** at first registration, never re-read) and **lazy**: registration records only the intent; the first *unbound* Queue drain provisions or adopts and records a **Worktree binding**, after which the binding — and any operator **Bind worktree** — takes precedence. The portable identifier is the worktree *name* (the operator-facing label, as shown in the **Worktree picker**), never a path: a path would be machine-specific, and a name absent on a given machine is an explicit failure, not a fallback. **to-tasks** now *always* writes this key (ADR-0115): by default `{ "name": "<current-worktree-basename>" }` for whatever checkout it was invoked in — feature worktree, **Trunk worktree**, pop-**managed**, or already-bound alike, uniformly, with no guard, warning, or refusal — resolved via `basename $(git rev-parse --show-toplevel)`; the `managed`/`isolated` argument overrides it to `{ "managed": true }`. It never omits the key, inverting the former opt-in policy.
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
The status derived whenever pop surfaces a Task set — `pop tasks status`, the **Queue dashboard**, and `pop queue status` (including the daemon scan) — all through the same derivation: the manifest first, then — when Agent verification is enabled — the **Verify verdict** at each set's current work checkout (its **Worktree binding** when bound, otherwise the repo's representative checkout on machine-global views). A **Ready** set has an eligible task; a **Done** set has only done tasks and, when verification is enabled, a PASS verdict for the set's current **verification episode**; a **Failed** set has a failed task. When no AFK task is eligible and the set is not Done, Failed, or Deferred: it is **Awaiting-approval** when only a human approval task is left (Agent-verified if verification is enabled), **Verify-failed** when verification could not clear it, and **Blocked** when an open AFK task is still gated behind a human task. With verification enabled, a terminal set with no PASS in the current episode reads as NEEDS-VERIFY; once PASS exists, terminal status never regresses on later commits — only a **Verified-at SHA** annotation when HEAD differs. With verification off, status derives from the manifest alone. Pop does not persist a separate completion flag beyond the verdict cache and manifest.
_Avoid_: Pane status, persisted Task set completion

**Agent verification**:
An independent **Verifier** agent's judgment of a **Task set**'s completed AFK work. Its verdict scope is only the set's `done` AFK tasks — the prompt carries their bodies and acceptance criteria, the accumulated diff, and the optional co-located `prd.md`; open/not-`done` AFK tasks and HITL tasks (any status) are excluded so the Verifier never fails a set on work it isn't equipped to judge (a not-yet-run HITL sign-off is not an unmet criterion). Gated by user config, off by default. When enabled it fires as the tail of a **Drain**: on a DONE set, and on an **Awaiting-approval Task set** it runs *before* the terminal HITL sign-off gate — a PASS then opens that gate, so cheap agent checking precedes expensive human time.
_Avoid_: review, QA, human verification, Completion sentinel

**Verifier**:
The agent that performs **Agent verification**, resolved from an ordered fallback list (`[tasks.verify].agents`, CLI `--verify-agent`, or `default_agents`) at a pinned **Effort** (default heavy) — falling through to the next agent on an **Agent quota pause** or missing binary, exactly like the implement quota fallback. It runs in a fresh context and is chosen independently of the implementing agents so it does not grade its own work.
_Avoid_: reviewer, checker, judge agent

**Verify verdict**:
The cached result of **Agent verification** for a **Task set**, held in the **Drain** store: PASS (proceed to approval or Done), FIXABLE (findings an agent can resolve), or NEEDS-HUMAN (only a human can resolve). Rows are keyed by `(repo, set, work_sha)`; a PASS in the current **verification episode** immunizes terminal status against later commits — HEAD moving past the verified SHA does not regress DONE or AWAITING-APPROVAL, only surfaces **Verified-at SHA**. Leaving the terminal zone (**Verification invalidation**) clears the cache so a new episode needs fresh verification. Distinct from the **Captured verify run** audit trail.
_Avoid_: verify result, verification status

**Verification episode**:
One contiguous stretch during which a **Task set**'s done-AFK work composition is unchanged: AFK work complete, Agent verification, then DONE or AWAITING-APPROVAL. A PASS within the episode immunizes against post-PASS commits. The episode ends when the done-AFK composition changes — an AFK task re-opens or newly becomes done (including **Remediation task** spawn) — not on mere terminal-zone exit: HITL-only movement (skip, complete, or reopen of a HITL task) never ends it, even when the set detours out of the terminal zone (e.g. skip-HITL→DEFERRED and back). The next terminal arrival after an episode end requires fresh verification.
_Avoid_: verify generation, verification epoch, verify cycle

**Verification invalidation**:
Clearing every cached **Verify verdict** row for a `(repo, set)` in the Drain store — ending the current **verification episode** so the next completion requires fresh Agent verification. Triggered whenever a **Task transition** moves an AFK task into open or into done (a reopen restarts the episode; a manually completed AFK body was never judged), and on **Remediation task** spawn. HITL-task transitions never invalidate — the **Verifier** judges only done-AFK work. Implemented as `DELETE` of all `verify_verdicts` rows for that key (not a soft epoch). The table is a cache, not the audit trail; **Captured verify run**s remain on disk.
_Avoid_: verdict expiry, SHA staleness, verify reset, verify epoch

**Verified-at SHA**:
The work SHA recorded on the set's latest PASS **Verify verdict** in the current **verification episode**, surfaced when runtime HEAD differs — signalling "cleared at this commit, HEAD has moved since" without regressing DONE or AWAITING-APPROVAL to NEEDS-VERIFY. Shown in yellow as `verified @ <shortSHA>` (12-char prefix, matching verify output) on **`pop tasks status`** in the Details column and on the **Queue dashboard** in the main STATUS column (and in the detail-view header when a set is opened). Absent when HEAD matches the verified SHA or the set has no PASS in the episode.
_Avoid_: stale SHA, verified commit, work SHA badge

**Verification idempotency after PASS**:
Once **Agent verification** returns PASS within the current **verification episode**, no subsequent automatic **Verifier** invocation may run — including on drain re-entry at DONE after terminal HITL completion, on HEAD drift from unrelated checkout work, or when another **Task set** advanced the same checkout. The cached PASS is authoritative; the drain's terminal verify path becomes a cache lookup only. Automatic re-verification is warranted only when no PASS exists in the episode: first arrival at the terminal zone, a prior non-PASS verdict (NEEDS-HUMAN or exhausted remediation cap), or **Verification invalidation** after the set leaves the terminal zone. Explicit human force (`pop tasks verify`, HITL gate Re-verify) remains available.
_Avoid_: SHA-gated re-verify, post-HITL verify loop, verify on HEAD move

**Post-HITL verification pass**:
The structural second touch of `drainVerifyPhase` when a drain continues after terminal HITL completion moves the set to DONE. It is not a separate verification policy — it reuses the same cache-first path as the pre-HITL pass. When a PASS exists in the episode it must be a no-op (no agent spawned); only a missing or non-PASS verdict may invoke the **Verifier**.
_Avoid_: second verify, post-approval verification

**Verify verdict disposition**:
How each three-way **Verify verdict** drives what happens next. **PASS** immunizes: no further automatic **Verifier** runs in the episode (**Verification idempotency after PASS**). **FIXABLE** spawns a **Remediation task**, **Verification invalidation** clears the cache, and re-verify is mandatory after remediation drains — a deliberate loop, not a failure retry. **NEEDS-HUMAN** (or exhausted remediation cap) parks at VERIFY-FAILED; the prior non-PASS verdict warrants re-verify on the next terminal drain attempt. Explicit human force (`pop tasks verify`, HITL gate Re-verify) sits outside this automatic disposition.
_Avoid_: verify result, verification status

**Accepted verdict**:
A human-authored PASS: a person reviewed a non-PASS **Verify verdict**'s findings, judged them non-blocking, and overrode the set to verified. Stored as an ordinary PASS row (flagged human-authored, carrying the human's note), so it reuses PASS idempotency and the **Verification invalidation** rules with no change to **Verified status resolution**. The note feeds forward as *context* into later **Verifier** prompts — informing the Verifier of a known non-issue so it isn't re-flagged — but never suppresses a fresh judgment, so a later real regression at that spot can still fail.
_Avoid_: override table, verdict override, dismiss, waiver

**Verified status resolution**:
The single read-side derivation that layers **Verify verdict**s onto a manifest to produce a **Task set status** — the shared core every surface routes through (`pop tasks status`, the **Queue dashboard**, `pop queue status`/daemon scan, and the pre-approval **Drain** phase). Its inputs are a manifest, the set's current work SHA, and two verdicts: the current-at-SHA verdict and the latest-PASS verdict. It gates only the terminal zone (a DONE or AWAITING-APPROVAL manifest status): a current PASS lets the terminal status stand, any non-PASS current forces VERIFY-FAILED, an older PASS immunizes against later commits (ADR-0096) and surfaces that PASS's SHA, and no PASS in the episode regresses to NEEDS-VERIFY. It is read-only and side-effect free — the decision to *run* the **Verifier** on a cache miss belongs to the **Drain** phase, not here — so it is exercised without a store or git. Callers hold the verdicts they pass in; the resolution echoes none back.
_Avoid_: verdict gate, status gate, verify check

**Remediation task**:
An AFK task spawned to fix **Agent verification** findings — by the **Verifier** on FIXABLE (auto origin) or by a human via the **Remediate** disposition (human origin); every Remediation task carries its **Remediation origin**. **Drain** picks it up like any eligible AFK task, bounded by the per-set **Remediation depth** cap, after which the set parks at VERIFY-FAILED. Spawning triggers **Verification invalidation** of the set's cached verdicts. Findings live only as a Remediation task's body — never as annotations inside another task's spec.
_Avoid_: fix task, verification findings file, verify note

**Remediation origin**:
Whether a **Remediation task** was spawned by the **Verifier** (auto) or by a human disposition (human). Determines whether the task counts toward **Remediation depth**.
_Avoid_: remediation source, spawn cause

**Remediation depth**:
The count of consecutive auto-origin **Remediation task**s since the last human-origin one. When it would exceed the configured maximum, the **Verifier** stops spawning and the set parks at VERIFY-FAILED. A human Remediation resets the count — human intervention grants fresh auto budget.
_Avoid_: remediation count, loop counter

**In Progress**:
A presentational refinement of the **Ready** display label, shown when a Ready **Task set** either already has at least one `done` task or is currently held by a live drain (a PID-alive **Runtime execution lock**) — signalling that draining has begun or is under way. It is NOT a derived **Task set status**: schedulability is still Ready, and all scheduling and summary logic keys on the underlying Ready status, never on this label. It refines READY only — a live drain that coincides with a non-READY status (AWAITING-APPROVAL, NEEDS-VERIFY, BLOCKED) leaves that status' label untouched (needs-you outranks liveness). Rendered blue to distinguish it from a fresh Ready set (cyan); applied identically in `pop tasks status` and the **Queue dashboard**.
_Avoid_: Started, Working, In-progress status, Active

**Live-drain indicator**:
The leading `●` glyph (in the house working colour, shared with the **Monitor dashboard**'s active-pane colour) that a **Queue dashboard** row shows when its **Runtime execution lock** is PID-alive — i.e. a live drain holds the set (**Picked-up Task set**). It is the sole visual cue that a drain is live now that the DRAIN column is retired, appears across every status (so an AWAITING-APPROVAL row with a paused agent carries it), and marks that `p` (preview the working pane) can reach a pane.
_Avoid_: DRAIN column, picked-up cell, running badge

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
The single task-execution command, `pop tasks implement`, that runs tasks through the **Task executor** and dispatches by **Task target reference** shape. A `<task-set>/<file>.md` reference runs exactly that one task in the current checkout (**Execution confirmation** prompt once). A bare set identifier — or no argument, choosing the highest-priority Ready set — **drains** the set with no AFK start prompt until it reaches Done, Blocked, Deferred, Failed, or an **Agent quota pause**; mid-drain **HITL gate prompt** and **Failed gate prompt** stay interactive on a TTY. For a whole-set drain a foreground implement always targets the **current checkout**: a live **Runtime execution lock** elsewhere refuses, otherwise it binds/rebinds the current checkout (**Default binding**) and drains there, ignoring any **Worktree directive** (directives are **Queue**-only). `--in-worktree` instead provisions a **managed** worktree forked from the current checkout's HEAD, binds the set to it, and drains there. There is no automatic worktree default and no `--inline` flag — the current checkout is the baseline, and `--in-worktree` is the explicit opt-in to isolation. The interactive **Drain target picker** is a **Queue dashboard** affordance only; bare `pop tasks implement` never prompts for a target, so the Queue's spawned drains never block. Completion is silent about merging: when a drain lands Done in a worktree, pop offers no integration — the human merges or opens a PR themselves, then archives to delete the managed worktree.
_Avoid_: Run, Drain, separate one-vs-many verbs, --inline, auto-worktree default, run issue, run issues, run all, next Task set, Run PRD

**Implement run**:
One invocation of a whole-set **Implement** — from set selection to its exit. It holds at most one live **Drain** at a time and may comprise several: reaching a gate menu parks (finishes) the held Drain so the menu runs lock-free, and resuming AFK work begins a fresh one (quota waits likewise). The Implement run, not the Drain, owns the gate menus, the pre-approval verify phase, and the shared prompt reader.
_Avoid_: Drain (for the whole invocation), session, segment, drain session

**Agent preset**:
A headless agent the task executor recognizes — `claude`, `opencode`, `cursor`, `codex`, or `pi` — selected by name and optionally augmented with extra invocation arguments (e.g. `claude --model opus4.8`). Pop runs the supplied command as given, exactly as it runs a **Custom agent command**; the sole difference is recognition. Because the first token names a known agent, the **Agent adapter** appends the flags Pop owns — the output protocol governed by **Agent output mode** — after the user's arguments, then the generated prompt as the final positional argument with stdin disconnected. Appending last makes those flags authoritative: a user value for an owned flag is overridden, not rejected. Recognition is what lets Pop parse the structured stream and keep every adapter capability; augmenting a recognized preset this way is distinct from replacing the invocation with a Custom agent command.
_Avoid_: Integration

**Agent fallback**:
The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[tasks.implement].agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next only on an **Agent quota pause**; a machine-global cooldown store records quota pauses per preset, and the Queue spawns plain `pop tasks implement` so manual and queue-triggered implements share the same fallback memory. The Verifier walks a parallel list, `[tasks.verify].agents`, with the same quota fall-through (plus a missing-binary skip). When attended **Integration conflict** assistance needs an agent, it uses only the first entry of the implement list — no quota-fallback walk, no separate queue-scoped config, and no `--agent` flag on `pop tasks integrate` for now. Standalone integrate resolves the list from config only; the post-drain epilogue inherits the list already resolved for that implement invocation (including explicit `--agent` flags on implement).
_Avoid_: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation, [queue].agents, [workload] default_agents

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
Identifying from **Agent output handling** that a task attempt stopped because the agent allowance is exhausted. Detection relies on a stable headless signal; a signal from a shared provider (e.g. opencode-go) may be matched once and wired into every **Agent preset** that can emit it. A detected quota pause stops implement cleanly without retrying, leaves the task Open, preserves partial runtime changes, and does not append a progress record. It is not a Failed, Skipped, or Interrupted task. Proactively reporting remaining allowance is the separate **Agent quota reporting** concern.
_Avoid_: Agent quota reporting, failed task, skipped task

**Implement quota fallback message**:
When **Implement** runs a multi-preset **Agent fallback** list and one preset hits **Agent quota pause**, pop prints a dim line naming the exhausted preset and that it is trying the next — mirroring Verifier's `quota-paused; trying next` wording — before invoking the next preset. The provider diagnostic remains the pause reason; no separate weekly-specific banner.
_Avoid_: silent agent fall-through, verifier-only quota messaging

**opencode-go provider quota**:
The opencode.ai workspace rolling-window allowance exhausted when running `opencode-go/*` models — whether the window is five-hour or weekly. Surfaced as a stable headless error regardless of which **Agent preset** fronts the provider.
_Avoid_: pi quota, opencode preset quota, separate weekly quota concept

**opencode-go quota signal**:
One of the stable headless substrings that gate **Agent quota detection** for **opencode-go provider quota**, matched case-insensitively: `5-hour usage limit reached` or `weekly usage limit reached`. The full diagnostic line (including `429`, relative reset hint, and upsell URL) is kept as the pause reason; only a recognized substring gates detection.
_Avoid_: 429 prefix requirement, usage limit reached alone, case-sensitive match, separate weekly signal term

**opencode-go quota reset**:
When the diagnostic includes `Resets in <N>min`, pop derives `PauseResetAt` as now plus N plus the **Quota assurance offset** (two minutes). When it includes a compound hint such as `Resets in <H>hr <M>min`, the same relative sum applies over hours and minutes. When the reset phrase is absent or unparseable, pop falls back to a signal-specific backoff plus the assurance offset: one hour for the **5-hour usage limit reached** signal, one day for **weekly usage limit reached**. Wired for both `pi` and `opencode` through `agentQuotaResetAt`.
_Avoid_: exact N only, absolute clock parsing for opencode-go, configured agent_quota_retry_after as opencode-go reset fallback

**Quota assurance offset**:
A fixed two-minute buffer added on top of a provider-stated relative reset window when deriving `PauseResetAt`, so **Agent fallback** and pinned-agent cooldown fire slightly after the provider's own countdown rather than on its exact edge.
_Avoid_: retry-after, cooldown grace, reset buffer

**pi quota scan scope**:
For **opencode-go provider quota** on the `pi` preset, detection scans the full raw capture line-by-line — including plain non-JSON stdout lines — not only structured `errorMessage` fields.
_Avoid_: errorMessage-only detection

**opencode quota scan scope**:
Same as **pi quota scan scope**: the `opencode` preset scans the full raw capture line-by-line for the shared opencode-go provider matcher, not only JSON `error` diagnostics.
_Avoid_: error-event-only detection

**Agent quota pause**:
The clean stop produced by **Agent quota detection**. It leaves the current task Open and preserves its partial runtime changes, and persists the paused attempt's **Captured run**. The drain then enters **Agent quota recovery wait** rather than exiting. The resuming agent inherits the paused attempt's in-flight context the same way a resumed **Interrupted task** does.
_Avoid_: Exhausted task, Interrupted task, Failed task

**Agent quota recovery coordinator**:
The machine-global `tasks/` primitive in `pop.db` that coordinates post-quota resume. **Agent preset** cooldowns stay machine-global; **Recovery turn** scope is per **Runtime path** so unrelated worktrees may resume in parallel but only one waiter per checkout re-enters drainage at a time — preventing parked waiters from releasing the **Runtime execution lock** while another set starts committing on the same tree. **Implement** owns the wait loop; **Queue** reads the coordinator to avoid duplicate spawns but does not own turn logic (consistent with ADR-0043).
_Avoid_: queue quota scheduler, global drain mutex, bob

**Recovery waiter**:
A quota-paused drain registered with the **Agent quota recovery coordinator** while its owning process polls for a **Recovery turn**. It names the task set, exhausted preset, reset instant, and the **Runtime path** from the set's **Worktree binding** at park time. Registration claims the set and checkout against duplicate queue spawns for the duration of the wait; deregistration happens on turn taken, interruption, or process exit.
_Avoid_: quota backoff, agent cooldown entry, parked set

**Agent quota recovery wait**:
The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume at the **Quota recovery resume point**. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run. SIGINT deregisters the waiter and exits as an **Interrupted task** drain (`ExitInterrupted`); the open task and partial checkout changes are preserved.
_Avoid_: in-process sleep, quota retry loop, blocking wait, --yes-only wait

**Quota recovery resume point**:
Where **Agent quota recovery wait** re-enters work after a **Recovery turn**: the same open task for a mid-drain task-attempt pause, or the **Verifier** for a post-drain verify pause — never a completed task re-run. Any **Agent preset** invocation during implement (task attempt or verify) may trigger recovery wait; all share the same checkout-scoped **Recovery turn**.
_Avoid_: verify-only wait, task-only wait, full drain restart

**Checkout gate hold**:
A lightweight registration with the **Agent quota recovery coordinator** when implement parks at a **Failed gate prompt** or **HITL gate prompt** (runtime lock released per ADR-0067). It names the task set and **Runtime path** and blocks **Recovery turn** acquisition on that checkout until the gate session ends — resume, exit, or interrupt — so a quota waiter on another set cannot resume agent work on the same dirty tree while a human sits at a gate.
_Avoid_: runtime lock through gate, gate checkout mutex, parked drain hold

**Recovery turn**:
One granted slot to resume agent work on a given **Runtime path** after the waiter's exhausted **Agent preset** cooldown clears globally. A waiter acquires a turn only when no other drain is actively executing on that checkout, no **Checkout gate hold** is active there, and the waiter is first under **Recovery turn ordering** for that path. The turn is preset-agnostic — at most one recovery resume per checkout at a time regardless of which **Agent preset** each waiter exhausted. Parallel worktrees resume independently; the guard is against multiple sets mutating the same checkout.
_Avoid_: next shot, quota lease, recovery gate, per-preset queue

**Recovery turn ordering**:
Among **Recovery waiter**s on the same **Runtime path**, turns go to the highest **Task set priority** first; equal priority breaks FIFO by registration time. **Worktree binding** supplies the path at park time; ordering does not compare across checkouts.
_Avoid_: round-robin, jitter lottery, global FIFO, per-preset queue

**Queue pinned quota backoff**:
Retired. Pinned-agent quota pauses no longer write a daemon-state **SetBackoff**; **Recovery waiter** registration in the **Agent quota recovery coordinator** is the sole spawn-skip signal for quota-blocked sets. Queue reads recovery waiters (and live drains) only.
_Avoid_: set backed off for pinned agent cooldown, quota_backoff

**Task attempt**:
One agent invocation for a task. The task executor retries an unsuccessful task up to the implement **Task retry cap**, waiting a **Task attempt retry delay** between consecutive failures — including **Task attempt timeout** outcomes. Exhaustion marks the task Failed, records the attempt count and reason locally, and stops draining.
_Avoid_: Task set retry, task dependency

**Task attempt timeout**:
The maximum duration for one task attempt, defaulting to one hour and configurable per command. When exceeded, the task executor terminates the agent process group and preserves partial changes. The outcome is **retry-eligible** like an incomplete assessment failure: it consumes one slot of the **Task retry cap**, waits a **Task attempt retry delay** before the next try, and carries the ADR-0040 "continue" digest forward. Only after the cap is spent does the executor mark an **Exhausted task**, append a Failed progress record, and stop at the **Failed gate prompt**. Distinct from **Agent quota pause** (clean stop, recovery wait) and from **Interrupted task** (SIGINT, no progress record).
_Avoid_: Task set timeout, interruption

**Task attempt retry delay**:
The wall-clock wait inserted after a failed agent invocation and before the next try, shared by **Task attempt** retries and **Verifier** retries alike. Applies to every retry-eligible failure — for implement, incomplete assessment outcomes and **Task attempt timeout** alike; for the **Verifier**, only invocation failures (timeout, agent error, unparseable output) — not a cleanly parsed NEEDS-HUMAN or FIXABLE verdict, which is the Verifier succeeding at its job. **Agent quota pause** on implement remains a clean stop with no retry loop; on verify it still falls through to the next configured agent without a delay. The default schedule is one minute after the first failure, five minutes after the second, fifteen minutes after the third and every subsequent failure when the attempt cap exceeds three; when the configured delay list is shorter than the retry count, the last entry repeats. Attempt one still starts immediately; delays apply only between retries. The wait is a blocking in-process sleep: the **Drain** and runtime lock stay held, the pane shows a countdown, and Ctrl-C during the wait exits as **Interrupted** with no further attempt. Configurable via **Task attempt retry schedule** at `[tasks]` root; distinct from **Queue backoff** (abnormal drain exits).
_Avoid_: retry backoff, attempt cooldown, API backoff, persisted retry schedule

**Task attempt retry schedule**:
The ordered list of duration strings at `[tasks]` root (`attempt_retry_delays`) governing **Task attempt retry delay** for both implement task retries and **Verifier** retries. Omitted ⇒ `["1m", "5m", "15m"]`. An empty list ⇒ zero delay (instant retries, restoring pre-backoff behavior). Parsed like `[queue] crash_retry_delays`: each entry is one inter-attempt wait, and once the list is exhausted the last entry repeats for every subsequent retry. Distinct from **Task retry cap** and from **Queue backoff**.
_Avoid_: max-tries, crash_retry_delays, retry_after

**Task retry cap**:
The maximum started agent invocations per retry loop before giving up. A single default at `[tasks]` root (`max_tries`, default 3) applies to both implement and verify; `[tasks.implement]` and `[tasks.verify]` may each override their side independently. On implement, an explicit `--max-tries` flag wins over config. The cap is **per agent preset**: the executor retries the current preset up to the cap (with **Task attempt retry delay** between failures), then **Agent fallback** moves to the next configured preset — on implement for quota, on verify for quota or after the current preset's retry loop is exhausted. Distinct from **Task attempt retry schedule** (how long to wait between tries).
_Avoid_: max-tries flag alone, attempt count, DefaultMaxTries

**Captured run**:
Durable telemetry for one structured agent invocation — an implement **Task attempt** or a **Verifier** run — stored among **Task artifacts** as a uuid-keyed pair under `streams/runs/`: `<uuid>.meta.json` (index fields: `run_id`, `phase`, `task_id`, `task_file`, `work_sha`, `start_time`, `end_time`, `outcome`, `verdict`, `agent`, `requested_agent`) and `<uuid>.events.jsonl.gz` (timestamped raw events). Each structured adapter-mode invocation gets a new random uuid; plain-output and custom-command invocations are not recorded. Persistence is best-effort and never blocks implement or verify. The **Verify verdict** in the drain store does not point at run paths. A cache hit that reuses an existing verdict at the current work SHA runs no agent and writes no new run.
_Avoid_: Captured attempt stream (when you mean the new pair), verify log, agent output log

**Captured attempt stream**:
The superseded on-disk layout for implement telemetry: one self-contained `attempt-NNN.jsonl.gz` per invocation under `streams/<task-stem>/` with inline header, timestamped events, and footer. Pop no longer writes this layout; existing files stay in place and readers synthesize a virtual **Captured run** meta (`run_id` `legacy:<task-stem>:attempt-NNN`) so they join the unified chronological timeline. Distinct from a new uuid **Captured run** pair under `streams/runs/`.
_Avoid_: Agent output log, progress record, transcript

**Captured verify stream**:
A **Captured run** whose `phase` is `verify`: one structured **Verifier** invocation, including quota-paused fall-through attempts and runs whose output was unparseable. `work_sha` is set on meta; `verdict` is set when that invocation's text was parsed into a **Verify verdict**. Not a separate on-disk tree or filename pattern.
_Avoid_: verify log, verification transcript

**Requested agent**:
The full resolved **Agent preset** string — preset name plus extra invocation arguments, e.g. `claude --model opus4.8` — that Pop invoked for a Task attempt. Pop always knows it at invocation time, so it is recorded verbatim in the Captured attempt stream's header and printed when the attempt starts. It states what was asked for, not what ran; the model an agent actually used is the separate **Actual model**.
_Avoid_: Agent name, preset name, model

**Actual model**:
The model identifier an agent itself reported inside its Captured attempt stream (e.g. Claude's `init` event). It is a derived, per-adapter, best-effort reading at display time — never recorded as a separate event — and is absent when the agent does not report one. It may differ from the model requested in the **Requested agent** arguments through aliases or provider fallbacks. Surfaced in the Attempt timing breakdown and shown once by the live renderer when the agent reports it mid-attempt; `pop tasks status` shows at most requested-agent metadata from the manifest and never reads streams.
_Avoid_: Model time, requested model, agent

**Attempt timing breakdown**:
The agent-specific accounting of where a Task attempt's wall-clock time went, derived from its Captured attempt stream: each attempt's outcome and total duration, its read-time-derived token spend (input/output/cache, claude-first and absent for adapters that report none), and — for agents whose stream pairs a tool invocation with its result — a per-tool count and duration, followed by **Model time**. Tool figures are reported under the agent that ran the attempt because tool vocabularies differ by agent. It is the shared header rendered in two places: implement prints it as a task finishes, and **Attempt stream replay** prints it above each attempt's replayed events (ordered by attempt start time). The standalone `pop tasks timings` lens that once reprinted the per-task history is retired in favour of stream. There is no cross-Task-set rollup.
_Avoid_: Workload report, run summary, set rollup

**Model time**:
The portion of a Task attempt's total duration during which no tool was in flight — the agent itself producing output: reasoning, narration, composing edits. It is the residual after removing every interval covered by a tool invocation awaiting its result, so overlapping (parallel) tool calls are not double-counted, and a tool still running when the attempt ends counts as tool time, not Model time. It appears in the Attempt timing breakdown only when per-tool figures do, labeled `model`. It is a derived reading of the Captured attempt stream, not a recorded event.
_Avoid_: Thinking time, unattributed time, idle time, overhead

**Stream entry timing**:
The elapsed time since the previous live line, shown as a `+Xs` prefix on each rendered stream entry while implement runs. It is part of the cosmetic live side-channel and never feeds completion assessment.
_Avoid_: Tool duration, attempt timing breakdown

**Attempt stream replay**:
The `pop tasks stream TASK_SET[/FILE.md]` command — the read-only lens over **Captured run**s (and legacy **Captured attempt stream** files via synthesized metas). It supersedes and retires the earlier `pop tasks timings` lens, of which it is a strict superset. For each run it renders the full **Attempt timing breakdown** as a header (including read-time-derived token spend), then the event sequence as human- and agent-legible text. It captures nothing new and never mutates. A bare `TASK_SET` target globs every run meta under `streams/runs/` plus legacy task-stem gzips, sorts by `start_time` into one chronological timeline across implement and verify (implement before verify at equal timestamps), then replays. A `TASK_SET/<file>.md` target filters to implement runs for that task. `--last` at set scope selects the single most recent run overall; `--full` and `--raw` behave as before. Import merge of runs is out of scope — uuid pairs only enable that as a future byproduct.
_Avoid_: log, transcript, agent output log, agent output dump

**Human-blocked Task set**:
A Task set with at least one still-open AFK task that cannot run because human-in-the-loop work must happen first — the pre-agent or mid-flow end of the HITL lifecycle. It derives status BLOCKED and a `blocked` **Drain outcome**: real agent work remains, gated on a human. Contrast an **Awaiting-approval Task set**, where no open AFK work remains and only human sign-off is left. Implement reports the condition and stops; the task executor never automatically runs HITL tasks.
_Avoid_: Failed Task set, Awaiting-approval Task set

**Awaiting-approval Task set**:
A **Task set** whose AFK work is **Agent verification**-cleared (PASS) and whose only remaining open work is a human's terminal approval (a HITL task). It derives status AWAITING-APPROVAL and an `awaiting_approval` **Drain outcome** — the post-agent, pre-human end of the HITL lifecycle, where the human signs off rather than "verifies" (the agent already did). Replaces the retired Unverified Task set.
_Avoid_: Unverified Task set, pending verification, review state, Blocked Task set

**Verify-failed Task set**:
A **Task set** that **Agent verification** could not clear on its own: the **Verifier** returned NEEDS-HUMAN, or the **Remediation task** depth cap was exhausted. It derives status VERIFY-FAILED and a `verify_failed` **Drain outcome**, carries the findings, and parks (no eligible AFK work). A human dispositions it two ways: **Accept** (record an **Accepted verdict** — the set stands verified) or **Remediate** (spawn a **Remediation task** with a note). Reopen/edit/re-verify remain available.
_Avoid_: failed verification, blocked, rejected

**Verify-fail gate prompt**:
The interactive choice shown when a **Drain** reaches a **Verify-failed Task set** on a TTY — the verify counterpart of the **HITL gate prompt** and **Failed gate prompt**. It offers Accept (record an **Accepted verdict** with a note), Remediate (spawn a **Remediation task** with a note), open a **Runtime shell**, or exit; `0` is exit. Headless runs use `pop tasks verify <set> --accept` / `--remediate "<note>"` instead. Re-verify is not offered here — re-running the **Verifier** is a separate force action, not a response to findings.
_Avoid_: verdict prompt, review prompt

**HITL gate prompt**:
An interactive choice shown when implement reaches a **Human-blocked Task set**, an **Awaiting-approval Task set**, or when a ready HITL task is targeted directly (`pop tasks implement <task-set>/<hitl>.md` routes to that task's gate rather than rejecting it as non-AFK). It defaults to agent assistance while letting the human complete the task, defer it, open a **Runtime shell**, re-verify, or exit; `0` is exit. After complete or defer clears the blocking HITL task, implement refreshes the set and continues from any newly eligible AFK task. Stays interactive in a drain pane with a TTY; `--yes` skips it.
_Avoid_: Automatic HITL execution, yes/no launch prompt

**Failed gate prompt**:
An interactive choice shown when a drain reaches or re-enters a Failed task during a foreground Implement. It defaults to re-running the task while still offering agent assistance, finishing by hand, opening a **Runtime shell** in the checkout, or exit without changing task state — the Failed-task counterpart of **HITL gate prompt**. Exit is bound to the fixed key `0` (rendered last so its number never shifts as options are added). It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs. Queue-initiated drains never show this menu; a **Retriable failure** (quota) is healed by **Agent quota recovery wait**, not queue reopen.
_Avoid_: Automatic retry, Open task

**Failed drain gate**:
A set-wide hard stop during **Implement**: while any task in the set is Failed, no other AFK task runs — even open tasks with no `blocked_by` dependency on the failure. Re-entering Implement on that set must land on the **Failed gate prompt** for the first failed task (manifest order), not advance to the next eligible open task.
_Avoid_: per-task failure skip, continue past failure

**Failure reason**:
The structured why recorded on the latest **Captured run** footer for a task attempt — the durable source read by `LatestFailureReason` and the Failed assistance prompt, distinct from the human-facing `progress.txt` line. It is not persisted on the task manifest (only `failed_after` is). Harness contract verdicts (missing **Completion sentinel**, empty summary, unchecked acceptance) and agent-emitted `TASK_FAILED` text are both failure reasons; quota exhaustion is not — it produces an **Agent quota pause** while the task stays Open.
_Avoid_: failed_after, progress record line

**Queue failed recovery**:
The queue-initiated branch of failed-drain handling: never auto-reopen an **Exhausted task**. **Queue** spawn policy stays Ready-only (Failed sets are not re-spawn candidates); quota healing stays on the **Agent quota pause** path. When a queue-spawned drain hits an Exhausted task, Implement's set-wide hard stop applies and the **Failed gate prompt** runs under the same interactive/static rules as any other implement — queue adds no separate reopen logic.
_Avoid_: auto-retry, queue reopen

**Retriable failure**:
A stop **Implement** heals unattended via **Agent quota recovery wait** without a human Failed gate decision — **Agent quota pause** on a task attempt or **Verifier** invocation (task stays Open or verify re-runs after turn). Not an **Exhausted task** whose agent could not finish the work; those require human disposition via the **Failed gate prompt**.
_Avoid_: retrieval failure, auto-retry, transient failure

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
A task that remains unsuccessful after its maximum attempts. The task executor marks it Failed locally, preserves any partial implementation changes for inspection, does not commit them, and stops draining the whole set until the failure is cleared.
_Avoid_: No-op task completion, reverted task

**Interrupted task**:
A task whose active agent process was terminated by user interruption (graceful SIGINT teardown) or process termination. The task executor forwards termination to the agent process group, preserves partial implementation changes, persists the interrupted attempt's **Captured run** (so a later resume can carry its in-flight narrative forward), appends no **Progress record**, and exits without committing. An interrupted task is not Failed. A hard kill of pop itself writes no stream — that is a crashed **Drain**, and the resume then has only the checkout diff to build on.
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

**Task transition**:
The governed move of one task between the four statuses (open, done, failed, skipped) through a single chokepoint. Legality is keyed by (from, to, actor): the **Task executor** may drive only open→done and open→failed; the human — via **Complete task**, **Skip**, and **Open task** — drives open→done (clearing a HITL task is this edge), failed→open, failed→done, open→skipped, skipped→open, skipped→done, and done→open. Every transition appends a **Progress record**, maintains the recorded attempt count (set on entering failed, cleared otherwise), lands as one atomic manifest write per batch, and applies **Verification invalidation** per its trigger rule. No other writer may change a task's status.

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

**Out-of-band mutation**:
A change to a **Task set**'s verdicts or manifest made from outside a drain — e.g. the **Accept** or **Remediate** disposition issued from the standalone CLI. Permitted only under **Checkout quiescence**.
_Avoid_: external mutation, offline edit

**Checkout quiescence**:
The state of a checkout with no live drain executing and no **Checkout gate hold** registered. Precondition for any **Out-of-band mutation**.
_Avoid_: idle checkout, unlocked

**Status table**:
The non-interactive summary printed by `pop tasks status` after discovery refresh. **Archived Task set**s are excluded from the default table; when at least one exists, a quiet footer reports the archived count and the `pop tasks status --archived` command that lists them, so filed-away work stays discoverable. `--archived` instead renders only the Archived Task sets. In the default table, Missing Task sets appear first as stale registrations, followed by Done Task sets. Remaining discovered Task sets then appear in scheduler order: descending priority with stable registration order for ties, so the user can read the active schedule top-to-bottom to understand which Ready work will be selected first. The automatically selected Ready Task set is marked explicitly. Before execution, the actual implement target is also marked; when an explicit Task set override differs from the automatic selection, the table shows both markers on their respective rows. The checkout note describes where a whole-set **Implement** would run by default: the bound checkout when the set has a **Worktree binding**, otherwise the **current checkout** (a **Default binding** is recorded there on first drain; a **Worktree directive** routes only the **Queue**, not a foreground Implement). Single task-file runs are still current-checkout operations. An interactive tasks dashboard is deferred until the table workflow is exercised.
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
Read-only shell tab completion for tasks subcommands, project names, **Task target references**, agent presets, and path flags. Positional completion on Implement, Open task, Complete task, and Skip behaves uniformly, and never offers a done thing: at the set-identifier stage it offers each non-Done set as `<task-set>/` with a trailing slash and a no-space directive, so resolving one set leaves the cursor right after the slash to continue tabbing into Task-set-relative files `<task-set>/<file>.md`; the `<task-set>/` form is itself a valid whole-set target, so the operator may stop there. Done Task sets are omitted at the set stage and done tasks at the file stage, because neither is actionable by any of the four verbs; Deferred, Malformed, and every other set stays offered, and explicitly typing a done target still resolves — the filter narrows completion, not resolution. Timings completes the unfiltered target list, since Done sets are exactly what timings inspects. Set-priority and show-path complete a single bare Task set identifier (no file stage). **Task set export** completes one or more bare Task set identifiers, offering every on-disk set regardless of status and excluding sets already present on the command line; alone among completion surfaces it orders sets newest-first (reverse **Task identifier** sort, exploiting the chronological id prefix) rather than alphabetically, because transfer is a recency-driven "export the set I just made" workflow. **Task set import** has no positional completion — the archive path is a filesystem argument outside this model. **Archived Task set**s are omitted from every completion surface except **Unarchive**, whose positional completion offers only Archived Task set identifiers; explicitly typing an archived identifier still resolves for the snapshot verbs that accept it, the same way the filter narrows completion rather than resolution for done targets. Completion never offers filesystem path segments. Completion may scan Task storage but must not auto-register Task sets, persist task state, or print warnings.
_Avoid_: Shell autosuggestion, discovery refresh

**Missing Task set**:
A locally registered Task set whose manifest is no longer present beneath its Task storage. Its registration, priority, and list order are preserved in case the Task set returns. It is skipped during execution and shown before all discovered Task sets in the status table so active work remains grouped toward the end for a future terminal UI.
_Avoid_: Malformed Task set

**Archived Task set**:
A registered Task set the human has filed away with **Archive**, recorded by an `archived` flag on its **Task state** registration entry. An Archived Task set is hidden from the **Status table**, from automatic selection and draining, and from every **Task shell completion** surface except **Unarchive**; its task markdown, **Task manifest**, **Progress record**, **Captured attempt stream**s, and task statuses are untouched, so archiving is reversible. Because an Archived set is outside the verification loop, `pop tasks status --archived` lists each set at its **manifest-derived** status only, skipping the **Verify verdict** overlay — a formerly-Done set reads Done, never NEEDS-VERIFY. The one exception is a set that held a managed **Worktree binding** at archive time: its pop-created worktree and branch may be deleted by the confirm-gated teardown at **Archive**, which **Unarchive** cannot restore. Archiving is a registration-metadata decision like **Task set priority** — not a derived **Task set status** and not a task-status transition — so it appends no **Progress record**. An action verb (**Implement**, **Open task**, **Complete task**, **Skipped task** via `skip`, set-priority) refuses an Archived Task set target and points the human to **Unarchive** first; read-only snapshot verbs (**Task set export**, **Show path**, and `timings`) still resolve an explicitly typed archived **Task identifier** because they neither schedule nor mutate the set.
_Avoid_: Deleted Task set, Task set export (the tar.gz archive), Done Task set, Missing Task set

**Archive**:
The command `pop tasks archive` that files Task sets away as **Archived Task set**s. With no argument it opens a **Multi-set selection** of every non-archived registered set — Done, Deferred, Ready, Blocked, Failed, Missing, and Malformed alike — with only **Done** sets pre-checked, so the common "review the done ones and move on" pass is one confirmation. A bare **Task set identifier** archives exactly that set regardless of its **Task set status**, with no picker. Archiving a set whose checkout is a pop-**managed** worktree prompts `delete managed worktree? [y/N]` **only when this is the last non-archived set bound to that checkout** (**Managed-worktree teardown reference count**): on confirm pop deletes the worktree and branch and releases the binding; declining aborts the archive (to keep the worktree, **Unbind worktree** first — non-destructive — then archive). When other non-archived sets still share the checkout, archive is metadata-only and no delete is offered. Archiving an unbound, adopted-bound, or trunk-bound set is metadata-only and reversible as before. `--yes` skips the picker and archives precisely the Done sets — the unattended form of the default — deleting the managed worktrees of any that reach zero live referents without a further prompt. Like **Multi-task selection**, a no-argument run with no interactive TTY and no `--yes` is rejected rather than mass-mutating silently, pointing the human to `--yes` or a bare identifier. Archiving several sets is one atomic **Task state** write and appends no **Progress record**.
_Avoid_: Delete, Task set export, Remove registration, Skipped task

**Unarchive**:
The command `pop tasks unarchive` that restores **Archived Task set**s, clearing the `archived` flag so the set reappears in the **Status table**, automatic selection, and completion. With no argument it opens a **Multi-set selection** listing only Archived Task sets with nothing pre-checked; a bare **Task set identifier** restores exactly that set. Like **Archive** it touches only **Task state** and appends no **Progress record**.
_Avoid_: Restore from export, Task set import, Open task

**Multi-set selection**:
The interactive checkbox UI that **Archive** and **Unarchive** open across whole Task sets — the cross-set sibling of the within-set **Multi-task selection**. Each row is one registered Task set showing its **Task identifier** and derived **Task set status**; Archive pre-checks Done rows and lists every other status as unchecked-but-checkable, while Unarchive lists only Archived Task sets with none pre-checked. Confirming (Enter) applies the checked sets as one atomic **Task state** write; cancelling (Esc) writes nothing. Like Multi-task selection it is terminal-only — a no-argument invocation with no interactive TTY is rejected rather than mutating silently.
_Avoid_: Multi-task selection (within-set, task-level), Project picker, `--all`

**Queue**:
The scheduling concern over Task-set draining across repositories, surfaced by two drivers: the **Queue daemon** (`pop queue run`, automatic, polls and fans out unattended over **Auto-drain**-marked Ready sets) and the **Queue dashboard** (`pop queue dashboard`, manual, the primary way a human starts drains). Both schedule onto the same substrate — **Repository identity** as the scheduling unit (a repo's worktrees collapse to one unit sharing one **Task storage**, not the picker **Project**) and **Worktree binding** as the per-set drain router. The daemon dispatches at most one drain per idle repository per Ready set — never once per worktree — targeting one specific not-currently-running Ready set rather than no-argument implement; each repository drains serially by local **Task set priority** under the **Runtime execution lock** while repositories run in parallel. The Queue drains a set only where a binding or its **Worktree directive** sends it (see **Drain routing**); it records no **Default binding** and has no fallback checkout, so an unbound set with no directive is not drained but surfaced as needing a bind. Reconciling a completed worktree branch back into trunk is the human's own concern — the Queue routes execution, never merges. Global cross-project priority ordering is a non-goal.
_Avoid_: Machine-global scheduler, per-worktree scheduler

**SetRef**:
The resolved, fork-free coordinates of one registered **Task set** that the **Queue** write-path acts on: its definition/state paths, **Repository identity** (repo key and common dir), project and runtime paths, plus the per-build derived facts (parked, bound, orphaned, auto-drain, raw status). Carried, never re-resolved, so acting on a set forks no git (ADR-0060). Embedded in the dashboard row and passed as the sole input to the **Drain control** verbs, which lets those verbs run against a named set without a TUI row.
_Avoid_: DashboardRow (the presentation row that embeds a SetRef and adds display labels), Drain target (the destination a drain lands on, not the set it acts on), ResolveInput (the CWD-based address that re-resolves coordinates)

**Drain control**:
The **Queue** write-path module (`queue/draincontrol.go`): the set of mutation verbs the dashboard reaches to launch a **Drain**, bind/adopt/provision a **Worktree binding**, unpark a set, and preview — LaunchDrain, CreateWorktree, AdoptWorktree, ProvisionManagedWorktree, UnparkSet, PreviewDrain, and peers. Keyed on **SetRef**, not on the dashboard row, so the same verbs are callable from `pop queue` commands, not only the TUI. Lifted out of the dashboard model/view file so the write-path's locality is one module.
_Avoid_: dashboard actions, DashboardRow callbacks (the verbs no longer take a view type)

**Picked-up Task set**:
A Task set currently being drained, identified by a live **Runtime execution lock** that records its **Task identifier**. Picked-up state is derived from lock liveness, never persisted as a task status; tmux panes are display only, not the source of truth. On the **Queue dashboard** it surfaces as the **Live-drain indicator** and, for a READY set, the **In Progress** label refinement — not as a dedicated column.
_Avoid_: In-progress task, pane state

**Queue daemon**:
The supervisor process behind `pop queue run`. It is foreground and explicit, never auto-started from a picker, because it runs coding agents unattended across projects; the operator parks it in a pane and Ctrl-C (`SIGINT`) is graceful shutdown. It is single-instance via a PID/lock file. Unlike the **Monitor** daemon, it needs no control socket: it persists agent cooldowns and drain lifecycle to the SQLite store, from which parked sets, backoff, and the **Queue journal** are derived, so `pop queue status` and `pop queue log` are pure store readers. On `run`, it reconciles in-flight drains from live **Runtime execution lock**s, so a restart never disturbs them. Its command surface is `run`, `status`, and `log`; Ctrl-C is stop.
_Avoid_: Monitor daemon, background service

**Queue scope**:
The set of work the **Queue daemon** supervises: **Auto-drain**-marked Ready Task sets in git-backed registered projects. Running `pop queue run` is standing consent to act, but the daemon drains only sets a human has marked **Auto-drain** (default off); there is no per-project opt-in flag and no per-drain AFK start prompt. The per-set opt-in is **Auto-drain**, toggled from the **Queue dashboard**; the per-set opt-out remains **Archive**; manual `i` from the **Queue dashboard** drains a set regardless of its **Auto-drain** bit. Queue spawns plain `pop tasks implement <set>` — no `--yes` — so **HITL gate prompt** and **Failed gate prompt** stay interactive when the drain pane has a TTY. The blast radius is self-limiting because the daemon only acts on Auto-drain Ready sets, and a Task set is a deliberately authored artifact; a project with no sets is skipped. A configured **Project** with no git checkout is also outside **Queue scope** — it has no **Repository identity** and therefore no **Task storage**; the supervisor silently skips it like a project with no sets, never a scan error. When a project has no tmux session, the daemon creates one detached and splits a drain pane into that session's main window (index 0); subsequent drains split additional panes there.
_Avoid_: Per-project queue opt-in, global priority queue, per-drain --yes

**Queue journal**:
The Queue journal *view* — not a separate persisted file. `pop queue log` reconstructs the event history (started, done, failed, HITL-blocked, quota-paused-and-agent-switched, crashed, backing-off, or parked) at read time by reading each **Drain** row, integration event, and park-clear from the SQLite store; there is no append-only journal file and **Implement** emits no separate drain-outcome record. **Agent fallback** and backoff are likewise derived from that stored Drain history. `pop queue status` reads live state, such as picked-up sets, cooling agents, parked sets, and idle projects; `pop queue log` reconstructs the journal history from the store.
_Avoid_: Progress record, Captured attempt stream, Task state

**Drain**:
One supervised execution of draining a **Task set**, tracked through an explicit lifecycle from start to a terminal disposition (its **Drain outcome**). A Task set may be drained many times — after a reset, a crash, or a quota pause — and each is a distinct Drain; a set's Drain history is the ordered record of them. The Drain, not the Task set, carries execution lifecycle state; the set's manifest-derived **Task set status** (what work remains) is a separate, derived concern.
_Avoid_: Run, attempt, drain record

**Drain outcome**:
How a **Drain**'s process ended — its exit reason, not the set's work disposition: finished (the drain ran to its own stopping point), quota-paused (an agent preset hit quota), interrupted (deliberate SIGINT teardown), or crashed (the process died unexpectedly, recorded by reconciliation rather than by the drain itself). The set's resulting work disposition — done, failed, blocked, awaiting_approval, verify_failed, deferred — is read from the manifest-derived **Task set status** (now also gated on the **Verify verdict**), never restated on the Drain. The drain's terminal `state` (`finished`/`quota_paused`/`interrupted`/`crashed`/`verify_failed`) is a column on the SQLite `drains` row — there is no separate outcome journal file, and no legacy `unverified` value is read forward (that vocabulary was retired outright). finished, quota-paused, and verify_failed are clean exits; interrupted and crashed are abnormal and drive crash backoff.
_Avoid_: Task set status, drain disposition, drain result

**Queue run output**:
The live stdout of `pop queue run` — an operator-facing event stream, not a repeating inventory. It prints one **Queue run baseline** on startup (the full scheduling-relevant picture of what the supervisor is watching), then only **Queue run deltas** when something changes: spawns, terminal drain outcomes, agent cooldowns, parks, and errors. A quiet tick with no change prints nothing. Drain panes keep their own implement output; `pop queue status` remains the on-demand full snapshot.
_Avoid_: Per-tick status dump, queue log replay

**Queue run baseline**:
The one-time inventory printed when `pop queue run` starts. It opens with a **Queue status summary** — aggregate queue work (running, queued, blocked) — then lists every scheduling-relevant bucket the supervisor is watching: running drains, queued ready sets, blocked state (parked sets, crash backoffs, agent cooldowns), and scan errors for in-scope projects that failed to scan or have a broken repo-root `.pop.toml` — in the same human-readable shape as `pop queue status`. Projects outside **Queue scope** and in-scope projects with no ready work and no active drain are not listed individually; they collapse into a single count line (e.g. "12 other projects: no ready work").
_Avoid_: Per-project idle listing, repeating status table

**Queue status summary**:
The headline block at the top of `pop queue status` and the **Queue run baseline**. It aggregates current queue work — how many Task sets are running, queued for drain, or blocked. Detail lines below expand each bucket; the summary is the at-a-glance answer to "what is in the queue right now?"
_Avoid_: Daemon state JSON, per-project idle dump

**Queue run delta**:
A single stdout line emitted by `pop queue run` when supervisor-relevant state changes after the baseline. Deltas cover spawns, terminal drain outcomes (done, failed, HITL-blocked, quota-paused, crashed), set parked, agent cooldown started, cooldown or backoff cleared (work may resume), and per-project scan errors. Unchanged state — still running, still cooling, still waiting — prints nothing.
_Avoid_: Heartbeat line, per-tick inventory repeat

**Queue backoff**:
The daemon's response to an abnormal drain exit, such as crash, kill, or interrupt. Unlike a clean failure or quota pause, an abnormal exit leaves the set Ready with nothing cooled and would otherwise re-spawn immediately. The daemon applies an escalating per-set delay and, after N consecutive abnormal exits, parks the set until a human clears it. A clean exit resets the counter. Distinguishing abnormal (crash/interrupt/kill) from clean (finished/quota-paused/verify-failed) exits reads the **Drain**'s terminal `state` directly (`store.drainStateAbnormal`); backoff and park are projected from that Drain history plus the `park_clears` table.
_Avoid_: Failed task, Agent quota pause

**Spawn deferral**:
The read-side answer to why a Ready set is not being spawned right now: a reason plus an optional until-instant. Three species — **Queue backoff** crash backoff (timed), Parked (indefinite, human-cleared), and **Agent quota recovery wait** (owned by the paused process). One vocabulary over deliberately separate mechanisms.
_Avoid_: spawn hold, pause, suppression, block

**Queue window**:
The single tmux window, named `pop-queue`, that the Queue daemon spawns its drains into within a Project's session. All queue-spawned drains for that project — both in-place and **Worktree set** — land here as panes under a balanced (`tiled`) layout, instead of in the user's working windows or in per-worktree sessions. One Queue window per project session; created on first spawn, reused thereafter.
_Avoid_: Drain session, worktree session, queue tab

**Auto-drain**:
A per-set persisted consent bit in **Task state**, alongside priority and the archived flag, marking that the **Queue daemon** may automatically drain this **Task set**. It defaults off for a freshly-discovered set, inverting the old standing-consent model: `pop queue run` drains nothing until a set is marked auto-drainable — from the **Queue dashboard** (`a`), from the **Auto-drain command** (`pop tasks auto-drain`), or by a human launching it by hand. A **Task manifest** may declare `"auto_drain": true` at the set level; pop reads that key once at first registration — whether via lazy discovery, import, or any other path that creates the registration entry — and seeds Task state accordingly; it does not re-sync on later refresh, so the **Queue dashboard** toggle and the **Auto-drain command** remain authoritative after registration. The bit also auto-clears — see **Auto-drain clearing** — once a drain leaves the set with all AFK work drained (DONE or AWAITING-APPROVAL), so a finished set stops carrying its auto-drain marker; re-enabling is a fresh human mark. It is orthogonal to **Archive** (which hides a set entirely), distinct from a **Picked-up Task set** (a runtime live-lock fact, not consent), and distinct from the **Run-next badge** (`NEXT`, a local-runner display marker that shares the word "auto" only in the retired `AUTO` badge — they are unrelated).
_Avoid_: Pickable, pick-up status, auto-pickup, queue-enrolled

**Auto-drain command**:
The non-TUI CLI act of setting a registered **Task set**'s **Auto-drain** consent bit: `pop tasks auto-drain <set>` enables it (idempotent — re-running never flips it back off), and `--off` disables it. Sibling to `pop tasks set-priority`/`archive`: it resolves and auto-registers an on-disk set the same way, **rejects** an **Archived Task set** (pointing the human at `pop tasks unarchive`), and mutates **Task state** silently with no trunk-checkout warning — symmetric with the **Queue dashboard** `a` toggle. Unlike that toggle it is explicit on/off rather than a flip, so it is safe to run from scripts. Per-**Task set** only: it takes a bare **Task set identifier**, never a `<set>/<file>.md` reference (there is no per-task auto-drain granularity).
_Avoid_: auto-drain toggle (that is the dashboard flip), enable-auto-drain / disable-auto-drain, per-task auto-drain, queue auto-drain

**Auto-drain clearing**:
The automatic flip of a set's **Auto-drain** bit from on to off when a drain finalizes with the set's derived status DONE or AWAITING-APPROVAL — the two states in which all AFK work is drained and the daemon has nothing left to do. It fires only from a finishing drain (whether launched by hand or by the **Queue daemon**), never from a background reader and never from a manual **Complete task** that reaches a terminal status with no drain (an accepted gap, since the human is present to toggle). It is idempotent, announced when it happens, and left as a durable per-set trace. Because it discards consent rather than merely hiding the marker, a later **Open task**, **Remediation task**, or **Verification invalidation** does not auto-re-fire the daemon — a human must re-mark **Auto-drain**. Chosen over suppressing only the ` · auto-drain` **Queue dashboard status suffixes** marker so persisted consent matches what the dashboard shows.
_Avoid_: auto-drain reset, consent expiry, AD auto-off, auto-drain revoke

**Queue dashboard**:
The interactive `pop queue dashboard` TUI — the primary hands-on surface for starting and managing **Queue** work, sibling to the **Project picker** and **Worktree picker**. Machine-global like `pop queue status`, it scans every registered repository's **Task storage** and renders one row per non-archived **Task set** with outstanding queue-actionable state, plus **Done Task set**s that still hold a managed **Worktree binding** (a clean-up reminder until archived or unbound). Default: single-line rows led by a **Live-drain indicator** cell, then columns PROJECT, TASK SET, STATUS, WORKTREE; elastic columns shrink on narrow panes. There is no DRAIN column and no FLAGS column — a live drain shows as the leading indicator and as the STATUS **In Progress** refinement; **Auto-drain**, orphaned, parked, and config-error state show as **Queue dashboard status suffixes** on the STATUS label. When **Queue dashboard two-line mode** activates, all rows switch to the two-line layout. Keys: `i` opens the **Drain target picker** for an unbound set then drains, or resumes silently in the bound checkout for a bound one; `b` bind or create a worktree in advance (without draining); `U` unbind (forget-only); `a` toggle **Auto-drain**; `p` preview the working pane; `ctrl+g` open the bound checkout in pop; `gg`/`G` move to top/bottom; `h`/left/`esc` back or exit; `l`/Enter open the **Task set detail view**. The former `I` integrate key is removed; `q` and the former `s` shortcut are intentionally unbound.
_Avoid_: Queue picker, queue status table, drain dashboard

**Queue dashboard two-line mode**:
A uniform layout for the **Queue dashboard** task-set table when single-line rows are too cramped. Activates when **either** the terminal width is below **80 columns** **or** any visible row's **Task set identifier** exceeds **36 characters**; when on, **every** row renders as two lines (not per-row variable height). Line 1 leads with identity: PROJECT, TASK SET (the set id), WORKTREE. Line 2 holds the **Task set status** (with its **Queue dashboard status suffixes**), indented under the TASK SET column. Distinct from cursor-row-only expansion and from the default single-line table with column fitting.
_Avoid_: wrap mode, multiline rows, stacked layout

**Queue dashboard status suffixes**:
Plain-text markers appended to a **Queue dashboard** row's derived **Task set status** label — ` · auto-drain` when the set's **Auto-drain** bit is on, ` · orphaned` when its **Worktree binding** points at a missing checkout, ` · parked` when abnormal backoff has parked the set, and ` · config error: <msg>` when a bare repo declares no trunk to route to. Shown in every status (not just READY), in both single-line and two-line rows, built where the yellow `verified @ <shortSHA>` suffix is. Uncoloured, unlike that verify suffix. The parked and config-error suffixes absorb what the retired DRAIN column used to carry.
_Avoid_: FLAGS column, AD badge, OR badge, auto-drain badge

**Drain target picker**:
The interactive chooser the **Queue dashboard** opens on `i` for an **unbound** **Task set**, fusing target selection with the drain into one bind-and-start action. It lists the repo's existing **non-managed** worktrees (pick → adopt as an adopted **Worktree binding**), a "new managed worktree" option (provision a managed binding forked from the **Trunk worktree**; the default cursor), and the **Trunk worktree** itself (drain inline, no binding). The chosen target is bound and then drained immediately. A set already holding a binding skips the picker and resumes in its bound checkout — retargeting requires **Unbind worktree** first. Options requiring a trunk (new managed worktree, trunk) are absent when no trunk is resolvable (an unconfigured bare repo). Managed and already-adopted worktrees are excluded from the existing-worktree list — a curated safe choice for the interactive path, *not* an invariant: the manifest **Worktree directive** path can still bind a set to a shared checkout (see **Worktree binding** and **Managed-worktree teardown reference count**).
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
`.pop.toml` (flat, in the repo) and `[repo."<path>"]` (central, in global `config.toml`) decode ONE shared repo-scope key schema: authoring a repo-scoped setting in either place is equivalent, and adding a new repo key makes both accept it. Per the **Config merge order**, the user's central `config.toml` (including its `[repo]` blocks) outranks the committed `.pop.toml`. Repo scope is a curated set of genuinely repo-specific keys (`workbenches`, `preferred_workbench`), never a mirror of global config — `projects`, `queue`, and daemon knobs stay global-only. `trunk` is the one central-only exception (per-checkout machine topology, never in `.pop.toml`).
_Avoid_: project entry override, glob-scoped behaviour

**In-tree config anchors**:
How pop finds repo-scope in-tree config (`.pop.toml`): at two anchors — this worktree and the **Trunk worktree** (falling back to the **Repository identity** root for a bare repo). Presence decides: a worktree with its own `.pop.toml` overrides the inherited trunk one; a worktree without inherits trunk's, dynamically. Reuses the trunk resolver of **Preferred workbench** inheritance.
_Avoid_: pop.toml inheritance, config walk, trunk snapshot

**Trunk worktree**:
A repository's single canonical fork base for managed **Worktree set**s. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must declare one explicitly via a `trunk = true` per-checkout **Repo override**. Managed worktrees fork from the trunk's HEAD; reconciling a completed worktree branch back into trunk is the human's own concern, not something pop does. An unconfigured bare repo has no trunk, so pop cannot provision a managed worktree there — it can only drain in place in whatever checkout the operator is currently sitting in.
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

**Config show**:
`pop config show`: prints the effective configuration as pop resolves it from the current directory — includes merged, repo keys canonicalized to absolute realpaths, folder-local overrides (`.pop.toml` + the current `[repo]` block) collapsed into effective values, and the current repo's resolved **Trunk worktree** (config-declared *or* git-derived) surfaced as an effective `trunk`/`bare`. Run outside any repo, the current-repo/trunk section is absent. Effective values only, no provenance annotation. TOML by default, `--json` for machines. Reaches config + git (for the derived trunk), never the task-binding store. The value counterpart to `pop config keys` (the accepted schema); renders the result of config resolution.
_Avoid_: config dump, config export

## Deprecated aliases

Removal of all deprecated aliases is gated on beta-tester sign-off, not a version number (inventory and checklist in CLEANUP.md).

- `idle`, `read` → **Clear**
- `needs_attention` → **Unread**
- `issue` → **Task**; `Issue set` → **Task set**
- `pop workload` (command family) → **`pop tasks`**; the umbrella term "workload" is retired — say "the repository's Task sets" or name the specific concept
- `run-issue`, `run-issues` → **Implement** (`pop tasks implement`); the one-task and whole-set verbs merged into one command that dispatches by target shape
- `reset-issue` → **Open task** (`pop tasks open`); `complete-issue` → **Complete task**; `skip-issue` → **Skip**
- `to-issues` (skill) → **to-tasks**; `run-one` (skill) → **run-task**
- `to-tasks-here-and-now` (skill), `Here-and-now` → removed; **to-tasks** now always writes the **Worktree directive** (defaulting to the current checkout's name), so there is no separate here-and-now mode (ADR 0115)
- `workload definition path`, `thoughts/issues` → **Task storage**
- `workload artifact ignore coverage` → removed; Task storage lives outside the repository tree (ADR 0039)
- `Queue base`, `queue_base`, `Execution base`, `execution_base` → **Trunk worktree**, `trunk`
- `Worktree-ready project`, `worktree_ready` → removed; there is no repo-capability auto-managed-worktree default — worktree execution is explicit via a **Worktree directive** or `pop tasks implement --in-worktree`
- `Integration backlog`, `Integration target`, `Mergeability`, `auto_merge_clean` → removed; pop no longer does worktree-merge integration — reconciling a drained branch into trunk is the human's own concern (ADR 0070)
- `Unverified Task set`, `UNVERIFIED` → **Awaiting-approval Task set** (Agent-verified, awaiting human sign-off) or **Verify-failed Task set** (ADR 0087)
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
