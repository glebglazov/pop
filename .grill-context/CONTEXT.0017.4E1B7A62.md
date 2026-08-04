+ Session nesting
  The **Project picker**'s display-only grouping of a project's non-trunk
  live sessions as a second level under the project row, opt-in via
  `[project] worktree_display = "flat" | "nested"` (default `flat`,
  permanently). No tmux session is ever renamed and no path changes — only the
  rendering, which drops the `<project>/` prefix on a nested row and trails a
  project holding nested sessions with `▸`/`▾`. The two modes deliberately
  list different rows: flat shows every worktree, nested only those with a live
  session. Membership is sessions, not checkouts — a **Map session** nests
  alongside the worktrees — so the level answers "what can I attach to under
  this project".
  avoid: worktree nesting, worktree tree, session grouping, nested picker
  under: Pickers

~ Work session
  A tmux session pop opened for one Work container, typed by the tmux user
  options `@pop_work_kind` (`map` | `task-set` | `routine`) and `@pop_work_id`.
  The stamp lives on the session rather than in pop.db because it describes
  something *live*: tying its lifetime to tmux's means there is never a stale
  row to reconcile, while the durable half of the same story — **Ticket
  claim**s — is already in the database. `pop project dashboard` lists Work
  sessions as rows but does not badge them by kind: it renders one glyph
  column, `◇` for a Map session and `■` for every other live session, because
  which kind of Work a session hosts is the **Work dashboard**'s question. The
  mechanism is kind-general so the Task-set and Routine spawns can stamp theirs
  too, and the Work dashboard reads the stamp there.
  was: A tmux session pop opened for one Work container, typed by the tmux user
    options `@pop_work_kind` (`map` | `task-set` | `routine`) and
    `@pop_work_id`. The stamp lives on the session rather than in pop.db
    because it describes something *live*: tying its lifetime to tmux's means
    there is never a stale row to reconcile, while the durable half of the same
    story — **Ticket claim**s — is already in the database. `pop project
    dashboard` lists Work sessions as rows and badges them by kind, reading the
    stamp and never the session name. The mechanism is kind-general so the
    Task-set and Routine spawns can stamp theirs too.

<!-- Minted by the map-session-nesting slice of the
2026-08-03-worktree-session-locality set, from ADR-0185. The term was coined as
`Worktree nesting` and renamed to `Session nesting` before minting: the level
holds a non-worktree member (a Map session), so a term naming the membership rule
after worktrees would have been wrong on arrival. It was never minted under the
old name, so no `was:` line is owed. The config key keeps its `worktree_display`
name deliberately — renaming a key to match a glossary term is churn.

The draft said "Project dashboard"; the glossary's term for that surface is
**Project picker**, so the reference is written under the term that exists rather
than coining a synonym for it.

Mints together with CONTEXT.0018, whose `~ Map session` references
`Session nesting`. -->
