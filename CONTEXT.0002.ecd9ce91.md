---
fragment: ecd9ce91
generation: 0002
branch: docs/binding-integration-cleanup
---

- Integration backlog

- Integration target

- Mergeability

~ Integration
  An agent setup that connects a coding tool (Claude, Pi, OpenCode) to the monitor, so its pane self-reports status. Installed via `pop integrate <agent>`. This is the ONLY surviving sense of the word: pop no longer has any worktree-merge "integration" — reconciling a drained worktree branch into trunk is the human's own concern (a PR or a manual merge), and pop neither computes mergeability nor offers a merge action.
  was: An agent setup that connects a coding tool (Claude, Pi, OpenCode) to the monitor, so its pane self-reports status. Installed via `pop integrate <agent>`.
  avoid: Hook, plugin (when you mean the whole setup), worktree-set integration, merge reconciliation
  under: Integrations

~ Worktree directive
  An optional `worktree` key in a Task manifest declaring where the set should drain when the Queue runs it: `{ "managed": true }` provisions a managed worktree forked from the Trunk worktree, or `{ "name": "<worktree>" }` adopts the existing worktree of that name on this machine. It is a Queue-only seed: a foreground `pop tasks implement` ignores the directive entirely and binds the current checkout (see Implement). A Registration seed (read once into Task state at first registration, never re-read) and lazy: the first unbound Queue drain provisions or adopts and records a Worktree binding, after which the binding takes precedence. The portable identifier is the worktree name, never a path.
  was: An optional `worktree` key... Absent leaves the drain in the current checkout... Honoured by every drain, foreground Implement and Queue alike.
  avoid: worktree_ready, worktree mode, isolation flag

~ Worktree binding
  A durable association between one Task set and one git checkout for that set's execution, recorded in shared per-repository drain state and owned by a provisioning module both `pop queue run` and `pop tasks implement` call. A binding carries a `Provisioned` bit: managed (pop ran `git worktree add` under its data dir) versus adopted (a human pointed an existing checkout at the set, or a foreground implement adopted its current checkout). The single destructive teardown trigger for a managed binding is Archive (confirm-gated); managed teardown no longer rides on any "integration" event, and Unbind worktree never deletes. A foreground implement that rebinds a set away from an idle managed binding first prompts to delete that managed worktree. A managed binding's checkout lives at a stable path derived from the set identifier and persists across drain exits so re-spawns resume the same branch. If a binding's checkout is missing, pop refuses to spawn and directs the human to repair git state or Unbind — it never silently re-provisions.
  was: ...The bound checkout is either the project's trunk checkout or a worktree, and only worktree (non-trunk) bindings enter the Integration backlog... managed (pop tears the checkout and branch down on integration or Unbind worktree) versus adopted... Archive does not release a binding.
  avoid: Runtime path override, per-spawn worktree

~ Unbind worktree
  The human act of releasing a Worktree binding, leaving Task set task statuses untouched. It is ALWAYS forget-only and never destructive: even a managed binding's checkout and branch are retained — only the association is dropped. The symmetric inverse of Bind worktree, invoked via `pop tasks unbind-worktree` with a Task set identifier (or `U` in the Queue dashboard). Refused while the set actively holds the Runtime execution lock. To delete a managed worktree, use Archive with its delete-worktree confirmation. Unbind followed by Archive is the explicit "keep the worktree, file the set" path.
  was: ...What it drops depends on the binding's `Provisioned` bit: a managed binding's checkout and branch are torn down (pop created them); an adopted binding is forget-only.
  avoid: abandon worktree, release worktree, teardown

~ Archive
  The command `pop tasks archive` that files Task sets away as Archived Task sets. With no argument it opens a Multi-set selection of every non-archived registered set with only Done sets pre-checked; a bare Task set identifier archives exactly that set. Archiving a set that holds a managed Worktree binding additionally prompts `delete managed worktree? [y/N]`: on confirm pop deletes the worktree and branch and releases the binding; declining aborts the archive (to keep the worktree, Unbind first — non-destructive — then archive). Archiving an unbound, adopted-bound, or trunk-bound set is metadata-only and reversible as before. `--yes` archives the Done sets and, for any holding a managed binding, deletes their worktrees without a further prompt.
  was: ...Archiving several sets is one atomic Task state write and appends no Progress record. [no worktree teardown]
  avoid: Delete, Task set export, Remove registration

~ Archived Task set
  A registered Task set the human has filed away with Archive, recorded by an `archived` flag on its Task state registration entry. Archiving is reversible for the set's metadata, markdown, manifest, progress record, streams, and task statuses — all untouched. The one exception is a set that held a managed Worktree binding at archive time: its pop-created worktree and branch may be deleted by the confirm-gated teardown, which Unarchive cannot restore. Hidden from the Status table, automatic selection, and draining.
  was: ...its task markdown, Task manifest, Progress record, Captured attempt streams, and task statuses are untouched, so archiving is non-destructive and fully reversible.
  avoid: Deleted Task set, Done Task set

~ Drain routing
  Resolving which checkout a whole-set drain runs in, split by trigger. A foreground `pop tasks implement` always targets the current checkout: a live Runtime execution lock elsewhere refuses; otherwise it rebinds to the current checkout (dropping any prior binding — an idle managed binding prompts to delete its worktree, an adopted/trunk one is silently re-pointed), recording a Default binding to current. The Queue instead honours bindings and directives only: an existing binding wins; else the set's Worktree directive provisions a managed worktree (from the Trunk worktree) or adopts a named one; else the set is not drainable — it surfaces as a needs-bind fault, never an invented checkout. There is no Integration-target fallback. `--in-worktree` on a foreground implement provisions a managed worktree forked from the current checkout's HEAD.
  was: ...Precedence: an existing Worktree binding wins... otherwise the set's Worktree directive seeded at registration... otherwise a Default binding to the chosen checkout (the current checkout for Implement, the Integration target for the Queue)... Implement and the Queue share this policy (one RouteDrainCheckout).
  avoid: checkout picker, runtime resolver

~ Default binding
  The Worktree binding a foreground `pop tasks implement` records to the current checkout it ran in, making an otherwise-unbound (or rebound) set sticky to where it last ran in the foreground. The Queue records no default binding: with no binding and no directive it does not drain. An operator Bind worktree or a Queue-honoured directive still takes precedence for Queue routing.
  was: The Worktree binding a no-directive drain records to the checkout it resolved on its first run — the current checkout for a foreground Implement, the Integration target for a headless Queue drain.
  avoid: implicit binding, auto-bind

~ Implement
  The single task-execution command, `pop tasks implement`, that runs tasks through the Task executor and dispatches by Task target reference shape. A `<task-set>/<file>.md` reference runs one task in the current checkout. A bare set identifier (or no argument) drains the set in the current checkout: a live Runtime execution lock elsewhere refuses, otherwise it binds/rebinds the current checkout (Default binding) and drains there, ignoring any Worktree directive (directives are Queue-only). `--in-worktree` instead provisions a managed worktree forked from the current checkout's HEAD (previously trunk), binds the set to it, and drains there. There is no automatic worktree default. Completion is silent about merging: when a drain lands Done in a worktree, pop offers no integration — the human merges or opens a PR themselves, then archives to delete the managed worktree.
  was: ...otherwise pop records a Default binding to the current checkout and drains there; `--in-worktree` instead provisions a managed worktree forked from the Trunk worktree... When a drain lands Done in a non-trunk checkout, an interactive run's completion prompt offers integration; `--yes` integrates only if the repo opted in with `auto_merge_clean`.
  avoid: Run, Drain, --inline, auto-worktree default

~ Queue dashboard
  The interactive `pop queue dashboard` TUI for starting and managing Queue work. One row per non-archived Task set with outstanding queue-actionable state, plus Done sets that still hold a managed Worktree binding (shown as a clean-up reminder until archived or unbound). The destination column shows, with no glyph: the bound branch for a bound set; a colored `[managed wt]` badge for an unbound set carrying a `managed: true` directive (the Queue will provision one on drain); a dim `needs bind` for an unbound set with no directive (the Queue will not drain it). Keys: `i` bind-and-drain via the Drain target picker, or resume a bound set; `b` bind/create in advance; `U` unbind (forget-only); `a` toggle Auto-drain; `p` preview; `l`/Enter detail view. The former `I` integrate key is removed.
  was: ...Each row shows the derived Task set status, the set's worktree/destination column (the bound checkout if bound, else the Trunk worktree — an honest "where auto-drain lands"), a live Picked-up drain indicator, and an Auto-drain badge. Keys: ...`I` integrate...
  avoid: Queue picker, drain dashboard
