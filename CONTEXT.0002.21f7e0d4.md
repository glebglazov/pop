---
fragment: 21f7e0d4
generation: 0002
branch: master
---

- Worktree-ready project

- Execution base

+ Trunk worktree
  A repository's single canonical integration anchor and the fork base for managed **Worktree set**s. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must declare one explicitly via a `trunk = true` per-checkout **Repo override**. Managed worktrees fork from the trunk's HEAD and every non-trunk binding integrates back into it. An unconfigured bare repo has no trunk, so pop can neither provision a managed worktree nor integrate there — it can only drain in place in whatever checkout the operator is currently sitting in. Renames and reframes the former Execution base: the same per-checkout setting, recast from "the queue's execution checkout" to "the thing you integrate into."
  avoid: Execution base, execution_base, queue base, queue_base, default worktree
  under: Configuration

~ Drain routing
  Resolving which checkout a whole-set drain runs in. Precedence: an existing **Worktree binding** wins (the drain resumes in the bound checkout); otherwise an explicit Runtime-path override; otherwise the **current checkout**. Pop never auto-provisions a worktree while routing — provisioning is always an explicit operator act (`pop tasks implement --in-worktree`, the **Drain target picker**, or **Bind worktree**). **Implement** and the **Queue** share this policy; the Queue spawns into the repo's representative checkout — the **Trunk worktree** for an unbound set — so unbound AFK drains land on trunk and serialize on its lock. Parallel fan-out across a repo's sets is therefore **opt-in** via per-set bindings, never automatic. Single-task file runs stay current-checkout.
  was: Resolving which checkout a whole-set drain runs in, following fixed precedence: an existing Worktree binding wins; then an explicit Runtime-path override; then a managed worktree forked from the Execution base when the project is Worktree-ready and --inline is not set; otherwise the current checkout (trunk drains inline). Implement and the Queue share one routing policy; only error handling on provision failure differs by trigger.

~ Implement
  The single task-execution command, `pop tasks implement`, that runs tasks through the **Task executor** and dispatches by **Task target reference** shape. A `<task-set>/<file>.md` reference runs exactly that one task in the current checkout (Execution-confirmation prompt once). A bare set identifier — or no argument, choosing the highest-priority Ready set — **drains** the set with no AFK start prompt until it reaches Done, Blocked, Deferred, Failed, or an **Agent quota pause**; mid-drain HITL and Failed gate prompts stay interactive on a TTY. For a whole-set drain: a valid existing **Worktree binding** wins (resume in the bound checkout); otherwise pop **adopts the current checkout** as a never-delete adopted binding and drains there; `--in-worktree` instead provisions a **managed** worktree forked from the **Trunk worktree** and drains there. There is no automatic worktree default and no `--inline` flag — the current checkout is the baseline, and `--in-worktree` is the explicit opt-in to isolation. The interactive **Drain target picker** is a Queue-dashboard affordance only; bare `pop tasks implement` never prompts for a target, so the Queue's spawned drains never block. When a drain lands Done in a non-trunk checkout, an interactive run's completion prompt offers integration; `--yes` integrates only if the repo opted in with `auto_merge_clean`.
  was: ...Given a bare Task set identifier — or no argument — it drains that set... A valid existing Worktree binding wins for whole-set drains; `--inline` does not bypass it. For unbound whole-set drains in a Worktree-ready project, Implement defaults to draining in a managed worktree forked from the repository's Execution base; `--inline` forces a trunk/current-checkout drain. Running Implement inside an existing worktree adopts that checkout as a never-delete adopted Worktree binding...
  avoid: Run, Drain, separate one-vs-many verbs, --inline, auto-worktree default

+ Drain target picker
  The interactive chooser the **Queue dashboard** opens on `i` for an **unbound** Task set, fusing target selection with the drain into one bind-and-start action. It lists the repo's existing **non-managed** worktrees (pick → adopt as an adopted **Worktree binding**), a "new managed worktree" option (provision a managed binding forked from the **Trunk worktree**; the default cursor), and the **Trunk worktree** itself (drain inline, no binding). The chosen target is bound and then drained immediately. A set already holding a binding skips the picker and resumes in its bound checkout — retargeting requires **Unbind worktree** first. Options requiring a trunk (new managed worktree, trunk) are absent when no trunk is resolvable (an unconfigured bare repo). Managed and already-adopted worktrees are excluded from the existing-worktree list, since each checkout belongs 1:1 to one set.
  avoid: checkout picker, drain wizard, runtime picker
  under: Tasks and queue

~ Queue dashboard
  The interactive `pop queue dashboard` TUI — the primary hands-on surface for starting and managing **Queue** work, sibling to the **Project picker** and **Worktree picker**. Machine-global like `pop queue status`, it scans every registered repository's **Task storage** and renders one row per non-archived **Task set** with outstanding queue-actionable state, excluding only a concluded **Done Task set**. Each row shows the derived **Task set status**, the set's worktree/destination column (the bound checkout if bound, else the **Trunk worktree** — now an honest "where auto-drain lands"), a live **Picked-up** drain indicator, and an **Auto-drain** badge. Keys: `i` opens the **Drain target picker** for an unbound set then drains, or resumes silently in the bound checkout for a bound one; `I` integrate; `b` bind or create a worktree in advance (without draining); `U` unbind; `a` toggle **Auto-drain**; `p` preview the working pane; `gg`/`G` move to top/bottom; `h`/left/`esc` back or exit; `l`/Enter open the **Task set detail view**. `q` and the former `s` shortcut are intentionally unbound.
  was: ...Each row shows the derived Task set status, the set's branch, a live Picked-up drain indicator, and an Auto-drain badge; keys drain (`i`), integrate (`I`), bind or create a worktree (`b`), abandon (`U`)...
  avoid: Queue picker, queue status table, drain dashboard
