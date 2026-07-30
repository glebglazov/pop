---
status: accepted
amends: [ADR-0005]
---

# Every linked worktree's session name carries its repository prefix

> **Relates:** amends [ADR-0005](0005-dashboard-session-name-is-approximate.md) (the approximation it licenses now covers more cases) and fixes a dedupe defect against [ADR-0110](0110-managed-worktrees-surface-in-the-project-picker-via-a-filesystem-walk.md).

`TmuxSessionName` prefixed a worktree's session name with its repository name only when the repository was bare; a worktree of an ordinary clone got the bare folder name. For **Managed** worktrees — whose directory *is* the task-set id — that means a drain in a non-bare repo opens a session called `2026-06-22-runtime-shell` with nothing saying which project it belongs to, while the `pop project` entry for that same directory advertises `pop/2026-06-22-runtime-shell` (ADR-0110). The two never dedupe, so the picker can list a checkout as openable while a live session for it hides under another name.

Decision: **a linked worktree always carries its repository prefix — `repoName/worktreeFolderName` — bare or not.** A repository's own main checkout keeps its plain directory name; "linked worktree" means the git common dir lives outside this checkout, which is also what distinguishes the two cases. The prefix must come from the **common dir**, not from `rev-parse --show-toplevel`: inside a worktree of a non-bare repo, `--show-toplevel` returns the worktree itself, which would yield `runtime-shell/runtime-shell`. `DetectRepoContextFromPath` already runs `--git-common-dir` and reads only `core.bare` from it, so the correct name is in hand and the change adds **no git forks** to a path that already forks git. The picker's managed-worktree walk stays pure-filesystem and file-based project expansion is untouched, so no hot path gets slower.

Session names for existing non-bare worktrees change once, and a `repo/worktree` name is what pop already produces for bare repos, so nothing new has to learn to parse it.

## Considered Options

- **Prefix managed worktrees only.** Rejected: the missing prefix is a property of the non-bare branch of one function, not of managed checkouts; fixing the class costs the same as fixing the instance.
- **Put the repository in a display label and leave the session name alone.** Rejected: the label is not what tmux dedupes on, so the picker/session mismatch would survive.
- **Brackets in the tmux name (`pop [2026-06-22-runtime-shell]`).** Rejected: every `tmux attach -t` would need quoting. Brackets stay available to picker *labels*, which are free to be prettier than the name.
- **Make `FastSessionName` exact.** Rejected — see below.

## Consequences

- `project.FastSessionName` — the deliberately git-free approximation ADR-0005 introduced for dashboard history matching — is now inexact for **every** worktree rather than only bare-repo ones. Left approximate on purpose: making it exact means forking git in the one place that exists to avoid it. ADR-0005's licence is widened, not withdrawn.
- A managed worktree's session name becomes `repoName/<setID>`, matching what ADR-0110's walk publishes, so a live drain session finally dedupes with its picker entry.
- A pre-provisioned managed worktree (ADR-0152) is named by the human, not by a set id, so its label shows the folder name. Following the binding instead would require a store read on the picker's hot path, which ADR-0110 forbids; unchanged here.
