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
The sanitized tmux identifier pop uses to refer to a **Session**. Each checkout path has exactly one session name, built the same way everywhere from git repo context — not from config or picker display labels. **Every linked worktree carries its repository prefix**, `repoName/worktreeFolderName`, whether the repository is bare or not; a repository's own main checkout keeps its bare directory name (never `repo/repo`). The prefix comes from the worktree's git common dir, so a **Managed** worktree reads `repoName/<setID>` and dedupes against the entry `pop project` lists for it. When the path is not a git checkout, the directory base name. Dots and colons are replaced for tmux compatibility. Works for any checkout path pop can resolve, including paths outside configured projects. **Standalone sessions** use tmux's existing name as-is.
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

**Spawn window**:
The single tmux window, named `pop-spawn`, that every named pane created by `pop pane create` lands in within a Project's session — spawned agent CLIs and long-running processes alike, tiled alongside each other. Sibling of the **Queue window**: one per project session, created on first spawn, reused thereafter. Supersedes the window formerly named `agent`; live `agent` windows are left where they are rather than renamed or adopted.
_Avoid_: agent window, agent tab, spawn tab, pane window

**Repository display label**:
The label shown for a repository on machine-global **Queue** surfaces (the **Queue dashboard** PROJECT column, `pop queue status`, daemon output) — the depth-aware picker display name with the trailing worktree segment removed, so a bare repo reads `game server` (not `game server/main`) while a `display_depth = 2` repo still reads `work/game server`. Derived by `repoName()` from `ProjectLabel`, the pre-suffix `displayName` captured at project expansion. It denotes the repository (worktrees collapse to one **Repository identity**), distinct from the trunk **Worktree** shown in the WORKTREE column and from the git-identity basename (`RepoLabel` / `repoLabelFromScan`) used for keying and binding paths. The **Project picker** deliberately keeps the full `game server/main` — there each worktree is its own row (ADR-0117).
_Avoid_: repo label, RepoLabel (that is the identity basename, not this display value)

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
The embedded skills that teach an agent to drive `pop pane` — driving panes (`tmux-pane`) and spawning another agent CLI into one (`spawn-agent`). Installed together via the **Integration component id** `pane-skills`, each resolved under the **Skills prefix**. Still selected in config via the **Integration skill alias** `pane`. An opt-in **Integration component**; pane monitoring works without it.
_Avoid_: Agent integration, hooks

**Task planning skills**:
The embedded, pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (batch-grill-me, grill-with-docs, grill-with-map, to-spec, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass), Tool skills (prototype, research — model-invoked, verbatim upstream), and the Setup skill (setup-matt-pocock-skills — session-shaped, manual-invocation-only, prepares a repo for the others). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.
_Avoid_: Workload framework, workload skills bundle, agent integration

**batch-grill-me**:
The interview primitive both grilling skills compose: design tree, frontier, rounds, find-facts-yourself. A verbatim upstream overlay that reads the glossary union and writes nothing at all; every pop addition is a composition concern belonging to the composing skill, so a session loading it has no write instruction to disobey.
_Avoid_: grilling primitive, base grill

**grill-with-docs**:
The standalone grilling skill: **batch-grill-me** plus the domain-modeling write discipline (glossary fragments, numbered ADRs) and commit-on-close. Composed over the primitive rather than inlining the interview rules, so its verbatim upstream region is the domain-modeling half alone. Never loaded by a wayfinding ticket — its contract mandates repository writes.
_Avoid_: grill-me, the grilling skill

**grill-with-map**:
The grilling skill a wayfinding ticket loads: **batch-grill-me** plus the wayfinding answer discipline (ADR-shaped answers, **ADR draft**s and **Context draft**s, prototypes to the Map's scratch directory). Writes only into the Map — never the repo, never a commit.
_Avoid_: grill-in-map, wayfinder grilling

**Shared skill document**:
A pop-owned companion document that more than one embedded skill depends on, held once under `integrate/skills/pop/_shared/` and copied into each consuming skill's installed directory at install time — only the destination differs per skill. `CONTEXT-FORMAT.md` (the glossary union rule and the `+`/`~`/`-` op syntax, read by batch-grill-me and written against by grill-with-docs and grill-with-map) and `ADR-FORMAT.md` (the ADR template, used by grill-with-docs for a numbered repo ADR and by grill-with-map for an unnumbered **ADR draft**) are the first two. Distinct from an ordinary companion file, which lives in its one skill's own directory; a shared document that goes missing or drifts from its source is a **Doctor** finding like any other rendered file.
_Avoid_: shared skill, common file, skill include

**Skills prefix**:
The configurable string prepended to an embedded skill's base name to form its installed name (`<prefix><base>`). Set via `skills_prefix` in `[integrations]`, default `pop-`; an empty value installs skills under their bare base name. The prefix reaches skill *bodies* too: render rewrites cross-skill references (the known embedded base names) to their resolved installed names, so a rendered skill never tells an agent to run a skill under a name that isn't in its listing. Embedded sources stay byte-intact — the rewrite happens only at render, keeping upstream-drift diffs clean.
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
How pop resolves effective configuration, by an ownership/modality-first law: (1) hand-authored (user-written) config always beats runtime-generated config at any scope; (2) the user's central `config.toml` beats a repo's in-tree `.pop/config.toml`. Ladder, highest→lowest: `config.toml` `[repo."<path>"]` → `config.toml` global → this worktree's `.pop/config.toml` → the **Trunk worktree**'s `.pop/config.toml` (→ **Repository identity** root fallback) → runtime (`config.runtime.toml`: worktree, then trunk, then global integrations) → embedded default. Runtime is a gap-filler: to override, remove or edit the hand-authored value. Integrations (`config.toml` beats runtime skills) is preserved as the tier-1-over-tier-3 case.
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
**Integrate outcome line**s group by agent (existing configured agent order). Within an agent: status wiring first, then file-based skills in embed catalog source order (`tmux-pane`; then `grill-with-docs`, `grill-consolidate`, `to-spec`, `to-tasks`). For each embed base, emit any **Stale skill removal line** for superseded resolved names immediately before that base's current line — so `pop-grill-consolidate  removed (stale)` sits next to `grill-consolidate  updated`, not in a separate trailing block.
_Avoid_: alphabetical integrate output, sort by label

**Agent integration profile**:
The per-agent record of how each **Integration component** is wired for one agent: its status-wiring install, removal and detection behaviour, its **Agent install path** roots for file-based components, and the legacy artifacts to prune. One profile per supported agent (claude, codex, cursor, pi, opencode); the profile is what makes a JSON-hook agent and a file-drop extension agent interchangeable to the rest of integrate. Distinct from an **Agent integration**, which is the wiring actually installed on a machine.
_Avoid_: agent adapter, agent support matrix, agent catalog

**Agent proceed verdict**:
The one answer every **Agent adapter** gives to "can you carry on?", carried on a result shape shared by all adapters so the orchestrator never reads provider text. Absent means yes. Present, it means the agent cannot do the work at all — as distinct from an attempt that ran and failed — and says at what **Agent proceed scope**, with what **Agent proceed recovery**, a reset instant when one is known, and whether the attempt is charged to the **Task retry cap**. **Agent quota pause** is one flavour; an **Agent authentication failure** is another, as are a binary missing from PATH and a plan gate. Detected on two channels — a passive read of the capture pop already consumes (like **Agent quota detection**), and an active **Agent availability probe**. The passive channel catches a session that lapsed mid-drain; the probe catches one already lapsed on arrival.
_Avoid_: agent unavailability, unusable agent report

**Agent proceed scope**:
How much of an agent an **Agent proceed verdict** condemns. *Preset* condemns the whole entry in the **Agent fallback** list — one adapter, one CLI, one login: it abandons the remaining **Task retry cap** for that preset, hands the turn to the next preset, and is the only scope that reaches the preset cooldown store and **Agent quota recovery wait**. *Model* condemns only the token the **Effort ladder** tier resolved, leaving the CLI healthy. Dispatch reads the scope rather than the flavour, so a new flavour lands without editing the orchestrator; every shipped detector answers *preset*.
_Avoid_: verdict kind, unavailability kind

**Agent proceed recovery**:
What would make the condemned **Agent proceed scope** usable again, carried on the **Agent proceed verdict**. Time-healing carries a reset instant and drives **Agent quota recovery wait**; human-healing carries no instant and must never enter that wait, because polling cannot resolve it; permanent never heals for this account and scope, so there is nothing to wait for and no expiry worth recording.
_Avoid_: retry policy, backoff kind, agent unavailability recovery

**Agent authentication failure**:
The human-healing **Agent proceed verdict** flavour: the agent CLI refuses to run because its session is absent or expired and asks the operator to log in. Confirmed shape for `cursor` (2026-07-29): the message arrives as plain text on stderr with an empty stdout — no structured stream at all — and the process exits 1.
_Avoid_: agent quota pause, unauthorized error, API key error

**Agent availability probe**:
The active half of **Agent proceed verdict** detection: a short read-only command an **Agent adapter** may expose alongside its attended-assistance capability, asking its CLI whether it is authenticated. Run lazily the first time a preset is reached in an **Implement run** and memoised one-way for that run — a preset marked unavailable stays skipped until the process ends. Only an explicit positive counts (`cursor-agent status --format json` → `isAuthenticated`, `claude auth status` → `loggedIn`, `codex login status`); a non-zero exit, unparseable output, or a timeout reads as *unknown*, and unknown proceeds to invoke the agent, because a probe must never block real work on its own parse failure. `pi` and `opencode` expose no status readout and have no probe. A probe is not an agent invocation and writes no **Captured run**. Distinct from **Agent catalog** availability, which is a PATH lookup and never execs.
_Avoid_: agent health check, auth preflight, agent status command, doctor check

**Workflow skill**:
An embedded skill that is a session-shaped workflow the user opens deliberately: manual-invocation-only via `disable-model-invocation` — batch-grill-me, grill-with-docs, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder. The counterpart of a Tool skill; the two kinds together make up the Task planning skills.
_Avoid_: command skill, manual-only skill

**Tool skill**:
An embedded skill that is a general-purpose instrument, not a session workflow: model-invoked (no `disable-model-invocation`), so it auto-triggers when the conversation shape matches — prototype and research, adopted verbatim from upstream. Callers such as the wayfinder skill compose tool skills by naming them; caller-side packaging rules (where the output lands — e.g. a Decision ticket's `## Answer`) live in the caller, never in the tool itself.
_Avoid_: helper skill, sub-skill, wayfinder component

**Setup skill**:
The embedded `setup-matt-pocock-skills` Workflow skill, kept under its upstream name to credit the flow's origin: a manual-only session that prepares a repository for the Task planning skills. It authors `docs/agents/issue-tracker.md` (skipped by default when the Work store choice is pop — an absent repo doc defers to each machine's seeded `work-store.md`; a committed one pins the choice for the whole team), sets up the domain-docs layout and `docs/agents/domain.md`, and adds an Agent-skills block to CLAUDE.md/AGENTS.md so repo-resident agents discover these files. It never scaffolds `.pop/`, and its triage-labels section is negated (pop ships no triage skill).

**Work store**:
The destination where planning skills publish their artifacts — task sets, specs, wayfinder maps and tickets, and future artifact kinds such as prototype data — together with that destination's vocabulary for expressing blocking edges and grabbing work. A repository resolves to exactly one Work store; pop's own **Task storage** backs the built-in default, and real trackers (GitHub, GitLab, local markdown, freeform) are alternative Work stores a repository may configure. Distinct from **Agent adapter** (the bridge to an agent CLI) and narrower than it sounds from "tracker": a Work store need not track anything, only hold published work.
_Avoid_: tracker, issue tracker (as the abstraction's name), task store, task storage adapter

**Work store doc**:
The per-operation document that adapts a planning skill's publish step to one **Work store** — sections such as publishing a spec, publishing tickets, and wayfinding operations, including any store-specific drafting vocabulary (e.g. effort and HITL/AFK for the pop store). Resolution is two-layer: the repo-level doc at `docs/agents/issue-tracker.md` (upstream tracker-doc convention, unchanged) wins when present; otherwise skills read pop's own doc, a **Shipped asset** at `${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md` which Integration refresh rewrites whenever it differs from the embedded copy. There is no machine-global user override: a user who wants different publish behaviour writes the repo doc.

**Shipped asset**:
A static document or file whose correct content is determined by the pop binary, not by the user — it is installed into pop's data dir and refreshed from the embedded copy whenever the two differ, so it always describes the binary that is installed. The user's config dir holds only hand-authored files: pop writes there only from a command whose purpose is editing config at the user's request, so a Shipped asset never lands there.
_Avoid_: seeded doc, machine-global config, static config

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
A raw tmux pane identifier used as an explicit command target, such as `%63`. A Pane ID target is global within tmux and bypasses Pop's name-based **Spawn window** lookup.
_Avoid_: Pane name, dashboard label

**Quick selection**:
A numeric shortcut for selecting a visible picker row relative to the cursor. Project and worktree pickers already expose quick selection; the **Dashboard picker** uses the same idea for fast target choice.
_Avoid_: Quick filter, fuzzy search

**List**:
The shared, generic scrolling-list viewport the pickers and dashboards stand on: it owns cursor, scroll, height, navigation, identity-preserving reload, and per-row drawing (the █ cursor block, quick-access prefix, padding), exposing the visible rows as strings for the caller to compose. A passive state+render module driven by the model (no key handling of its own); rows are generic with Key/Cell closures.
_Avoid_: list widget, viewport, scroller, picker (the picker is a List adapter)

**Frame**:
The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, status, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Render is bottom-anchored: the body is padded to its full budget, so trailing regions (warnings, status, hints) sit at the terminal bottom even when the body is short — an empty-list hint no longer pulls the status line up under the header. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it. The hints region advertises the **Help binding** (`C-h help`) on surfaces that support a **Help overlay**.
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
The persisted record of projects you've switched to, with timestamps. Recorded only when a Switch (or window open / cd-to-pane landing) actually happens — a picker selection abandoned before that point (e.g. Esc at the Workbench prompt) leaves no entry.
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

**Yank**:
Sending a picker's selected value into a target tmux pane as typed input (`--yank-target <pane>`, SendKeys without Enter) rather than writing it to the system clipboard — the project and worktree pickers' delivery mode when the caller names an origin pane. Distinct from a **Clipboard copy**: the value lands at a shell prompt ready to edit, not in a paste buffer.
_Avoid_: copy, paste, clipboard

**Clipboard copy**:
Writing a value to the system clipboard through the shared tmux/OSC52 helper — tmux `load-buffer` inside tmux, an OSC 52 escape to `/dev/tty` otherwise. The delivery mode every dashboard copy verb uses, because a dashboard opened in a tmux popup has no origin pane to **Yank** into.
_Avoid_: yank, pbcopy

### Workbench

**Workbench**:
A named blueprint for the shape of a whole tmux **Session** — its named windows and, within each window, a **Layout** (an explicit weighted split tree of **Pane spec**s). Defined in global config, a repo's `.pop/config.toml`, or a global `[repo."<path>"]` block; resolved per checkout as a most-specific-wins union by name. Instantiated into a live **Session** by `pop workbench apply` (alias `wb`). The whole-session thing, one tier above a **Layout**. Formerly called "Session template".
_Avoid_: Session template, layout (that is the per-window tier), workspace, desk, session preset

**Layout**:
The arrangement and sizing of panes within a single tmux window — pop's own weighted split tree (a window's `layout` field), the per-window tier. Keeps tmux's own word for the same scope; strictly per-window, never the whole session (that is a **Workbench**).
_Avoid_: Workbench (the whole-session tier), session template, window arrangement

**Pane spec**:
A leaf node in a **Workbench** window's **Layout**: a declaration of a pane to create — its optional name (→ pane title), command, cwd, and weight. Distinct from a **Pane**: a spec has no pane ID or attention status and carries a birth command/weight; a Pane is the live tracked result it produces when a Workbench is applied. Internal (non-leaf) tree nodes are unnamed splits (children = "rows"/"columns" over weighted children), not Pane specs.
_Avoid_: Pane (the live tracked pane), pane template, pane definition

**Preferred workbench**:
A personal, per-checkout choice of which **Workbench** auto-applies when a session is born for that checkout, skipping the create-time prompt. Stored per-worktree in **Integration runtime config** (`[workbench.preferred]`, path-keyed; set via the picker's `ctrl+w` or `pop workbench prefer`), with a coarser per-repo `preferred_workbench` default on a global `[repo."<path>"]` block. Never in `.pop/config.toml` — it is personal taste, not committed team config. Resolves finest-first: this worktree's entry → the **Trunk worktree**'s entry (inheritance, dynamic at open) → the repo default → none (then `pick_on_create` decides prompt vs flat). Three-valued per worktree: unset (inherit), a name, or explicit none (flat/prompt here, overriding any inherited default). A resolved value that auto-applies suppresses the create-time prompt regardless of `pick_on_create`; a stored name that no longer resolves is skipped with a warning and resolution continues.
_Avoid_: preferred layout, default workbench, preferred worktree, default worktree

**Workbench order**:
A global `[workbench] order` list that fixes the display sequence of the interactive **Workbench** lists (the `pick_on_create` create prompt and the **Preferred workbench** picker). Tokens are the literal on-screen labels: **Workbench** names plus the special options `<empty>` and `<reset>`. One flat rule: tokens named in `order` front-load in that sequence; everything unnamed follows in default order — `<empty>` leads the tail, Workbenches in resolution order, `<reset>` trails. An unresolvable name is ignored (same tolerance as a stale **Preferred workbench**). Global-only for now; per-repo ordering is deferred.
_Avoid_: workbench sort, pick order, default workbench (that is Preferred workbench)

**Empty (Workbench option)**:
The `<empty>` entry in the interactive **Workbench** lists: in the create prompt it starts a plain workbench-less **Session**; in the **Preferred workbench** picker it writes an explicit-none preference (opt this checkout out of any inherited or repo default). Angle brackets mark it as a special, non-Workbench option. The Preferred picker also offers `<reset>` — delete this checkout's entry and fall back to inheriting down the chain (distinct from `<empty>`, which is an active "none", not a "forget my choice").
_Avoid_: no workbench, no workbench (here), reset to default, none

**Cell budget**:
A container node's cell extent along its split axis minus the N-1 border cells its N children consume — the amount actually apportionable among those children. Distinct from the container's own size: tmux charges one cell per split to the surviving pane, so a Layout that apportions the raw extent overruns by exactly the border count and the tail child absorbs the deficit.
_Avoid_: pane size, container size, available space, total size

**Apportionment**:
The single derivation of a Layout container's child weights into exact cell counts against its Cell budget, using largest-remainder so the counts sum to the budget and leftovers land deterministically rather than on the last child. Both realization phases consume one Apportionment: the splits pass each child's count as tmux `-l`, and the correction pass resizes to the same counts. Weights are never turned into geometry twice.
_Avoid_: sizing, weight math, percentage, split ratio

**Layout realization**:
The two-phase construction of a live window from a Layout: N-1 successive tmux splits that subdivide the container's own pane (child 0 reuses it), followed by a correction pass that resizes every child to its apportioned size. The splits are load-bearing, not provisional — each cuts the last pane made, so unsized splits halve the tail geometrically and hit tmux's minimum-pane floor at four children in an 80x24 window.
_Avoid_: apply, instantiation, rendering, pane building

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
The local `<id>/index.json` manifest and its sibling task markdown files beneath the **Task storage** `tasks/` directory, optionally alongside a co-located `spec.md` (the set's whole context in one folder; spec-less sets are normal). A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. Spec existence remains irrelevant to task scheduling and execution — `spec.md` is optional enrichment the **Verifier** may read, never a required input.
_Avoid_: Issue set, PRD, workload, prd.md (legacy filename; no backward-compat read)

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
The git checkout from which task execution starts. It defaults to the selected project's path and may be overridden for a command. For a **Task set**, pop resolves it from the set's **Worktree binding** — and *only* from the binding: a set carrying an unsatisfied managed intent resolves to **no checkout at all** rather than falling back to the trunk, so a set that has not been placed is reported as unplaced instead of silently acting on the repository's shared checkout. Pop resolves it to the checkout root and uses that root for the agent working directory, dirty-tree preflight, staging, commits, and the **Runtime execution lock**. Task artifacts remain in the separate **Task storage**.
_Avoid_: Workload runtime path, task storage, shared git root

**Worktree set**:
A **Task set** drained in its own pop-provisioned git worktree under a **Worktree binding**. The checkout is an ephemeral execution context, not a navigable project peer: pop does not auto-create a session for it. It is still a registered git worktree, so it remains reachable on demand via the Worktree picker, which creates a session only when the human selects it.
_Avoid_: Worktree task, per-task worktree, queue shard

**Drain routing**:
Resolving which checkout a whole-set drain runs in — one rule for both triggers: an existing **Worktree binding** wins regardless of who triggered the drain. A foreground **`pop tasks implement`** invoked outside the binding drains *at* the binding and reports where, refusing when the current repository is not the set's; moving a set off its binding takes a **Forced rebind**. Unbound, a foreground implement binds the current checkout (**Default binding**); the Queue has no current checkout, so a set with no binding is a needs-bind fault, never an invented checkout. Both triggers validate the bound checkout before spawning, and a live **Runtime execution lock** elsewhere refuses. `--in-worktree` provisions a **managed** worktree forked from the current checkout's HEAD, and on an already-bound set is refused without a **Forced rebind**. There is no integration-target fallback. Single-task file runs stay current-checkout.
_Avoid_: checkout picker, runtime resolver, workspace routing

**Default binding**:
The **Worktree binding** a foreground **`pop tasks implement`** records to the current checkout when it drains an otherwise-unplaced set, making that set sticky to where it first ran. It never arises from rebinding: a bound set is re-pointed only by a **Forced rebind** or an operator **Bind worktree**. The **Queue** records no default binding: with no binding it does not drain.
_Avoid_: implicit binding, auto-bind

**Worktree binding**:
A durable association between one **Task set** and one git checkout for that set's execution, recorded in shared per-repository drain state and owned by a provisioning module both **`pop queue run`** and **`pop tasks implement`** call. It is the universal per-set drain router: **Drain routing** consults bindings first, then applies the remaining precedence rules. Bindings are per-set, but a checkout is no longer owned exclusively: several sets may bind the **same** checkout (an N-sets-to-one-checkout sharing that arises when **to-tasks** binds a checkout another set already uses — ADR-0115/ADR-0116); the former one-checkout-to-one-set exclusivity is retired. A binding carries a `Provisioned` bit meaning *pop created this directory* — derived from the checkout's location, not from which verb recorded the binding: a checkout under the managed-worktree root is provisioned even when a set merely adopted it, and a checkout anywhere else is adopted even if pop's **Worktree picker** created it. Destructive teardown of a managed checkout is **reference-counted** (see **Managed-worktree teardown reference count**): **Archive**, a rebind that moves a set off the checkout, and **Fold** can each drop the last reference and reach the confirm-gated delete — keyed on the checkout, not on the leaving binding's `Provisioned` bit — and **Unbind worktree** never deletes. Bindings outside the managed root default to adopted/never-delete, so a hand-written or unrecognized binding can never trigger a directory deletion; pop deletes only what it demonstrably created. A foreground implement that rebinds a set away from an idle managed binding first prompts to delete that managed worktree. A managed binding's checkout lives at a stable path — derived from the **Task set identifier** when pop provisioned it *for* that set, or from a generated slug when the worktree was created ahead of any set — and persists across drain exits, failures, and supervisor restarts so re-spawns resume the same branch rather than forking afresh. The directory name is a label, never a key: resolution reads `RuntimePath` from the binding row, so a worktree whose name no longer matches its set is correct, not stale. If a binding's checkout is missing or no longer registered with git, pop refuses to spawn and directs the human to repair git state or **Unbind worktree** — it never silently re-provisions.
_Avoid_: Runtime path override, per-spawn worktree, timestamped checkout

**Managed-worktree teardown reference count**:
The rule gating destructive deletion of a pop-**managed** worktree: pop removes the checkout and branch only when it lives under the managed-worktree root **and zero** non-archived **Task set**s still hold a **Worktree binding** to that path. Keyed on the checkout path and the live referent count — not on the deleting binding's `Provisioned` bit — so among several sets sharing one managed checkout, the *last* one to let go fires the confirm-gated delete, and an adopting set (whose own binding is `adopted`, never self-torn-down) can still be that trigger. Three acts can drop the last reference and so reach teardown: **Archive**, a rebind that moves a set off the checkout, and **Fold**. Each asks first — the single-trigger rule is retired, the always-confirms rule is not. Closes both failure modes of N-sets-to-one-checkout sharing: deleting a worktree out from under a still-active set, and leaking a pop-created worktree whose original managed set was released first.
_Avoid_: 1:1 checkout ownership, provisioned-bit teardown

**Fold**:
The act of replaying a finished **Task set**'s branch onto the **Trunk worktree**'s branch and releasing its checkout — `pop tasks fold <set>`, or the Fold action on a foldable set in the **Work dashboard** and the **Assist session**. Trunk is never left mid-operation and never gains a merge commit: fold **rebases the set branch onto trunk** inside the set's own checkout (plain rebase — merge commits inside the set branch are flattened), then moves trunk by fast-forward only; if trunk moved in between it redoes the rebase once and then refuses. Foldable means bound plus **DONE** or **Awaiting-approval** — folding an Awaiting-approval set *is* the sign-off, so after a successful rebase and fast-forward it completes every remaining open HITL task in the set, named up front in the confirmation. Pop computes no mergeability verdict in advance and keeps no backlog — the attempt itself is the answer, discovered in the foreground; a conflict opens the **Fold conflict prompt**. On success it releases the **Worktree binding**, then applies **Managed-worktree teardown reference count**; the set is not archived, which stays a separate confirmed act. Fold never pushes and never fetches: landing trunk anywhere else, and refreshing it, are the human's own concern.
_Avoid_: integrate, merge, land, ship, reconcile

**Fold conflict prompt**:
The attended, TTY-only choice pop presents when a **Fold** rebase stops on a conflict in the set's checkout: agent assistance (default, Enter), **resume** (continue the in-flight rebase), **retry** (abort it and restart fold from preflight), verify the set, or exit. It re-appears after every unsuccessful resolution rather than refusing once, and carries the set's **Verified-at SHA** badge so the human can see whether the work is still cleared. Unreachable without a TTY — an unattended resolver moving trunk is exactly what fold refuses to be.
_Avoid_: conflict menu, merge prompt, resolver

**Unbind worktree**:
The human act of releasing a **Worktree binding**, leaving **Task set** task statuses untouched. It is ALWAYS forget-only and never destructive: even a **managed** binding's checkout and branch are retained — only the association is dropped. The symmetric inverse of **Bind worktree**, invoked via `pop tasks unbind-worktree` with a **Task set identifier** (or `U` in the **Queue dashboard**). Refused while the set actively holds the **Runtime execution lock**. To delete a managed worktree, use **Archive**, a rebind, or **Fold** with its delete-worktree confirmation; Unbind followed by Archive is the explicit "keep the worktree, file the set" path.
_Avoid_: abandon, abandon worktree, release worktree, teardown

**Bind worktree**:
The human act of retargeting where a **Task set** drains, via `pop tasks bind-worktree <set>`. Its default mode, run from inside the target checkout, adopts that checkout as an **adopted** **Worktree binding** (pop never deletes the checkout). `--managed` instead records a **managed intent** on the set — dropping any existing binding — so the next Queue drain provisions a pop-managed worktree forked from the **Trunk worktree**, exactly as `register --managed` would have; provisioning stays lazy, never immediate. Symmetric sibling of **Unbind worktree**; both mutate the shared binding store and run without the daemon. Refuses to re-point a set that is already bound elsewhere without `--force`, and never re-points a set holding a live **Runtime execution lock**.
_Avoid_: adopt worktree, claim worktree, queue bind

**Dirty runtime strategy**:
Controls how task execution starts from a dirty runtime checkout. `continue` starts execution without modifying the existing dirty state; it is the default both when the option is absent and when it is present without a value, and after successful task completion the normal implementation commit intentionally includes both pre-existing and agent changes. `commit-and-continue` captures the existing dirty state in a separate implementation commit before invoking the agent. `stash-and-continue` stashes tracked and untracked changes but not ignored files, prints the stash reference when one is created, and leaves restoration to the user; an empty stash does not prevent execution. When the runtime is dirty the command always displays `git status` and the chosen strategy's effect, then requires interactive `y` confirmation; `--yes` auto-confirms, and a non-interactive run without `--yes` is rejected. Implement applies the chosen strategy once before draining its selected Task set.
_Avoid_: Clean runtime checkout requirement, automatic stash restoration

**Implementation commit**:
A commit created by the task executor from runtime-checkout changes. After successful task completion, the executor stages all runtime changes and commits them with a task-derived subject and the agent summary as body. The subject's scope names the Task set by its identifier without the timestamp prefix. Task artifacts remain local and unstaged.
_Avoid_: Task artifact update, progress record

**Task manifest**:
The `index.json` within a Task set. It remains the source of truth for task eligibility and completion, and carries **only** the `tasks` array — it no longer holds set-level `worktree` or `auto_drain` keys (ADR-0115: binding and auto-drain are register/CLI/dashboard concerns, not manifest fields). A legacy manifest still carrying those keys is **not** Malformed; the keys are ignored with a deprecation warning. Task-level fields inside the array still follow their declared types.
_Avoid_: Issue manifest, workload, dashboard
**Worktree directive**:
A retired manifest key. The `worktree` key in a **Task manifest** is now read by nothing: registration ignores it and records no intent (ADR-0115), and placement is CLI-only — `pop tasks register --managed` and `pop tasks bind-worktree --managed` provision a **managed** worktree and bind it eagerly (ADR-0147), so "placed" and "registered" are the same instant. The registered-intent field survives solely as a Queue-side healing path for sets registered under the old lazy behaviour; a foreground **`pop tasks implement`** never consults it, and nothing writes a new one.
_Avoid_: worktree_ready, worktree mode, per-set worktree flag, isolation flag

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
An independent **Verifier** agent's judgment of a **Task set**'s completed AFK work. Its verdict scope is only the set's `done` AFK tasks — the prompt carries their bodies and acceptance criteria, the accumulated diff, and the optional co-located `spec.md`; open/not-`done` AFK tasks and HITL tasks (any status) are excluded so the Verifier never fails a set on work it isn't equipped to judge (a not-yet-run HITL sign-off is not an unmet criterion). Gated by user config, off by default. When enabled it fires as the tail of a **Drain**: on a DONE set, and on an **Awaiting-approval Task set** it runs *before* the terminal HITL sign-off gate — a PASS then opens that gate, so cheap agent checking precedes expensive human time.
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
The work SHA recorded on the set's latest PASS **Verify verdict** in the current **verification episode**, surfaced as a three-state badge wherever pop shows a set's status: **green** `verified @ <shortSHA>` when runtime HEAD matches that SHA, **yellow** when HEAD has moved past it ("cleared at this commit, HEAD has moved since" — without regressing DONE or AWAITING-APPROVAL to NEEDS-VERIFY), and **red** `unverified` when verification is enabled but no PASS exists in the episode. Omitted entirely when verification is off. The shortSHA is a 12-char prefix, matching verify output; the badge renders on **`pop tasks status`** in the Details column, on the **Work dashboard** in the main STATUS column and the detail-view header, and in the **Fold conflict prompt**.
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
The single task-execution command, `pop tasks implement`, that runs tasks through the **Task executor** and dispatches by **Task target reference** shape. A `<task-set>/<file>.md` reference runs exactly that one task in the current checkout (**Execution confirmation** prompt once). A bare set identifier — or no argument, choosing the highest-priority Ready set — **drains** the set with no AFK start prompt until it reaches Done, Blocked, Deferred, Failed, or an **Agent quota pause**; mid-drain **HITL gate prompt** and **Failed gate prompt** stay interactive on a TTY. For a whole-set drain it is **binding-first** like every other set-scoped command: a bound set drains at its **Worktree binding** wherever implement was invoked from, and only a **Forced rebind** re-points it; an unbound set binds the current checkout (**Default binding**). `--in-worktree` instead provisions a **managed** worktree forked from the current checkout's HEAD, binds the set to it, and drains there. There is no automatic worktree default and no `--inline` flag — the current checkout is the baseline for an unplaced set, and `--in-worktree` is the explicit opt-in to isolation. The interactive **Drain target picker** is a **Queue dashboard** affordance only; bare `pop tasks implement` never prompts for a target, so the Queue's spawned drains never block. Completion is silent about merging: when a drain lands Done in a worktree, the human **Fold**s or opens a PR themselves.
_Avoid_: Run, Drain, separate one-vs-many verbs, --inline, auto-worktree default, run issue, run issues, run all, next Task set, Run PRD

**Implement run**:
One invocation of a whole-set **Implement** — from set selection to its exit. It holds at most one live **Drain** at a time and may comprise several: reaching a gate menu parks (finishes) the held Drain so the menu runs lock-free, and resuming AFK work begins a fresh one (quota waits likewise). The Implement run, not the Drain, owns the gate menus, the pre-approval verify phase, and the shared prompt reader.
_Avoid_: Drain (for the whole invocation), session, segment, drain session

**Agent preset**:
A headless agent the task executor recognizes — `claude`, `opencode`, `cursor`, `codex`, `pi`, or `kimi` — selected by name and optionally augmented with extra invocation arguments (e.g. `claude --model opus4.8`). Pop runs the supplied command as given, exactly as it runs a **Custom agent command**; the sole difference is recognition. Because the first token names a known agent, the **Agent adapter** appends the flags Pop owns — the output protocol governed by **Agent output mode** — after the user's arguments, then delivers the generated prompt per-adapter: as the final positional argument for most presets, as the `-p` flag value for `kimi` (which has no positional-prompt form). Pop-owned flags come last among flags, making them authoritative: a user value for an owned flag is overridden, not rejected. Recognition is what lets Pop parse the structured stream and keep every adapter capability; augmenting a recognized preset this way is distinct from replacing the invocation with a Custom agent command.
_Avoid_: Integration

**Agent fallback**:
The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[tasks.implement].agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next on any preset-scoped **Agent proceed verdict**: a quota pause, an **Agent authentication failure**, or a missing binary. It does *not* fall through on ordinary task failure. A machine-global cooldown store records quota pauses per preset; a human-healing verdict writes no cooldown. When every configured preset is human-healing unavailable, implement exits `ExitSetup` and the task stays Open. The Verifier walks a parallel list, `[tasks.verify].agents`, with the same class plus a fall-through once a preset's retry loop is exhausted — an asymmetry with implement that is deliberate and unresolved. When attended **Integration conflict** assistance needs an agent, it uses only the first entry of the implement list.
_Avoid_: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation, [queue].agents, [workload] default_agents

**Custom agent command**:
A trusted, opaque command supplied via `--agent-cmd` that Pop runs verbatim through a shell, with the generated prompt appended as the final positional argument. Pop neither recognizes nor inspects it, so it forgoes every adapter capability: plain output only, no **Agent quota detection**, no live rendering, and no entry in the **Captured attempt stream**. It governs only unattended task attempts, never attended HITL assistance. It replaces the invocation wholesale — the inverse of an augmented **Agent preset**.
_Avoid_: Override command, escape-hatch agent, agent passthrough

**Built-in model catalog**:
A short, hand-maintained list of model aliases Pop ships for each recognized **Agent preset**, surfaced as a column in `pop tasks agents`, recommended value first — a suggestion surface to help a planner fill an **Agent preset**'s `--model`, never exhaustive, never a validation gate. Distinct from the **Effort ladder** (the resolution surface): several presets' catalogs now feed built-in ladders — `claude`, `codex`, `cursor`, `pi`, and `kimi` — while the curated lists stay advisory. Only `claude`'s entries are stable, auto-resolving, account-independent aliases; the `codex`/`cursor`/`pi`/`kimi` ladders pin version- and account-specific ids that need maintenance and are overridable defaults, not commitments. kimi's baked entries use the standard-login `moonshot-ai/…` alias form; installs whose provider config names aliases differently (e.g. managed `kimi-code/…`) must override through `[effort.kimi]`, since kimi resolves `--model` by exact config key only.
_Avoid_: model source, live model listing, model provenance, Curated model aliases

**Effort**:
An optional per-task `effort` key in the **Manifest** — `light`, `standard`, or `heavy` — naming how much capability the task wants from whichever agent runs it. It is the *single* user-facing strength knob: there is deliberately no separate reasoning axis. pop resolves it through the chosen agent's **Effort ladder** into a bundle of *both* a `--model` and a **Reasoning effort** (the model's thinking level), so one tier decides which model runs and how hard it thinks. Orthogonal to the agent axis owned by **Agent fallback** and explicit `--agent` augmentation; it never selects an agent. An absent key means `standard`; an unknown token is a contract fault that makes the Task set **Malformed**. Resolution applies only when no `--model` is hand-pinned in `--agent` args — pinning a model skips the whole bundle (model and reasoning both), since the tier's reasoning was curated for the tier's model; an absent `effort` injects nothing. A reasoning value hand-set in `--agent` args is kept while the ladder model still applies.
_Avoid_: Priority, weight, tier, task size, difficulty

**Effort ladder**:
A per-agent, per-tier ordered list of **(model, Reasoning effort)** bundles that resolves an **Effort** to a concrete `--model` plus a reasoning channel for whichever agent was chosen. Pop ships built-in ladders for `claude`, `codex`, `cursor`, `pi`, and `kimi`; every other agent (e.g. `opencode`) has none built-in and is configured in `config.toml` under `[effort.<agent>]`, which fully replaces the built-in for an agent it names. Each tier is a TOML array of `{ model, reasoning }` tables, reasoning optional. Resolution uses the head of the chosen tier; the ordered tail is reserved for a deferred runtime fallback, and each fallback element carries its own reasoning. Reasoning is rendered per-adapter — `claude --effort`, `codex -c model_reasoning_effort=`, `pi --thinking`, `kimi` via a `KIMI_MODEL_THINKING_EFFORT` environment variable on the invocation — except for `cursor`, which selects a full concrete model name per tier and does not emit a separate reasoning parameter. Agents with no reasoning mechanism (`opencode`) or no ladder make that part a graceful no-op. kimi's built-in ladder is heavy `moonshot-ai/kimi-k3`@high, standard `moonshot-ai/kimi-k3`@low, light `moonshot-ai/kimi-k2.7-code-highspeed` model-only — k3 accepts only `low`/`high`/`max` on the wire (a `medium` env value is a server 400), so `@low` is the lightest native k3 rung; the light tier's plan gating surfaces as **Agent fallback** fall-through via **Plan gate**. Surfaced per agent in `pop tasks agents` with built-in-versus-configured provenance.
_Avoid_: Model catalog, effort table, model tier map, model priority list

**Reasoning effort**:
The model's thinking-intensity level (e.g. `low`/`medium`/`high`/`xhigh`/`max`), distinct from pop's **Effort** tier despite the shared word. Not a user-facing knob: it is bundled into each **Effort ladder** tier alongside the model and resolved together, so a tier sets both which model runs and how hard it thinks. Passed per-adapter (`claude --effort`, `codex -c model_reasoning_effort=`, `pi --thinking`, `kimi` via the `KIMI_MODEL_THINKING_EFFORT` environment variable — the only per-invocation channel kimi exposes, which bypasses kimi's own effort validation, so kimi ladder entries must pair only efforts the model declares); `cursor` has no separate reasoning parameter and instead selects a full model name that already encodes the desired capability. Agents with no mechanism (`opencode`) ignore it. A value hand-set in `--agent` args is respected over the ladder's; for kimi, a `KIMI_MODEL_THINKING_EFFORT` already present in pop's own environment counts as hand-set and likewise wins.
_Avoid_: Effort (pop's task tier), thinking budget, depth

**Interactive agent preset**:
A named attended-assistance command known to an Agent adapter. It is separate from an Agent preset because assisting a human at a HITL gate is an attended conversation, not a headless task attempt; custom headless agent commands do not imply an interactive preset. Every supported preset launches its own interactive binary, so when an **Agent preset** carries extra arguments, those arguments ride into that preset's own attended assistance. kimi's interactive mode accepts no initial-prompt argument, so its attended launch is the bare binary and the generated briefing is delivered on the clipboard for the human to paste.
_Avoid_: Agent preset, stripped headless command, agent-cmd

**Agent adapter**:
The preset-specific bridge between Pop and a supported agent. An adapter may provide headless invocation, headless output handling, agent-assistance invocation, and a **Model source**; attended assistance launches the preset's own interactive binary and is owned by the adapter rather than the HITL gate prompt. An adapter reports assistance Unavailable only when it has no usable interactive command at all (e.g. custom headless `--agent-cmd`).
_Avoid_: Universal JSON protocol, agent integration

**Agent catalog**:
The readout of `pop tasks agents`: every recognized **Agent preset** with its binary, whether that binary is on PATH, which preset is the default, and notes such as attended-assistance availability. It reports what Pop owns by PATH lookup only and never execs agents. Authentication belongs to **Doctor**, which runs each preset's **Agent availability probe** — the promise this entry has always made, now kept. Its audience is a planner choosing an **Agent preset** as much as a human. Model details come from each preset's **Model source**, surfaced only on request.
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
A quota-paused drain registered with the **Agent quota recovery coordinator** while its owning process polls for a **Recovery turn**. It names the task set, exhausted preset, reset instant, the **Runtime path** from the set's **Worktree binding** at park time, and its owner's PID + start token. Registration is a **Checkout claim**: it blocks admission of any other drain onto the set *and* the checkout for the duration of the wait — enforced at `BeginDrain` and Queue dispatch, not just documented. Deregistration happens on turn taken, interruption, or process exit; a dead owner's waiter is swept by reconcile.
_Avoid_: quota backoff, agent cooldown entry, parked set

**Agent quota recovery wait**:
The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume at the **Quota recovery resume point**. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run. Pre-reset it prints a local-time countdown on the regular poll cadence; post-reset it prints the **Recovery block reason** on change plus a periodic heartbeat, never on the fast external-deregistration check. SIGINT deregisters the waiter and exits as an **Interrupted task** drain (`ExitInterrupted`); the open task and partial checkout changes are preserved.
_Avoid_: in-process sleep, quota retry loop, blocking wait, --yes-only wait

**Quota recovery resume point**:
Where **Agent quota recovery wait** re-enters work after a **Recovery turn**: the same open task for a mid-drain task-attempt pause, or the **Verifier** for a post-drain verify pause — never a completed task re-run. Any **Agent preset** invocation during implement (task attempt or verify) may trigger recovery wait; all share the same checkout-scoped **Recovery turn**.
_Avoid_: verify-only wait, task-only wait, full drain restart

**Checkout gate hold**:
A registration naming the task set, **Runtime path**, and holder PID + start token, taken when implement parks at a **Failed gate prompt**, **HITL gate prompt**, or **Verify-fail gate prompt** (runtime lock released per ADR-0067). It exists in **two scopes**, and which one applies is decided by whether the parked human's work lives in the tree. As **set-scoped occupancy** — the default for every gate — it is keyed on (**Runtime path**, task set) and occupies only *that set* for **Checkout quiescence**: it keeps an **Out-of-band mutation** from racing the human's own disposition of the set they are sitting on, and is invisible to every other set on the same checkout. As a **checkout-scoped Checkout claim** — a Failed-gate park with uncommitted files in the tree, dirtiness snapshotted at park time — it occupies the whole checkout, blocking admission; at most one claiming hold per **Runtime path**, an invariant the schema enforces. Registration never replaces a different live owner's hold for the same set, and release removes only the holder's own row. Liveness is the real release: a hold whose owner PID is dead is ignored and replaceable, so a crashed gate never wedges anything.
_Avoid_: runtime lock through gate, gate checkout mutex, parked drain hold

**Recovery turn**:
One granted slot to resume agent work on a given **Runtime path** after the waiter's exhausted **Agent preset** cooldown clears globally. A waiter acquires a turn only when no **Checkout claim** other than its own registration is live on that checkout, and it is first under **Recovery turn ordering** for that path. Human waits are not claims, so a waiter resumes past an open gate menu; a dirty Failed-gate hold is a claim and still blocks. The turn is preset-agnostic — at most one recovery resume per checkout at a time regardless of which **Agent preset** each waiter exhausted. Parallel worktrees resume independently; the guard is against multiple sets mutating the same checkout.
_Avoid_: next shot, quota lease, recovery gate, per-preset queue

**Recovery turn ordering**:
Among **Recovery waiter**s on the same **Runtime path**, turns go to the highest **Task set priority** first; equal priority breaks FIFO by registration time. **Worktree binding** supplies the path at park time; ordering does not compare across checkouts.
_Avoid_: round-robin, jitter lottery, global FIFO, per-preset queue

**Queue pinned quota backoff**:
Retired. Pinned-agent quota pauses no longer write a daemon-state **SetBackoff**; **Recovery waiter** registration in the **Agent quota recovery coordinator** is the sole spawn-skip signal for quota-blocked sets. Queue reads recovery waiters (and live drains) only.
_Avoid_: set backed off for pinned agent cooldown, quota_backoff

**Task attempt**:
One agent invocation for a task. The task executor retries an unsuccessful task up to the implement Task retry cap, waiting a Task attempt retry delay between consecutive failures — except an implement Task attempt timeout, which retries instantly with no delay. Exhaustion marks the task Failed, records the attempt count and reason locally, and stops draining.
_Avoid_: Task set retry, task dependency

**Task attempt timeout**:
The maximum duration for one task attempt, defaulting to 45 minutes and configurable per command. When exceeded, the task executor terminates the agent process group and preserves partial changes. The outcome is retry-eligible: it consumes one slot of the Task retry cap and carries the ADR-0040 "continue" digest forward — but on implement it retries INSTANTLY, with no Task attempt retry delay, unlike an incomplete-assessment failure. A timeout almost always means execution simply ran too long (typically an oversized context window), and a fresh attempt restarts from the compact prior-attempt digest rather than the bloated transcript, so a wait would add nothing; genuine transient API failures surface instead as an Agent quota pause with its own recovery wait. On verify a timeout stays a delayed retry (the instant-retry rationale is implement-specific — a Verifier timeout is more likely a real hang). Only after the cap is spent does the executor mark an Exhausted task, append a Failed progress record, and stop at the Failed gate prompt. Distinct from Agent quota pause (clean stop, recovery wait) and from Interrupted task (SIGINT, no progress record).
_Avoid_: Task set timeout, interruption

**Task attempt retry delay**:
The wall-clock wait inserted after a failed agent invocation and before the next try, shared by Task attempt retries and Verifier retries alike. Applies to retry-eligible failures — for implement, incomplete assessment outcomes (an unexpected client exit, API error, or process exit before the timeout without a proper sentinel reason); for the Verifier, invocation failures (timeout, agent error, unparseable output) — not a cleanly parsed NEEDS-HUMAN or FIXABLE verdict, which is the Verifier succeeding at its job. The one retry-eligible outcome that does NOT wait is an implement Task attempt timeout, which retries instantly; a Verifier timeout still uses this schedule. Agent quota pause on implement remains a clean stop with no retry loop; on verify it still falls through to the next configured agent without a delay. The default schedule is one minute after the first failure, five minutes after the second, fifteen minutes after the third and every subsequent failure when the attempt cap exceeds three; when the configured delay list is shorter than the retry count, the last entry repeats. Attempt one still starts immediately; delays apply only between retries. The wait is a blocking in-process sleep: the Drain and runtime lock stay held, the pane shows a countdown, and Ctrl-C during the wait exits as Interrupted with no further attempt. Configurable via Task attempt retry schedule at `[tasks]` root; distinct from Queue backoff (abnormal drain exits).
_Avoid_: retry backoff, attempt cooldown, API backoff, persisted retry schedule

**Task attempt retry schedule**:
The ordered list of duration strings at `[tasks]` root (`attempt_retry_delays`) governing **Task attempt retry delay** for both implement task retries and **Verifier** retries. Omitted ⇒ `["1m", "5m", "15m"]`. An empty list ⇒ zero delay (instant retries, restoring pre-backoff behavior). Parsed like `[queue] crash_retry_delays`: each entry is one inter-attempt wait, and once the list is exhausted the last entry repeats for every subsequent retry. Distinct from **Task retry cap** and from **Queue backoff**.
_Avoid_: max-tries, crash_retry_delays, retry_after

**Task retry cap**:
The maximum started agent invocations per retry loop before giving up. A single default at `[tasks]` root (`max_tries`, default 3) applies to both implement and verify; `[tasks.implement]` and `[tasks.verify]` may each override their side independently. On implement, an explicit `--max-tries` flag wins over config. The cap is **per agent preset**: the executor retries the current preset up to the cap (with **Task attempt retry delay** between failures), then **Agent fallback** moves to the next preset. Any preset-scoped **Agent proceed verdict** abandons the remaining cap for that preset immediately and consumes no further attempts — the cap governs attempts that ran, not a preset that cannot run. Distinct from **Task attempt retry schedule** (how long to wait between tries).
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
The agent-specific accounting of where a Task attempt's wall-clock time went, derived from its Captured attempt stream: each attempt's outcome and total duration, its read-time-derived token spend (input/output/cache, claude-first and absent for adapters that report none), and — for agents whose stream pairs a tool invocation with its result — a per-tool count and duration, followed by **Model time**. Tool figures are reported under the agent that ran the attempt because tool vocabularies differ by agent. It is the shared header rendered in two places: implement prints it as a task finishes, and **Attempt stream replay** prints it above each attempt's replayed events (ordered by attempt start time). The standalone `pop tasks timings` lens that once reprinted the per-task history is retired in favour of stream. Timing itself does not roll up across Task sets — attempt durations are only comparable within a task — but spend does, through the **Spend lens**.
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
The interactive choice shown when a **Drain** or an **Assist session** reaches a **Verify-failed Task set** on a TTY — the verify counterpart of the **HITL gate prompt** and **Failed gate prompt**. Agent assistance is the first option and the Enter default, matching the **HITL gate prompt** rather than diverging from it; Accept (record an **Accepted verdict** with a note), Remediate (spawn a **Remediation task** with a note), open a **Runtime shell**, and exit follow. Assistance does not disposition the set, but it may *draft* a **Remediation task**: on return the gate re-derives the **Task manifest**, and a remediation task the agent wrote is presented for the human to confirm rather than retyped from scratch. The **HITL assistance prompt** carries the CLI forms of every allowed outcome, so the human can also act from the **Runtime shell**. Headless runs use `pop tasks verify <set> --accept` / `--remediate "<note>"` instead. Re-verify is not offered here — re-running the **Verifier** is a separate force action, not a response to findings.
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
The execution-kind **Checkout claim**: the running **Drain** row, held only while implement is actively executing in a checkout — acquired around each contiguous run of AFK attempts and released at every wait for human input (pre-run confirmation, **HITL gate prompt**, **Failed gate prompt**). Gate menus, **HITL assistance session**s, and the **Runtime shell** run lock-free; resuming re-acquires, refusing cleanly if the checkout is claimed meanwhile. Verification runs hold it like any drain — a reverify launched from a gate re-acquires before running. It is no longer the *only* busy signal: admission checks the full **Checkout claim** union, so a quota-waiting or dirty-Failed-gate set keeps its checkout even without a running Drain row.
_Avoid_: Global task lock, project-name lock

**Out-of-band mutation**:
A change to a **Task set**'s verdicts or manifest made from outside a drain — e.g. the **Accept** or **Remediate** disposition issued from the standalone CLI. Permitted only under **Checkout quiescence**.
_Avoid_: external mutation, offline edit

**Checkout quiescence**:
The precondition for any **Out-of-band mutation**: no **Checkout claim** on the checkout, and no live **Checkout gate hold** *for the set being mutated* held by a foreign process. Quiescence is asked per set, not per checkout — a human parked at one set's gate has no standing to block a disposition of a different set sharing the tree. A hold owned by the mutating process itself does not occupy: the human sitting at the gate is the one the hold exists to protect, so their own Accept or Remediate proceeds. A refusal names the occupant *and where to reach it* — PID, controlling tty, and **Queue pane** where known — because the resolution is almost always "answer the prompt that is still open"; when the occupant is a **Recovery waiter**, it also reports whether that waiter is next under **Recovery turn ordering**.
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
A registered Task set the human has filed away with **Archive**, recorded by an `archived` flag on its **Task state** registration entry. An Archived Task set is hidden from the **Status table**, from automatic selection and draining, and from every **Task shell completion** surface except **Unarchive**; its task markdown, **Task manifest**, **Progress record**, **Captured attempt stream**s, and task statuses are untouched, so archiving is reversible. The flag governs *scheduling and display only* — it reaches no running process, so it can never retire a **Checkout gate hold**, cancel a **Drain**, or answer an open gate menu. Occupancy is therefore never filtered by it: a live gate or drain owned by an Archived set still appears wherever occupancy is reported, precisely so a hidden row cannot become an invisible blocker. Because an Archived set is outside the verification loop, `pop tasks status --archived` lists each set at its **manifest-derived** status only, skipping the **Verify verdict** overlay — a formerly-Done set reads Done, never NEEDS-VERIFY. The one exception is a set that held a managed **Worktree binding** at archive time: its pop-created worktree and branch may be deleted by the confirm-gated teardown at **Archive**, which **Unarchive** cannot restore. Archiving is a registration-metadata decision like **Task set priority** — not a derived **Task set status** and not a task-status transition — so it appends no **Progress record**. An action verb (**Implement**, **Open task**, **Complete task**, **Skipped task** via `skip`, set-priority) refuses an Archived Task set target and points the human to **Unarchive** first; read-only snapshot verbs (**Task set export**, **Show path**, and `timings`) still resolve an explicitly typed archived **Task identifier** because they neither schedule nor mutate the set.
_Avoid_: Deleted Task set, Task set export (the tar.gz archive), Done Task set, Missing Task set

**Archive**:
The command `pop tasks archive` that files Task sets away as **Archived Task set**s. It refuses a set with live occupancy — a running **Drain**, a **Recovery waiter**, or an open **Checkout gate hold** — naming the PID and where the prompt is parked, because filing away a set whose pane is still waiting on a keystroke hides the one surface that would have reminded the human to answer it; `--force` archives anyway and releases the hold. With no argument it opens a **Multi-set selection** of every non-archived registered set — Done, Deferred, Ready, Blocked, Failed, Missing, and Malformed alike — with only **Done** sets pre-checked, so the common "review the done ones and move on" pass is one confirmation. A bare **Task set identifier** archives exactly that set regardless of its **Task set status**, with no picker. Archiving a set whose checkout is a pop-**managed** worktree prompts `delete managed worktree? [y/N]` **only when this is the last non-archived set bound to that checkout** (**Managed-worktree teardown reference count**): on confirm pop deletes the worktree and branch and releases the binding; declining aborts the archive (to keep the worktree, **Unbind worktree** first — non-destructive — then archive). When other non-archived sets still share the checkout, archive is metadata-only and no delete is offered. Archiving an unbound, adopted-bound, or trunk-bound set is metadata-only and reversible as before. `--yes` skips the picker and archives precisely the Done sets — the unattended form of the default — deleting the managed worktrees of any that reach zero live referents without a further prompt. Like **Multi-task selection**, a no-argument run with no interactive TTY and no `--yes` is rejected rather than mass-mutating silently, pointing the human to `--yes` or a bare identifier. Archiving several sets is one atomic **Task state** write and appends no **Progress record**.
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
The set of work the **Queue daemon** supervises: **Auto-drain**-marked Ready Task sets in git-backed registered projects, plus every non-paused **Routine** regardless of whether its bound directory is git-backed (Routines are discovered from `routines/` in pop's data dir, not from project scanning). Running `pop queue run` is standing consent to act, but the daemon drains only sets a human has marked **Auto-drain** (default off) and fires only Routines a human authored and has not paused; there is no per-project opt-in flag and no per-drain AFK start prompt. The per-set opt-in is **Auto-drain**, toggled from the **Queue dashboard**; the per-set opt-out remains **Archive**; manual `i` from the **Queue dashboard** drains a set regardless of its **Auto-drain** bit. Queue spawns plain `pop tasks implement <set>` — no `--yes` — so **HITL gate prompt** and **Failed gate prompt** stay interactive when the drain pane has a TTY. The blast radius is self-limiting because the daemon only acts on Auto-drain Ready sets and deliberately authored Routines; a project with no sets is skipped. A configured **Project** with no git checkout is also outside set-draining scope — it has no **Repository identity** and therefore no **Task storage**; the supervisor silently skips it like a project with no sets, never a scan error. When a project has no tmux session, the daemon creates one detached and splits a drain pane into that session's main window (index 0); subsequent drains split additional panes there.
_Avoid_: Per-project queue opt-in, global priority queue, per-drain --yes

**Queue journal**:
The Queue journal *view* — not a separate persisted file. `pop queue log` reconstructs the event history (started, done, failed, HITL-blocked, quota-paused-and-agent-switched, crashed, backing-off, or parked) at read time by reading each **Drain** row, integration event, and park-clear from the SQLite store; there is no append-only journal file and **Implement** emits no separate drain-outcome record. **Agent fallback** and backoff are likewise derived from that stored Drain history. `pop queue status` reads live state, such as picked-up sets, cooling agents, parked sets, and idle projects; `pop queue log` reconstructs the journal history from the store.
_Avoid_: Progress record, Captured attempt stream, Task state

**Drain**:
One supervised execution of draining a **Task set**, tracked through an explicit lifecycle from start to a terminal disposition (its **Drain outcome**). A Task set may be drained many times — after a reset, a crash, or a quota pause — and each is a distinct Drain; a set's Drain history is the ordered record of them. The Drain, not the Task set, carries execution lifecycle state; the set's manifest-derived **Task set status** (what work remains) is a separate, derived concern.
_Avoid_: Run, attempt, drain record

**Drain outcome**:
The `interrupted` outcome is now a deliberate human handoff, not an abnormal exit. Interrupting a live drain lands on the **Interrupt gate prompt** (park-and-resume, like reaching any gate); only Exit from that gate records the `interrupted` terminal, and Continue produces no terminal at all (the drain resumes to its own later stopping point). `interrupted` is reclassified as a **clean** exit: it no longer drives **Queue backoff** (only `crashed`/kill remain abnormal), because a manual interrupt now clears **Auto-drain** so there is no re-spawn to throttle. finished, quota-paused, verify_failed, and interrupted are clean; only crashed is abnormal.
_Avoid_: Task set status, drain disposition, drain result

**Queue run output**:
The live stdout of `pop queue run` — an operator-facing event stream, not a repeating inventory. It prints one **Queue run baseline** on startup (the full scheduling-relevant picture of what the supervisor is watching), then only **Queue run deltas** when something changes: spawns, terminal drain outcomes, agent cooldowns, parks, and errors. A quiet tick with no change prints nothing. Drain panes keep their own implement output; `pop queue status` remains the on-demand full snapshot.
_Avoid_: Per-tick status dump, queue log replay

**Queue run baseline**:
The one-time inventory printed when `pop queue run` starts. It opens with a **Queue status summary** — aggregate queue work (running, queued, blocked) — then lists every scheduling-relevant bucket the supervisor is watching: running drains, queued ready sets, blocked state (parked sets, crash backoffs, agent cooldowns), and scan errors for in-scope projects that failed to scan or have a broken repo-root `.pop/config.toml` — in the same human-readable shape as `pop queue status`. Projects outside **Queue scope** and in-scope projects with no ready work and no active drain are not listed individually; they collapse into a single count line (e.g. "12 other projects: no ready work").
_Avoid_: Per-project idle listing, repeating status table

**Queue status summary**:
The one-line headline atop `pop queue status`, aggregating current queue work — how many Task sets are running, queued for drain, blocked, or awaiting approval. Below it, `pop queue status` now renders the **same task-set table as the Queue dashboard** — the same rows, columns (PROJECT / TASK SET / STATUS / WORKTREE + the leading live-drain indicator), row filter, and sort, built from one shared row builder and one shared comparator — replacing the former per-bucket detail inventory (Picked-up / Active worktrees / Queued ready / Blocked / Awaiting approval / Skipped). It stays non-interactive so it remains greppable and pipeable, and doubles as the Queue run baseline. Only a trailing **Scan errors** section survives alongside the table.
_Avoid_: Daemon state JSON, per-project idle dump

**Queue run delta**:
A single stdout line emitted by `pop queue run` when supervisor-relevant state changes after the baseline. Deltas cover spawns, terminal drain outcomes (done, failed, HITL-blocked, quota-paused, crashed), set parked, agent cooldown started, cooldown or backoff cleared (work may resume), and per-project scan errors. Unchanged state — still running, still cooling, still waiting — prints nothing.
_Avoid_: Heartbeat line, per-tick inventory repeat

**Queue backoff**:
The daemon's response to an abnormal drain exit — now crash or kill only. A manual interrupt is no longer abnormal (it clears **Auto-drain** via **Interrupt auto-drain revocation**, so there is nothing left for the daemon to re-spawn and throttle). The daemon applies an escalating per-set delay and, after N consecutive abnormal exits, parks the set until a human clears it; a clean exit resets the counter. Distinguishing abnormal (crash/kill) from clean (finished/quota-paused/verify-failed/interrupted) reads the **Drain**'s terminal `state` directly (`store.drainStateAbnormal`).
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
The automatic flip of a set's **Auto-drain** bit from on to off. Two triggers: (1) at drain finalization to a terminal disposition — derived status DONE or AWAITING-APPROVAL, the states in which all AFK work is drained (ADR-0098); and (2) a manual interrupt of a live drain (**Interrupt auto-drain revocation**), which clears unconditionally at interrupt with Continue reviving the prior value. Both fire only from a live/finishing drain, never from a background reader; both are idempotent, announced, and a durable per-set trace. Because they discard consent rather than hide the marker, a later **Open task**, **Remediation task**, or **Verification invalidation** does not auto-re-fire the daemon — a human must re-mark **Auto-drain**.
_Avoid_: auto-drain reset, consent expiry, AD auto-off, auto-drain revoke

**Queue dashboard**:
Retired name for the Work dashboard — the surface was renamed when Wayfinding Maps joined Task sets on one table; `pop queue dashboard` survives only as a hidden alias. The Queue concept itself (daemon, status, log — the scheduling concern) keeps its name and scope.
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

**Execution-state store**:
The machine-global SQLite database in pop's data dir holding every layer-2 execution fact — Drains, Worktree bindings, Verify verdicts, agent cooldowns, spawn intents, gate holds (ADR-0055/0118). Layer-1 Task set status stays manifest-derived on disk and is never stored here. A process holds exactly one lazily-opened cached handle, and every subsystem borrows that handle — nothing opens the database through a second path, and borrowers never close the shared handle (ADR-0140). Pure readers never create the database as a side effect. Process liveness (the PID + start-time predicate) is a policy the store receives at open, not a closure callers pass per operation.
_Avoid_: drain store, pop.db, daemon state, per-repository drain state

**Binding-first runtime resolution**:
The rule that any command acting on a **Task set** resolves its **Runtime path** from the set's **Worktree binding** when bound, and only falls back to the **current checkout** when unbound — the same law the **Drain** already follows. It governs `pop tasks verify` (**Accept**, **Remediate**, re-run), `pop tasks status`, and the **Assist session**, so every surface reads and writes verdicts at one checkout's HEAD and cannot disagree with the **Queue dashboard**.
_Avoid_: cwd resolution, current-directory routing

**Forced rebind**:
The explicit act of moving a **Task set** off its **Worktree binding** from a foreground **`pop tasks implement`**, requested with `--force-rebind` and never implied by where the command was run. Without the flag the drain follows the binding instead of re-pointing it. With it, a set that already has a done task (**Started**) prompts first — rebinding resumes the drain in a checkout that lacks that work — and a non-interactive run needs `--yes`. It is the single authorization both a plain rebind and `--in-worktree` on an already-bound set pass through; the confirm-gated deletion of the vacated managed checkout (**Managed-worktree teardown reference count**) is a separate question, asked after it.
_Avoid_: implicit rebind, cwd wins, --force, retarget

**Eager managed provisioning**:
The rule that `--managed` provisions its worktree at the moment the human asks for it — `pop tasks register --managed` and `pop tasks bind-worktree --managed` both fork a worktree from the **Trunk worktree** and record a **Worktree binding** then and there, rather than deferring to the first Queue drain. It makes "the set is registered managed" and "the set has a checkout" the same instant, so no surface ever has to answer where an intended-but-unplaced set lives. A repository with no resolvable **Trunk worktree** refuses the registration outright instead of failing later at dispatch. Because the fork base is now register-time trunk rather than drain-time trunk, a managed worktree whose branch carries no commits yet is fast-forwarded onto current trunk at its first drain; once the set has real work on the branch it is left alone.
_Avoid_: lazy provisioning, deferred worktree, managed intent

**Unbound managed worktree**:
A pop-**managed** worktree with zero non-archived **Task set**s bound to it — the state the **Managed-worktree teardown reference count** already computes as `liveReferentCount == 0`. It arises as residue, when the last referent folds or archives, and now also from birth, when the **Worktree picker** creates one ahead of any set so a design session's docs commit lands in the checkout the implementation will later run in (ADR-0152). Pop never deletes one on its own: it is marked in the picker and removed by the human's existing delete action, because an abandoned one may hold the only copy of that docs commit. Binding a set to it via **to-tasks** or **Bind worktree** makes it an ordinary managed worktree, and **Fold** tears it down through the unchanged reference-counted path.
_Avoid_: orphaned worktree, scratch worktree, vacant worktree

**Fold conflict assistance**:
The attended agent session **Fold** offers when merging trunk into the set's branch conflicts. It requires a TTY and is unreachable from the **Queue** or any daemon — an unattended resolver moving trunk is the failure mode that got worktree-set integration deleted in the first place. It resolves inside the set's own checkout, never in the **Trunk worktree**, after which fold retries the fast-forward.
_Avoid_: auto-merge, conflict bot, mergeability check

**Checkout claim**:
The occupancy that makes a checkout busy for admission: a live running **Drain** (execution), a live **Recovery waiter** (quota recovery), or a **Checkout gate hold** parked at a **Failed gate prompt** over uncommitted work. Derived as a read-side union — no claims table — and enforced at three chokepoints: `BeginDrain`, **Queue** dispatch (as spawn deferral with a reason), and **Recovery turn** acquisition. Every claim source is liveness-backed (owner PID + start token), so a dead owner's claim is swept by reconcile rather than wedging the checkout. Human waits other than a dirty Failed gate — the **HITL gate prompt**, the verify-fail gate, approval tasks — deliberately claim nothing: a human reading a menu must not stall the queue.
_Avoid_: runtime lock (umbrella sense), checkout lease, gate hold (as blocker), occupancy flag

**Recovery block reason**:
Why an eligible Recovery waiter (cooldown elapsed) was not granted a Recovery turn on its checkout, computed inside the same acquisition transaction: a kind — gate hold, live drain, turn held, or behind another waiter — plus the blocking set's ID. Surfaced in the Agent quota recovery wait status line so a post-cooldown wait names its blocker instead of claiming the quota is still the cause.
_Avoid_: block cause, wait reason, denial reason

**Interrupt gate prompt**:
The interactive menu shown when SIGINT (Ctrl-C) interrupts a live **Drain** on a TTY, instead of the drain exiting 130. The fourth sibling of the **HITL gate prompt**, **Failed gate prompt**, and **Verify-fail gate prompt**: the signal first tears down the running agent attempt (graceful SIGTERM→SIGKILL, persisting the **Interrupted task**'s **Captured run**), parks the **Runtime execution lock** and registers a checkout gate hold, then presents 1 Continue draining / 2 Get agent assistance / 3 open **Runtime shell** / 0 Exit. Continue re-acquires the lock and re-runs the interrupted task carrying its attempt digest forward (ADR-0091), then keeps draining; assistance and shell are side-trips that return to this menu; `0` is exit. Shown on any interactive TTY drain — foreground or a **Queue**-spawned pane; `--yes` and non-interactive input keep today's teardown-and-exit with no menu.
_Avoid_: interrupt menu, SIGINT prompt, Ctrl-C gate

**Interrupt auto-drain revocation**:
Interrupting a live **Drain** clears the set's **Auto-drain** consent bit unconditionally at the moment of interrupt — the human is taking manual ownership, so `pop queue run` stops re-firing the set. The pre-interrupt value is snapshotted: choosing Continue at the **Interrupt gate prompt** revives it (announced to the user), so a peek-and-continue leaves consent unchanged, while Exit or a crash-at-gate leaves it cleared. Net: consent is truly discarded only when the human does not resume, yet it is cleared throughout the at-gate window so the daemon cannot grab the set before the human decides. Re-enabling after Exit is a fresh human mark. Distinct from **Auto-drain clearing**'s terminal (DONE/AWAITING-APPROVAL) trigger and unrelated to clearing on pick-up (rejected, see ADR-0120).
_Avoid_: interrupt auto-drain clear, clear-on-pickup, queue stop-on-interrupt

**Kimi quota signals**:
The stable stderr substrings that gate **Agent quota detection** for the `kimi` preset, matched case-insensitively against the full raw capture (kimi writes quota diagnostics to stderr, never to its stream-json): `usage limit for this period` (5-hour rolling window), `usage limit for this billing cycle` (weekly cycle), and `monthly usage limit` (monthly shared quota). Transient overload and concurrency 429s (`engine is currently overloaded`, `too many requests`) never gate detection — kimi retries those internally first. kimi's error texts carry no reset hint, so **PauseResetAt** derivation is fixed signal backoff plus the **Quota assurance offset**: one hour for the 5-hour signal, one day for the weekly signal, seven days for the monthly signal.
_Avoid_: kimi rate limit, kimi 429

**Plan gate**:
An agent's report that the resolved model is permanently unrunnable on this account — concretely kimi's HTTP 401 `does not have access to …` (`provider.auth_error`) when an **Effort ladder** entry names a subscription-gated model such as `kimi-k2.7-code-highspeed`. A plan gate triggers **Agent fallback** fall-through like an **Agent quota pause** but records no cooldown: the gate is deterministic for that account+model, so re-probing is cheap and the pause machinery's reset-time semantics do not apply. It is not a task failure — the next preset simply takes the task.
_Avoid_: subscription error, auth failure (generic 401s are not plan gates)

**Run spend**:
The token and dollar accounting of one **Captured run** — input, output, cache-read and cache-write tokens, plus cost where the adapter reports it. Tokens are the primary unit and are always present for a structured adapter with a **Usage extraction rule**; cost is captured only where an adapter ships it for free (currently pi's per-message `cost.total`) and is never inferred from a pricing table. Derived at read time from the stored events, never recorded as its own field.
_Avoid_: token usage, cost, billing

**Usage extraction rule**:
A per-adapter statement of **where a stream's authoritative usage lives and whether it accumulates or replaces** — the seam that turns raw events into **Run spend**. claude emits a per-API-call `usage` block on every assistant message (sum them); cursor emits one whole-run total on the terminal `result` event (read it, sum nothing); pi emits a cumulative block on every `message_update` delta and a settled one on `message_end` (sum `message_end`, ignore deltas). It is deliberately not a field-name translation table: names map cleanly while accumulation semantics do not, and getting the latter wrong produces a plausible wrong number rather than an error. An adapter without a rule yields **Token-blind run**s.
_Avoid_: usage parser, token field map, usage schema

**Token-blind run**:
A **Captured run** whose adapter reports no usage, so its **Run spend** is unknown rather than zero. Token-blind runs are counted and reported alongside every total they sit behind, so a figure is never quietly understated by runs it could not measure. A new adapter with no **Usage extraction rule** produces them; that visibility is the intended mitigation, not a defect.
_Avoid_: zero-usage run, unmeasured run, missing usage

**Spend lens**:
The `pop tasks spend [TASK_SET]` command — the read-only cross-set lens over **Run spend**, and the second lens over the same substrate as **Attempt stream replay**. Bare, it rolls up the ten most recent **Task set**s, one row each, sorted by total tokens; with a `TASK_SET` it breaks that set down per task, listing verification runs as their own rows. `--json` emits the same data machine-readably. The headline metric is **tokens per completed task**: every implement attempt charges to its task, including failed and retried ones, so retry waste stays visible instead of averaged away; verify runs have no task and charge to the set. It captures nothing and never mutates.
_Avoid_: tokens command, usage report, cost report, rollup

**Assist session**:
A human-in-the-loop session opened on an arbitrary **Task set** at its current derived status, without draining or re-running the **Verifier**. It presents the gate menu that status calls for — the **HITL gate prompt** for a **Human-blocked** or **Awaiting-approval Task set**, the **Verify-fail gate prompt** for a **Verify-failed Task set**, the **Failed gate prompt** for a failed one, and a generic assistance menu otherwise, which offers **Fold** as well when the set is Done and still bound. Entered from `pop tasks assist <set>` inline in the current terminal, or from the **Queue dashboard**, which spawns a `<task-set>-assist` pane in the **pop-queue** window (one per set — an existing pane is jumped to, never twinned). It runs under **Binding-first runtime resolution**, refuses while the set's drain is live, and registers a non-claiming **Checkout gate hold** for its duration. Being a human session it requires a TTY, and refuses headless rather than degrading — a **Missing** or **Archived Task set**, or a mismatch between the **current checkout**'s repository and the set's, refuses likewise.

**Assist prompt**:
The context loaded into an **Assist session**'s agent assistance. It identifies the **Task set** and its **Task storage** path, its derived status, the manifest listing (per-task status, type, effort, and blockers), the **Worktree binding** and **Runtime path**, recent progress, the latest **Verify verdict** findings, the task contract the agent must respect, and the operations it may perform. Task bodies are not inlined — the agent reads them from **Task storage**.
_Avoid_: set dump, full context load

**Remediation gate blocker**:
The `blocked_by` edge added from every **open** HITL task in a **Task set** to a newly spawned **Remediation task**, so the set's human gates cannot be signed off while agent remediation work is still pending. It changes no derived **Task set status** (an open remediation already makes the set READY): what it buys is that a manual **Complete task** on a gate is refused until remediation lands, and the gate's declared scope names the remediation work. HITL tasks already Done or Skipped are never rewired, and existing sets are not backfilled.
_Avoid_: gate dependency, approval lock

**Remediation review block**:
The section printed on-screen at a **HITL gate prompt** and a **Verify-fail gate prompt** listing each done **Remediation task** in the set by title with its **Completion sentinel** summary — what the agent claims it fixed. It exists because the human at a gate previously saw none of this: the summaries reached only the assisting agent's **HITL assistance prompt**, never the terminal. Scoped to remediation work alone, so it never buries the gate task body the human must act on.
_Avoid_: completed work dump, progress log

**Remediation history block**:
The prompt section carrying every done **Remediation task** in the set — title plus its capped **Completion sentinel** summary — into two consumers: each later AFK task attempt in that set (so a second remediation never re-treads the first blind, and a reopened task knows where the **Verifier** already caught the set), and the **Verifier** prompt itself. In the Verifier prompt it is framed like the prior human note: always present when remediations exist, labelled as the implementer's unverified claims with the work diff authoritative. It is pop's one cross-task prompt channel — every other history a task attempt sees is scoped to that task alone. The terminal-facing counterpart the human reads at a gate is the **Remediation review block**.
_Avoid_: progress digest, prior attempt digest, self-report

**Verifier summary line**:
An optional `SUMMARY: <one line>` in the **Verifier**'s response contract, naming in one line why remediation is needed. It becomes the spawned **Remediation task**'s title as `Remediation <cycle>: <summary>` — single line, sanitized like a human note, capped around 72 characters. A human-origin remediation has no Verifier line, so its title comes from the first line of the human's `--remediate` note under the same cap. When neither source exists the task falls back to a generic title, because an unparseable verdict is a far worse failure than a vague title. Distinct from the **Completion sentinel**'s summary, which reports post-hoc what a remediation attempt actually did.
_Avoid_: findings headline, verdict summary

**Done inclusion**:
The hidden-by-default treatment of DONE **Task set**s on both Queue read surfaces. A DONE set — any, including one still holding a managed **Worktree binding** — is omitted from `pop queue status` and the **Queue dashboard** unless included via the `--include-done` flag (both surfaces; sets the initial state) or, in the dashboard, the **Show done** toggle in the **Queue dashboard filter menu** (a session-only view state). It hides only DONE and is a view filter, not persisted consent — distinct from **Archive**, which persists on **Task state** and hides a set in every status. Replaces the earlier rule that kept DONE sets holding a managed Worktree binding visible as a teardown reminder; that reminder now lives behind Done inclusion, with teardown still gated at **Archive**.
_Avoid_: done carve-out, show-done flag, done filter, done reminder

**Queue surface sort order**:
The single row ordering shared by `pop queue status` and the **Queue dashboard**. Precedence: (1) a **live-drain** tier, then (2) an **auto-drain** tier, then (3) an **orphaned** tier (Worktree binding pointing at a missing checkout) — each floating above the status scheme; then (4) the status scheme itself. In the status scheme, **IN Progress** and **READY** rows float cross-project as two leading bands (each ordered by Project ascending, then Task set identifier descending), and every remaining status groups by **Project** first, then by status in the order AWAITING-APPROVAL, NEEDS-VERIFY, VERIFY-FAILED, FAILED, BLOCKED, DEFERRED, DONE, MISSING/MALFORMED, then Task set identifier descending. So needs-you and idle work is read per-project, while running and ready work is read across all projects at a glance.
_Avoid_: dashboard sort tiers, running tier, project-grouped sort

**Queue dashboard filter menu**:
The modal popup opened with `f` on the **Queue dashboard**, holding row-inclusion filter toggles — currently a single **Show done** toggle (see **Done inclusion**), extensible to future inclusion filters (by status, by project). A sibling of the `a` action menu. Its state is a session-only view setting, reset to default (done hidden) on relaunch. Distinct from `/`, the fuzzy text filter, which is a transient query over already-included rows rather than an inclusion setting.
_Avoid_: filter dialog, search popup, done toggle key, Show-done key

**Dashboard verify verb**:
The `v` verb in the Queue dashboard action menu that spawns a pane running `pop tasks verify <set> --task-runtime-path <checkout>` on a NEEDS-VERIFY or VERIFY-FAILED set — a manual, un-locked Verifier force that reuses the drain pane-per-set tagging but records no Runtime execution lock or spawn intent, and is hidden on live-drain rows because a plain verifier run is not quiescence-gated.
_Avoid_: re-verify button, verify key, verify action

**Handoff verb**:
A **Work dashboard** action that hands the operator's attention to a tmux pane it spawns or focuses, rather than acting in place. Every handoff verb performs the same steps in the same order — spawn the pane, or focus the existing one when that activity is already running for the set rather than re-sending the command into it; focus (`SelectPane` + `SwitchClient`); quit the dashboard — so no verb invents its own post-spawn behaviour. It is bound to an **uppercase** key, which is the operator's only cue that the key navigates away; lowercase keys act in place and leave the dashboard open. A handoff may put one step in front of itself (a picker modal) and still be a handoff. When it moves the operator nowhere (no pane to focus, no checkout bound, ineligible row, focus unavailable outside tmux) it does not quit: it reports why in the dashboard's status line and stays put.
_Avoid_: dashboard action, launch verb, spawn action

**In-place verb**:
A **Work dashboard** action that mutates state or opens a nested UI without moving the operator anywhere — bind, unbind, auto-drain toggle, status writes, archive, unpark, copy name. Bound to a **lowercase** key. The counterpart to a **Handoff verb**; the case of the key is the whole distinction.
_Avoid_: local action, quiet verb

**Live-pane affordance**:
The colouring of a **Handoff verb**'s key — in the **Work dashboard** action menu and, as a compact per-activity cluster, on the row itself — showing what that activity's pane for this set is doing, and thereby what the key will do. Dark: no pane, the key spawns one. Grey: a **Pane tag**ged pane exists but sits at a bare shell, its command finished — the key respawns. Green: the command is running — the key jumps to it. Read from tmux once per dashboard poll, never from pop's own store, because a pane that dies leaves `list-panes` at once while a stored record outlives it. It replaces a separate preview verb outright: the verb that starts a thing is the verb that returns you to it. It covers only the activities pop supervises — drain, verify, fold, assist, and the wayfinder session (keyed by its window name, which is the map id, rather than by a tag). A **Runtime shell** is the operator's process, not pop's: it is never tagged, never tracked, always dark, and every press spawns a fresh one.
_Avoid_: pane indicator, preview, live badge

**Pane tag**:
A per-pane tmux option (`@pop_*`) pop writes onto a pane it spawns, naming the key that pane serves — a routine id or **Task set** id. It lives in tmux, not in pop's store, so it outlives the pop process that set it, and it is how pop later answers "which pane belongs to this?" without bookkeeping of its own. **One tag per activity, not one per set**: drain, verify, fold, and assist each get their own, so a lookup can never return another activity's pane and send keystrokes into a process already running there.
_Avoid_: pane marker, pane label, pane option

**Pinned action menu**:
The **Work dashboard** action menu opened with `A` rather than `a`: it survives each **In-place verb** it fires and re-filters as `J`/`K` move the row cursor beneath it, so one verb can be swept down many rows. A **Handoff verb** fired from it still hands off and still quits — the pin is a convenience for verbs that stay, and exempting handoffs from their own contract inside a mode is the per-verb inconsistency this design exists to remove. `A`, like `/`, `G`, and `gg`, is a **mode** key: the verb case rule governs row verbs only, so a capital mode key that moves the operator nowhere is not an exception to it.
_Avoid_: sticky menu, persistent menu, multi-select

**Status submenu**:
The nested list of Task-set status verbs inside the Work dashboard's action menu, opened with `s`: complete, open (reopen), skip, archive, unarchive. Each shells out to its `pop tasks <verb> <set>` command so the whole-set **Multi-task selection** runs in a real terminal instead of a second in-dashboard picker. Its sibling `S` is assist. Inert on **Wayfinder map** rows.
_Avoid_: status menu, state menu, verbs menu

**Copy-name verb**:
The `y` verb on every **Queue dashboard** level, copying the cursored row's identifier via **Clipboard copy** and always reporting a transient status confirmation. Payload follows the level: a bare **Task set identifier** on a task-set table row, the map id on a Wayfinder Map row, a **Task target reference** (`<task-set>/<file>.md`) in the **Task set detail view** and **Task text peek**, and a bare ticket id on a Map ticket. Bound both as a direct keypress and as an action-menu entry, so it is discoverable without being slow to reach.
_Avoid_: yank verb, copy id, clipboard verb

### Routines

**Routine**:
A recurring unit of unattended agent work — an optional **Routine schedule** plus a prompt plus a per-Routine **Routine memory** — that fires **Routine run**s over time and never reaches a terminal status. Deliberately not a **Task set**: the Task set lifecycle is terminal (DONE, **Auto-drain clearing**, **Archive**) while a Routine is a generator (ADR-0119). A Routine is **directory-bound**, not repository-bound: creation records the directory it was created from as its bound directory, which may be any directory including non-git ones (e.g. `$HOME`), so **Repository identity** is not required. It lives as a directory artifact `routines/<id>/` in pop's data dir beside `repos/` — `prompt.md`, `memory/`, `runs/`, and a slim `state.json` — which doubles as the **Queue daemon**'s discovery registry. The artifact is split by ownership: authored intent (the optional schedule, the runtime agent list, effort) rides in YAML frontmatter at the top of `prompt.md`, the same frontmatter shape a **Project routine** uses, while machine state (the pause bit with reason, created-at, and the bound directory as a registry fact) stays in `state.json`; one intent format everywhere means agent refinement edits one file and project-to-authored copying is a file copy, and the **Run-affecting fingerprint** splits the file accordingly (frontmatter = settings, body = prompt). A Routine is **created paused**: authoring alone is not consent to fire — the human proves the flow with manual fires and then resumes it, and the persisted pause bit (`pop routine pause`/`resume`) is the only enable/disable state (no separate draft state). The pause bit carries a reason (created / manual / failure / changed) surfaced by the **Routine dashboard** and refinement loop. The daemon fires only Routines that are non-paused, scheduled, **and** anchored by at least one prior non-skipped run; a never-fired Routine is never daemon-fired, so the first fire is always a human act. An **unscheduled** Routine is a durable, first-class state — manual-fire-only, never daemon-fired regardless of pause bit — not merely a transitional gap before a schedule is set. A Routine is editable after creation — prompt, schedule, runtime agents, and effort; the bound directory and id are fixed at creation (change either by delete + re-add). Validated write paths (`pop routine edit --schedule/--agent/--effort`, the dashboard modals) remain the mandated route and rewrite frontmatter. Editing any run-affecting input pauses the Routine (reason: changed) **only once it has fired at least once**; a never-fired Routine keeps reason created through edits. Resume is never refused, but resuming untested is on the human. A live run keeps the prompt it already read. Invalid frontmatter — unparseable YAML or a schedule the parser rejects — suspends only that Routine with a warning on the daemon log and dashboard, never the whole registry. CLI surface: `pop routine new` (scaffolds the dir paused, then drops into the **Routine refinement loop**; renamed from `add` — no alias kept), `edit` (bare form enters the same loop), `list`, `pause`, `resume`, `fire` (manual immediate run), `runs`, `handoff` (emits the **Routine handoff** prompt on stdout), and the **Routine dashboard**. Routines never appear as **Work dashboard** task-set rows.
_Avoid_: typed task set, cron task, recurring task set, scheduled task

**Routine schedule**:
The **optional** recurrence declaration in a **Routine**'s `prompt.md` frontmatter — not cron syntax, but one clause production with fixed order: `[every <N><unit>] [on <days>] [at H[:MM]] [utc]`, every clause optional and at least one required when a schedule is present at all. A Routine may carry no schedule (**unscheduled**): `pop routine new` accepts a missing `--schedule`, and a set schedule is cleared back to unscheduled with `pop routine edit <id> --schedule ""` or an empty submit in the **Routine dashboard**'s edit-schedule modal (clear-to-unset, mirroring the agent/effort modal's semantics). The parser itself still rejects an empty expression — absence is handled before the parser, never persisted as a parseable form. The **Queue daemon** never fires an unscheduled Routine; it stays manual-fire-only (`pop routine fire`) indefinitely, and `list` and the dashboard's SCHEDULE column render the absence as `manual`. See ADR-0134. When present, a schedule parses into two shapes: a **rolling** schedule (`every` with an `h`/`m` unit, fires at last-fired plus the interval) or a **slot** schedule carrying a day step, a 7-bit weekday mask, and a time of day. Days accept ranges (`mon-fri`, non-wrapping), comma lists, and `weekdays`/`weekends` sugar that does not mix into a list. A slot's next fire is the slot time on the last-fired date, pushed `step` days on if that instant has already passed, then advanced to the next masked day. Wall-clock forms are **machine-local** unless suffixed `utc`; stored instants remain UTC and only slot computation converts. `daily at H[:MM][ utc]` remains a permanent parse-only alias — expressions are stored exactly as typed, never normalised. Editable after creation via `pop routine edit <id> --schedule "<expr>"` and via the **Routine dashboard**'s edit-schedule action; both paths validate through the same schedule parser before writing, and one exported grammar constant feeds the parser error, both flag helps, the empty-list hint, and the **Routine refinement session**'s framework contract. The daemon reads last-fired from the **Execution-state store**; a Routine with no non-skipped run yet is never due (the anchor is the first manual fire). For an anchored routine, a missed fire catches up exactly once immediately. See ADR-0133; zone rule per ADR-0126.
_Avoid_: cron expression, crontab

**Routine run**:
One firing of a **Routine**: an agent invocation executed in the Routine's bound directory, spawned into a tmux pane like a **Drain** (the project session for git-backed directories; a pop-created `routines` session for non-git ones), producing a per-run report file under the Routine's `runs/`. Pop wraps the routine's prompt with a standard preamble/postamble — memory path plus read-memory-first, report path plus write-report-and-update-memory, plus the completion-sentinel contract: the agent must end its output with `ROUTINE_COMPLETE` or `ROUTINE_FAILED: <reason>`. Run outcome is assessed, not inferred from exit status alone: exit 0 with `ROUTINE_COMPLETE` and a report on disk → succeeded; `ROUTINE_FAILED` → failed with the sentinel's reason; exit 0 with no sentinel → failed (missing sentinel); no report file → failed (missing report); nonzero exit, exec error, crash-reconcile, and quota-exhaustion remain fail reasons. Any failed run — daemon-fired or manual — pauses the Routine (reason: failure); a broken Routine never keeps firing on schedule. Each run row records the **Run-affecting fingerprint** in effect when it fired. Agent resolution: the Routine's own frontmatter agent list when set, else `[routines].agents`, else the resolved implement list — an ordered fall-through sharing the machine-global cooldown store; the chosen preset's model is picked through the `[effort.<agent>]` ladder using the Routine's effort (default standard). Assessment/report only in v1: a run never emits a **Task set**. A Routine run takes no **Runtime execution lock**; its only exclusivity is per-Routine — a fire due while the previous run of the same Routine is still live is skipped and logged, never queued. Run lifecycle rows (fired-at, outcome, fail/skip reasons, report path) live in the **Execution-state store**; report content stays on disk, retained indefinitely in v1.
_Avoid_: routine drain, routine attempt, cron job

**Routine memory**:
The per-**Routine** persistent, agent-managed workspace directory (`memory/` in the routine's storage). The run preamble points at it; the agent reads it before working and writes back what it learned (e.g. already-assessed error IDs), so successive **Routine run**s don't re-process the same items. Pop provisions the directory and never parses its content.
_Avoid_: pop memory, task set memory, global memory

**Run-affecting fingerprint**:
The canonical hash of a **Routine**'s explicitly-set run-affecting inputs — `prompt.md` content, schedule, runtime agent list, effort — in fixed key order with unset fields omitted, so adding a new criterion later never mass-pauses existing Routines (a fingerprint only changes when a human sets something). Recorded on every **Routine run** row; the **Queue daemon** compares the current fingerprint against the last run's before firing and on mismatch pauses the Routine (reason: changed) instead of firing. CLI/dashboard write chokepoints also pause eagerly for immediate feedback; the fingerprint is the safety net that catches direct `prompt.md` edits (e.g. by the **Routine refinement session**).
_Avoid_: config hash, prompt hash

**Routine dashboard**:
The interactive `pop routine dashboard` TUI — sibling view of the **Work dashboard** in the same shell, toggled bidirectionally with `v`; each command opens its own view. One row per **Routine**: ROUTINE, DIRECTORY, SCHEDULE, LAST RUN, STATUS. STATUS shows liveness/pause first (running / paused-with-reason), and for an idle Routine the last run's outcome (ok / failed), so a schedule failure is visible without drilling in; SCHEDULE renders an unscheduled Routine as `manual`. Keys mirror the queue grammar, including its action-menu pattern: `a` opens a layered action overlay on the focused row whose verbs are fire, pause/resume, preview, edit prompt (suspends the TUI into `$EDITOR` on `prompt.md`), edit schedule (a modal text input pre-filled, validated on enter, an empty submit accepted as clear-to-unset), agent/effort (`g`, a modal editing the Routine's runtime agent list and effort), refine, `y` handoff (assembles the focused Routine's **Routine handoff** prompt and copies it via the shared clipboard helper), and runs. The refine verb does not suspend the TUI: it spawns the **Routine refinement loop** whole into a tmux window and the dashboard stays live, so rows refresh from the **Execution-state store** on the normal tick rather than on a gate return (ADR-0132); outside tmux the verb refuses with a pointer to `pop routine edit <id>`. Direct shortcuts stay for `i` fire now, `p` preview the run pane, `l`/Enter open the runs-list detail view (open a report peek from there), and `c` copy-report-path: in the main view it copies the focused Routine's latest report absolute path, in the runs list the focused run's, in the report peek the viewed report's — no report yet → status-line note, no-op. `h`/left/esc back, `gg`/`G` top/bottom. The runs detail view shows fail reasons alongside outcomes. A Routine whose state or frontmatter fails to load renders as a warning row rather than erroring the whole view.
_Avoid_: routines picker, cron dashboard

**Routine refinement loop**:
The HITL gate a **Routine** is worked in before it earns its schedule: entered automatically by `pop routine new` after scaffolding, by bare `pop routine edit <id>`, and by the refine verb in the **Routine dashboard** action menu. It follows the house gate grammar — a numbered line-based menu (`Choose [1]:` via the shared prompt-line reader, word aliases like `fire`/`resume`/`quit` accepted, static default 1), not the dashboards' single-key TUI grammar: 1 opens a **Routine refinement session** (default, blocking until the agent exits), 2 fires a real test run streaming in the terminal (a normal **Routine run** — it records a run row, keeps its report, and becomes the schedule anchor; there is no dry-run variant), 3 opens the last report in `$PAGER`, 4 opens `prompt.md` in `$EDITOR`, 5 edits the schedule, 6 resumes the Routine and exits, 0 exits leaving it paused. The loop always runs as a plain CLI process owning its terminal outright: entered from the dashboard it is **spawned whole into its own tmux window** (`pop routine edit <id>`, window named after the Routine id, in the session derived from the bound directory, reused and switched to when present, nothing sent into a live one) rather than suspending the TUI into it, because a dashboard-hosted gate cannot reliably hand the keyboard to the agent it launches (ADR-0132); outside tmux that path refuses and names `pop routine edit <id>`. The loop is how the created-paused discipline resolves: iterate until a manual fire looks right, then resume.
_Avoid_: wizard, setup mode

**Routine refinement session**:
The interactive agent chat spawned from the **Routine refinement loop**'s agent-session choice (menu item 1, the default), launched inline in the Routine's bound directory so it can probe the repo and any MCP tooling (e.g. live JQL). It is always inline in whatever terminal the loop itself owns — the loop, not the session, is what tmux-spawns when entered from the **Routine dashboard** (ADR-0132). Front-loaded with rules embedded in the pop binary: the routine framework contract (prompt wrapping, **Routine memory** dir, report path, schedule grammar including local/utc, and that the schedule is **optional** — an unscheduled Routine is a valid durable end state, manual-fire-only, with a cadence settled in conversation and set via `pop routine edit <id> --schedule` when the human wants one), the Routine's concrete paths, and a six-item checklist (goal, data source, definition of seen/new, memory format, report format, empty-run behavior). The session is **mode-aware**, branching on `prompt.md` content — the `pop routine new` stub or blank is create mode (checklist as written), anything else is revise mode: the current `prompt.md` is embedded verbatim in the briefing and the checklist is reframed as an audit (what does the prompt already settle; ask only about wanted changes and genuine ambiguity). Nothing else is embedded — last report, memory contents, and run history stay for the session to read itself. The agent edits `prompt.md` directly and changes the schedule only through `pop routine edit <id> --schedule` so parser validation holds. Preset resolution follows `[routines].agents`; a `--refine-agent <preset>` flag on `new`/`edit` overrides it for that loop session's chats, and is forwarded when the dashboard spawns the loop.
_Avoid_: routine authoring session, prompt wizard

**Routine handoff**:
A prepared continuation prompt for a fresh agent session, assembled from a **Routine**'s artifacts so a human can act on what the Routine has been collecting (e.g. "fix all the bugs this routine found"): the Routine's current `prompt.md` embedded inline (the framing), the latest **Routine run**'s report referenced by absolute path with its outcome and any fail reason noted, the **Routine memory** directory referenced by absolute path with a read-this-directory instruction, and the bound directory named as where the work happens. Built from the last run row regardless of outcome (`LastRoutineRun`); a Routine with no runs yet still hands off, with the report section replaced by a no-runs-yet note. The prompt is a pure context brief — it bakes in no task and closes by saying the user will follow up with instructions, since the ask varies per use. Surfaced as `pop routine handoff <id>` printing to stdout (pipeable into another agent) and as the `y` verb in the **Routine dashboard** action menu, copying via the shared clipboard helper. Distinct from the dashboard's `c` copy-report-path, which copies only a path.
_Avoid_: routine copy, routine export

**Project routine**:
A manual-only **Routine** variant a repo ships in-tree: each committed `.pop/routines/<name>.md` in a checkout is one Project routine — the filename is its name (validated by the same rules as authored Routine ids; an invalid or unparseable file is warned about and skipped, never fatal), the file body its prompt, with optional YAML frontmatter carrying `agents` and `effort` only (a `schedule` key or any unknown key is warned about and ignored — the schedule ban is by design). It is virtual, never registered: pop discovers it live from the checkout at list/fire time; the file is the sole source of truth and nothing is copied into the data-dir `routines/` registry, so the **Queue daemon** never sees it. It carries no schedule (setting one is rejected, surfaces render `manual`), no pause bit, and none of the created-paused / anchor ceremony — firing is always a human act, in the checkout it was triggered from, with the same run wrapper and completion-sentinel contract as any **Routine run**. Per-user state — run rows in the **Execution-state store**, **Routine memory**, run reports — lives in pop's data dir under a separate `project-routines/<checkout-key>/<name>/` root (never under `routines/`, which is the daemon's discovery registry), keyed per-checkout (canonical checkout path + name), never inside the project: teammates and sibling worktrees share the definition via git but never share memory or run history. CLI addressing: an authored Routine id wins an exact-name match; `project:<name>` addresses a Project routine explicitly (and is the escape hatch when shadowed). Pop may edit the prompt file (including agent-assisted refinement) but never commits the change — committing is the user's act. Surfaces list a checkout's Project routines only when invoked from inside it, visually marked as project-origin so they read as distinct from authored Routines; the CLI list renders pause state as `-` for them.
_Avoid_: virtual routine, .pop routine, project-local routine

### Wayfinder

**Wayfinding**:
The search phase of a large effort: resolving decision tickets one at a time until the way to a destination is clear. Produces decisions, not deliverables; implementation happens in Task sets a Map spawns. Driven by the wayfinder skill (forked from mattpocock/skills). pop owns the lifecycle — registration, the frontier, claiming, resolution, arrival — and the skills own only the HITL conversation; nothing is written into the repository under study for the whole life of a Map, so decisions reach it through **Mint** at handoff (ADR-0172).
_Avoid_: exploration, discovery (code term), planning effort, effort (that is the task strength knob)

**Map**:
The canonical artifact of one Wayfinding effort: a folder holding `map.md` (destination, notes, decisions-so-far index, fog, out-of-scope) plus its Decision tickets. A first-class concept beside Task sets, not a Task set kind — it never drains, and its membership grows and shrinks as fog graduates. Stored per-repository in Task storage under a `maps/` sibling of `tasks/`; the folder is the Map's content, and **Map registration** is what makes it Work pop looks after.
_Avoid_: wayfinder task set, plan, chart

**Map verb family**:
`pop map` — the one command family that reads and mutates Maps: `status`, `show`, `register`, `archive`, `unarchive`. Renamed from `pop wayfinder` as a hard cut with **no alias** (same discipline as the `pop queue` cut): kind nouns everywhere, and "wayfinder" survives only as the *skill's* name. Reads never create state; a Map's metadata is never hand-edited once the family owns it.
_Avoid_: pop wayfinder, map commands

**Decision ticket**:
One unit of a Map: a question whose resolution is a decision, recorded as `issues/NN-<slug>.md` with `Type:` (research/prototype/grilling/task), `Status:`, and `Blocked by:` lines, its answer appended under `## Answer` on resolution. Distinct from a task: no acceptance criteria, no agent commit, and a claimed state exists (persisted in_progress stays malformed for tasks).
_Avoid_: task (the Task-set unit), issue, question file

**Map manifest**:
The `index.json` beside a Map's `map.md` — the machine-readable half of a Map, mirroring the **Task manifest** so no consumer hand-parses metadata out of N ticket markdown files. Per Decision ticket it carries id, file, title, type, status (`open` | `resolved`; a claim is pop.db state, never a file state), `blocked_by`, `adr_drafts` and `context_drafts`, plus a Map-level `spawned_sets` array defaulting to empty. Blocking edges live here because they are definitional and travel with the content. Where one exists it is the source of truth for status, type and blocking; a Map without one still reads its ticket markdown headers. Validation names every problem — unknown status or type, a blocker naming no entry, an entry with no markdown file, a markdown file with no entry — and a failing manifest renders the Map Malformed.
_Avoid_: ticket index, map index file, wayfinder manifest

**Map status**:
The `Status:` line in `map.md` — `active` (default), `done` (way found; skill writes it at handoff), or `abandoned` (closed without reaching the destination). Declared, not derived: fog is prose, so "way is clear" is a judgment the session records. Orthogonal to **Map archive**, the reversible pop-side flag that hides old Maps from default views without deleting; deletion stays manual.
_Avoid_: map state, derived map status

**Work container registry**:
The machine-global `work_containers` table keying every registered Work container by (kind, id) — the one place that answers "does pop look after this?" for any kind. It carries the registration time and the `archived` bit, and deliberately no status: status is derived from files on every read, so a cached copy here would be a second source of truth. Maps are the first kind to key into it; Task sets migrate onto it from **Task state** later.
_Avoid_: work table, container index, map registry

**Map registration**:
`pop map register <map-id>` — the act that ends charting and makes a Map registered Work. It validates the **Map manifest** and, when clean, writes the Map's **Work container registry** row. A malformed Map is refused with *every* problem named at once and nothing written, so registration is a fix loop the session re-runs until it comes back clean; a second run on an already-registered Map is a no-op, never a reset. Explicit by design — no verb registers a Map as a side effect, because a lazy row-on-first-act would be a second, invisible registration path (the one exception is **Map archive**'s legacy fold, which is migrating a decision already made). Always **plain, never managed**: wayfinding writes nothing into the repository, so a Map has no checkout of its own, provisions no worktree, and `register` has no `--managed` flag.
_Avoid_: chart complete, map create, lazy registration

**Map archive**:
`pop map archive` / `pop map unarchive` — the reversible act of hiding a Map from default views and restoring it, written as the `archived` bit on the Map's **Work container registry** row, so a Map is filed away through the same mechanism a Task set is. Archiving is idempotent; unarchiving a Map that is not archived is an error. Both refuse an unregistered Map and name **Map registration**, since the bit rides a registration. The retired `wayfinder-archive.json` side-file folds into this bit on an ordinary read and is then deleted — the fold registers the ids it archives, because the bit exists nowhere else.
_Avoid_: hide map, delete map, archive file

**Spawned set**:
A Task set created from a Map's resolved decisions (via to-spec/to-tasks) once the way — or an early-splittable chunk — is clear. The forward link between the two concepts: the Map records the ids of sets it spawned; a spawned set's `spec.md` records its source Map. One Map may spawn many sets.
_Avoid_: child set, output set

**ADR draft**:
An ADR body written during a wayfinding session into the Map's `adrs/` directory, identified by an 8-hex id and carrying no ADR number. It is the single copy of the decision's repo-facing form; the ticket answer links it rather than restating it. A number is assigned only when a slice **Mint**s it.
_Avoid_: pending ADR, unnumbered ADR, ADR stub

**Context draft**:
The glossary ops (`+`/`~`/`-`) a wayfinding ticket settles, written into the Map's `context/` directory, one file per ticket. Identical in syntax to a `.grill-context` fragment and differing only in destination — the repo is not written during wayfinding.
_Avoid_: glossary draft, pending fragment, fragment stub

**Mint**:
To copy an **ADR draft** or **Context draft** out of the Map and into the repo as a numbered ADR or a `.grill-context` fragment. Performed by the slice that implements the decision, as an acceptance criterion, so the decision lands in the same commit as the change it describes.
_Avoid_: publish, materialise, fold

**Work dashboard**:
The unified `pop work dashboard` TUI: one machine-global table of what you are doing or planning, interleaving Task set rows (unchanged behaviour and keys) with Map rows per project. On a Map row `i` spawns an attended wayfinder session for the next frontier ticket (new window in the repo's tmux session, named after the Map); Enter/`l` opens the Map detail view. Subsumes the Queue dashboard; `pop queue dashboard` stays a hidden compatibility alias. The Queue daemon is untouched — Maps are invisible to it.
_Avoid_: queue dashboard, work board, working (that is the pane status)

**Map detail view**:
The drill-down entered with Enter/`l` from a Work dashboard Map row — mirror of the Task set detail view: the Map's Decision tickets with the frontier highlighted; `i`/Enter on a frontier ticket spawns an attended wayfinder session for that specific ticket.
_Avoid_: ticket list, map inspector

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
`.pop/config.toml` (in the repo's `.pop/` directory) and `[repo."<path>"]` (central, in global `config.toml`) decode ONE shared repo-scope key schema: authoring a repo-scoped setting in either place is equivalent, and adding a new repo key makes both accept it. Per the **Config merge order**, the user's central `config.toml` (including its `[repo]` blocks) outranks the committed `.pop/config.toml`. Repo scope is a curated set of genuinely repo-specific keys (`workbenches`, `preferred_workbench`), never a mirror of global config — `projects`, `queue`, and daemon knobs stay global-only. `trunk` is the one central-only exception (per-checkout machine topology, never in `.pop/config.toml`).
_Avoid_: project entry override, glob-scoped behaviour

**In-tree config anchors**:
How pop finds repo-scope in-tree config (`.pop/config.toml`): at two anchors — this worktree and the **Trunk worktree** (falling back to the **Repository identity** root for a bare repo). Presence decides: a worktree with its own `.pop/config.toml` overrides the inherited trunk one; a worktree without inherits trunk's, dynamically. Reuses the trunk resolver of **Preferred workbench** inheritance. The `.pop/` directory is the in-tree home for everything a repo ships to pop — config and **Project routine** prompts (`.pop/routines/`). A legacy flat `.pop.toml` is no longer read; its presence draws a one-line warning pointing at the new path.
_Avoid_: pop.toml inheritance, config walk, trunk snapshot

**Trunk worktree**:
A repository's single canonical fork base for managed **Worktree set**s. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must have one named, either hand-authored as a `trunk = true` per-checkout **Repo override** or recorded by pop into the runtime tier (`config.runtime.toml`) when `--trunk` names one at a managed register — the hand-authored value winning, and the trunk key itself never resolving through the trunk-anchored runtime layer. Managed worktrees fork from the trunk's HEAD; reconciling a completed worktree branch back into trunk is the human's own concern, not something pop does. A bare repo with no trunk from either source has none, so pop cannot provision a managed worktree there — it can only drain in place in whatever checkout the operator is currently sitting in.
_Avoid_: Execution base, execution_base, queue base, queue_base, default worktree

**Config finding**:
A single config-validation problem discovered during load, keyed to its config path (e.g. `effort.foo`, `projects[2].display_depth`) and carried on the loaded config instead of thrown. Surfaced two ways: as the `error` from the getter for that key, and as a non-blocking entry in the picker's warning banner.
_Avoid_: config error (when you mean a non-fatal finding, not unparseable TOML)

**Core capability**:
The one thing a command must produce to be worth running — e.g. the project list for `pop project dashboard`. A command aborts on a config problem only when a value it consumes is invalid *and* essential to this capability; every other config problem degrades to a default plus a warning, and the command still runs.
_Avoid_: command feature, required config

**Include**:
A sidecar TOML file the global `config.toml` pulls in via `includes`, carrying only a whitelisted subset of config — registered **Project**s, task config (`[tasks]`, plus the deprecated `[workload]` alias), per-agent **Effort ladder**s, **Repo override** blocks, workbenches, and `[workbench]` options — so a user can keep which directories they work on out of the main file. Precedence is parent first, then includes in listed order; the first definition of any whitelisted key sticks, and any non-whitelisted section in an include is warned about and ignored. Distinct from `.pop.toml`, which rides in a repo and describes one already-registered project.
_Avoid_: Import, partial, sidecar config, overlay

**Config show**:
`pop config show`: prints the effective configuration as pop resolves it from the current directory — includes merged, repo keys canonicalized to absolute realpaths, folder-local overrides (`.pop/config.toml` + the current `[repo]` block) collapsed into effective values, and the current repo's resolved **Trunk worktree** (config-declared *or* git-derived) surfaced as an effective `trunk`/`bare`. Run outside any repo, the current-repo/trunk section is absent. Effective values only, no provenance annotation. TOML by default, `--json` for machines. Reaches config + git (for the derived trunk), never the task-binding store. The value counterpart to `pop config keys` (the accepted schema); renders the result of config resolution.
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

**Session name derivation trade-off** — `project.SessionName` is the single source of truth for exact session names (linked worktrees, main checkouts, non-git paths). It calls git commands and is correct for all entry points that create, attach to, or kill sessions. The **dashboard** deliberately uses `project.FastSessionName` for history matching because exact derivation is too slow for a frequently-opened popup. `FastSessionName` is a pure string approximation (directory base + tmux-safe sanitization); it matches `SessionName` for main checkouts and non-git paths, but is inexact for every linked worktree (bare or non-bare) where the exact name is `repo/worktree`. See ADR-0005 and ADR-0157.

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
