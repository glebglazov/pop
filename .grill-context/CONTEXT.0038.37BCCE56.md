---
fragment: 37BCCE56
generation: 0038
branch: master
---

+ Dashboard reload
  The Work dashboard's one refresh primitive: a full snapshot rebuild from the
  manifests and the **Execution-state store**, fired by the poll tick and
  immediately after any write verb's refresh outcome — so the dashboard's own
  writes are read back at once, while another process's writes wait at most one
  poll interval. Every reload is sequence-stamped when it **starts**, and a
  result older than the newest one applied is dropped, so two overlapping
  rebuilds can never land a pre-write snapshot over a post-write one. Everything
  the dashboard shows or acts through is renewed by it — the rows, the kind
  adapters a verb performs through (whose git memos are one-load-scoped by
  contract), a peeked document's text — and nothing updates outside it: no verb
  patches the view optimistically, and no read cache outlives the loop except by
  a content key. A file watcher is deliberately absent: every stale read found
  traced to state escaping the rebuild, never to the poll being slow.
  avoid: optimistic patch, partial refresh, cache invalidation, reload race, file watcher
  under: Language

~ Config dashboard host
  A program that opens the **Config dashboard** inside itself, and what it owes
  the component (ADR-0202 decisions 11 and 14). Three things: while the modal is
  open the host's own keys are fully suspended — no page toggle, no kind's
  action verb, because one host binds `ctrl+x` to *force delete worktree* and
  the component binds it to *remove the override*; the host never lets it print,
  its stdout being a data channel in two of the three; and after a write the
  host re-reads config, since a host that loaded once is rendering the value the
  human just changed. The Work dashboard is the first host, where the modal
  lives in `dashboardshell` rather than on a page, because both the page toggle
  and the one config load are the shell's; the project and worktree pickers are
  the other two, hosting it in `ui.Picker` itself, which is handed an opener
  rather than config. A picker has nothing to re-read: it builds its items
  before it runs and holds no live config, so only the Work dashboard
  hot-reloads. The modal's post-write re-read is no longer the only trigger:
  the Work dashboard shell also re-reads when a config file's mtime changes
  under it, checked each poll, so a `pop config` write or a hand edit from
  another pane lands without a restart — amending ADR-0202 decision 14 through
  the same reconciliation path the modal already uses. Nothing else
  hot-reloads: the supervisor re-reads every pass, each drain it spawns is a
  fresh process, and an in-flight drain finishes on the list it started with.
  was: The same contract with the modal's post-write re-read as the Work
    dashboard's only config reload, per ADR-0202 decision 14's "nothing is
    hot-reloaded".
