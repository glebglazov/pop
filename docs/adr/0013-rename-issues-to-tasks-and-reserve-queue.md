# Rename issues to tasks; reserve "queue" for a global scheduler

Status: accepted

The "issue" vocabulary was inherited from Matt Pocock's original skill, where items became GitHub issues. Pop's items drifted into something else — agent-runnable work units with acceptance criteria, statuses, and dependencies — and the CLI naming had two tab-completion collisions: `run-issue`/`run-issues` (long shared prefix) and `pop worktree`/`pop workload` (shared first letter).

Decision: rename **issue → task**, **issue set → task set**, and the command family **`pop workload` → `pop tasks`**. The one-vs-many verbs become **`run`** (one task) and **`drain`** (whole set); the manual overrides drop their suffix and become **`open`** (named for its target status, replacing `reset-issue`), **`complete`**, and **`skip`**. The rename goes all the way down: storage moves to `repos/<repo>-<hash>/tasks/<set-id>/` with `state.json` (auto-migrated from the old layout on first touch), the manifest key becomes `"tasks"`, the implementation-commit prefix becomes `tasks(<set-slug>): <id>` — plural because the scope names a Task set, and `<set-slug>` is the Task set identifier without its timestamp prefix (`rename-issues-to-tasks`, not `2026-06-06-rename-issues-to-tasks`; the commit carries its own date) — and the planning skills become `to-tasks` and `run-task`.

The skill renames span both copies: the embedded skills in the pop binary (`cmd/skills/pop/`) and the chezmoi-managed dotfiles source (`~/.local/share/chezmoi/home/dot_agents/skills/{to-issues,run-one}` → `~/.agents/skills/`), which is the primary sync source for skills. `to-prd` keeps its name but its workload references update. Both must land in the same wave — renaming one side alone leaves skills referencing commands that don't exist yet (or no longer exist).

The word **"queue" is deliberately reserved** and unused today. A planned feature — pick the next task set across *all* projects by priority and run it — is the thing that word actually names. Naming today's per-repo namespace `queue` would either freeze the global meaning out or force a second rename. The umbrella term "workload" is retired entirely; the per-repo concept dissolves into "the repository's task sets".

## Naming rule

Sibling commands must be tab-completion-friendly: no long shared prefixes, ideally distinct first letters. This rule drove `run`/`drain` and `open`, and is expected to constrain future subcommand names. The existing s-cluster (`status`, `set-priority`, `show-path`, `skip`) is accepted as-is.

## Considered options

- **`pop queue` for today's namespace** — fits the per-repo scheduler semantics, but spends the word the future global scheduler needs.
- **`pop backlog`** — accurate for the per-repo pile, unique first letter, but the planned `pop tasks new "<query>"` (spawn a grilling agent from a prompt) reads wrong as `backlog new`; `tasks new` won by that margin.
- **`step`/`exec` verb pairs** — rejected for `run`/`drain`: "step" implies linear order the dependency DAG doesn't have; "drain" was already glossary language.
- **Renaming `worktree` → `tree` instead** — rejected; worktree is the canonical git term and pop's core domain.
- **CLI-only rename (storage keeps issue vocabulary)** — rejected; permanent drift between glossary, CLI, and disk is the disease being treated, and a single-user tool makes full migration cheap now.
