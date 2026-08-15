# How to work out which Work store this repository publishes into

The `issue-tracker` convention answers one question: when a planning session
turns a plan into issues, specs or task sets, **where do they go**, and by what
mechanics. It is normally answered by `~/.agents/docs/issue-tracker.md`, which
pop's Integration refresh installs. You are reading this recipe because that
file is not there — most likely a machine where integration was never
refreshed.

Work the steps in order and stop at the first one that resolves a store.

## 1. Refresh integration first

Re-running `pop integrate <agent>` re-asserts pop's agent-facing assets, and
that step links `~/.agents/docs/issue-tracker.md` at pop's shipped tracker doc
when nothing occupies the path. On a machine that simply never integrated, that
one command resolves this kind for every repository at once, and resolves it the
way every other machine is resolved. Prefer it to deriving anything by hand.

## 2. Otherwise ask the repository what it already uses

If integration cannot be refreshed here, look for the store the repository is
already publishing into, in this order:

1. **A tracker document in version control** — `docs/agents/issue-tracker.md`,
   or whatever the repository's agent docs point at. It wins outright: it is the
   team's answer, and it names the publishing mechanics as well as the store.
2. **A tracker the tooling implies** — a configured GitHub or Jira project, a
   `.scratch/` issues tree, a pop Work store already holding task sets for this
   repository (`pop work show-path`). Evidence that issues have been filed
   somewhere is evidence of the store.
3. **The human.** If nothing above resolves, ask which tracker this repository
   files into rather than guessing one. Publishing into the wrong store is
   worse than not publishing.

## 3. Write the result down, in the layer that fits where it came from

- **Derived by you** from the repository's tooling goes to the **pop memory**
  layer — pop's inference about one repository on one machine.
  `pop repo conventions get issue-tracker` names that layer's path.
- **Stated by the human**, or true for every repository on this machine, belongs
  further out: offer `docs/agents/issue-tracker.md` when the store is the team's
  and version control should carry it, and prefer refreshing integration when
  the answer is really "this machine's default store".

Record the **publishing mechanics** with the store, not just its name: which
verb creates an issue, what a blocking edge looks like there, and any labels the
store expects. A store name alone still leaves the next agent to work out how to
file into it.
